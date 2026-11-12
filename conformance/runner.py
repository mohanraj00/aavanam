"""Run the conformance vectors against an implementation through an adapter.

Usage: python3 -B conformance/runner.py --adapter null_adapter:NullAdapter [vector-id ...]
"""
from __future__ import annotations

import argparse
import base64
import copy
import hashlib
import importlib
import json
import math
import sys
from decimal import Decimal
from pathlib import Path

HERE = Path(__file__).resolve().parent
VECTORS = HERE / 'vectors'
sys.path.insert(0, str(HERE.parent / 'tools'))
import check_spec  # noqa: E402
BASE_PROFILE = 'aavanam/base/v1'
NOT_READ = {'unsupported_operation', 'reserved_profile'}
ENVELOPE_FIELDS = ('status', 'reasons', 'operation', 'operation_profile', 'context')


class VectorError(Exception):
    """The vector does not agree with the format in conformance/README.md."""


def load(root: Path = VECTORS) -> dict[str, dict]:
    """Return each vector under `root` by its ID, the path without `.json`."""
    vectors = {}
    for path in sorted(root.rglob('*.json')):
        vid = path.relative_to(root).with_suffix('').as_posix()
        try:
            vectors[vid] = json.loads(path.read_text())
        except json.JSONDecodeError as e:
            raise VectorError(f'{vid}: not valid JSON: {e}') from None
    return vectors


def digest(data: bytes) -> str:
    return 'sha256:' + hashlib.sha256(data).hexdigest()


class Expander:
    """Replace the handles of one vector with values for the adapter."""

    def __init__(self, vid: str, vector: dict):
        self.vid = vid
        self.payloads = vector.get('payloads', {})
        self.refs: dict[str, str] = {}
        self.contexts: dict[str, str] = {}
        self.bundles: dict[str, dict] = {}
        self.stored: dict[str, str] = {}

    def payload(self, name: str, fault: str | None, sent: dict[str, str]) -> dict:
        if name not in self.payloads:
            raise VectorError(f'{self.vid}: unknown payload {name}')
        decl = self.payloads[name]
        data = decl['text'].encode() if 'text' in decl else base64.b64decode(decl['base64'], validate=True)
        value = {'digest': digest(data), 'media_type': decl['media_type'], 'size': len(data)}
        if fault == 'digest':
            value['digest'] = digest(data + b' fault')
        elif fault == 'size':
            value['size'] += 1
        elif fault is not None:
            raise VectorError(f'{self.vid}: unknown payload fault {fault}')
        if decl.get('stored', True):
            sent[value['digest']] = base64.b64encode(data).decode()
        return value

    def bundle(self, name: str, fault: str | None) -> dict:
        if name not in self.bundles:
            raise VectorError(f'{self.vid}: bundle handle {name} is not bound')
        value = copy.deepcopy(self.bundles[name])
        if fault == 'drop_payload':
            payloads = value.get('payloads') if isinstance(value, dict) else None
            if not isinstance(payloads, dict) or not payloads:
                raise VectorError(f'{self.vid}: bundle {name} has no payload to drop')
            del payloads[min(payloads)]
        elif fault == 'format':
            value['format'] = 'sample/bundle/v1'
        elif fault is not None:
            raise VectorError(f'{self.vid}: unknown bundle fault {fault}')
        return value

    def value(self, v, sent: dict[str, str]):
        if isinstance(v, list):
            return [self.value(x, sent) for x in v]
        if not isinstance(v, dict):
            return v
        if '$ref' in v:
            if v['$ref'] not in self.refs:
                raise VectorError(f'{self.vid}: handle {v["$ref"]} is not bound')
            return self.refs[v['$ref']]
        if '$context' in v:
            if v['$context'] not in self.contexts:
                raise VectorError(f'{self.vid}: context handle {v["$context"]} is not bound')
            return self.contexts[v['$context']]
        if '$bundle' in v:
            return self.bundle(v['$bundle'], v.get('fault'))
        if '$absent' in v:
            return digest(f'aavanam-vector absent {v["$absent"]}'.encode())
        if '$payload' in v:
            return self.payload(v['$payload'], v.get('fault'), sent)
        return {k: self.value(x, sent) for k, x in v.items()}

    def request(self, operation: str, request: dict) -> tuple[dict, list[str | None]]:
        """Return the adapter request and the handle of each `put` descriptor."""
        sent: dict[str, str] = {}
        if operation != 'put':
            return self.value(request, sent), []
        texts, names = [], []
        for entry in request['descriptors']:
            if 'raw' in entry:
                texts.append(entry['raw'])
            else:
                value = self.value(entry['value'], sent)
                texts.append(json.dumps(value, ensure_ascii=False, separators=(',', ':'), sort_keys=True))
            names.append(entry.get('as'))
        return {'descriptors': texts, 'payloads': sent}, names


def codes(reasons, where: str, problems: list[str]) -> list[str]:
    if not isinstance(reasons, list):
        problems.append(f'{where}: reasons is not a list')
        return []
    out = []
    for r in reasons:
        if not (isinstance(r, dict) and isinstance(r.get('code'), str)
                and isinstance(r.get('refs'), list) and isinstance(r.get('capabilities'), list)):
            problems.append(f'{where}: a reason does not have code, refs and capabilities (STS-RSN-2): {r}')
            continue
        out.append(r['code'])
    return sorted(out)


def same(value):
    """Return a value that is equal for two JSON values only if they have the same JSON types and values."""
    if isinstance(value, bool) or value is None:
        return (type(value).__name__, value)
    if isinstance(value, (int, float)):
        return ('number', value)
    if isinstance(value, list):
        return ('array', [same(x) for x in value])
    if isinstance(value, dict):
        return ('object', sorted((k, same(x)) for k, x in value.items()))
    return (type(value).__name__, value)


def detail_problems(details: list[dict], reasons, where: str) -> list[str]:
    """Compare each reason code with its sorted `refs` or `capabilities`, as `reason_details` gives them (STS-RSN-2)."""
    keys = [k for k in ('refs', 'capabilities') if all(k in r for r in details)]
    want = sorted([r['code'], *(sorted(r[k]) for k in keys)] for r in details)
    have = [r for r in reasons if isinstance(r, dict)] if isinstance(reasons, list) else []
    if not all(isinstance(r.get('code'), str) for r in have):
        return [f'{where}: a reason has a code that is not a string (STS-RSN-2)']
    if not all(isinstance(r.get(k), list) and all(isinstance(x, str) for x in r[k]) for r in have for k in keys):
        return [f'{where}: a reason has {" or ".join(keys)} that is not a list of strings (STS-RSN-2)']
    got = sorted([r.get('code'), *(sorted(r[k]) for k in keys)] for r in have)
    return [] if got == want else [f'{where}: the reasons with {" and ".join(keys)} are {got}, not {want} (STS-RSN-2)']


def item_problems(e: dict, a: dict, where: str) -> list[str]:
    """Return the differences between an expected and an actual item."""
    problems = []
    if a.get('status') != e['status']:
        problems.append(f'{where}: status is {a.get("status")!r}, not {e["status"]!r}')
    got = codes(a.get('reasons'), where, problems)
    if got != sorted(e['reasons']):
        problems.append(f'{where}: reasons are {got}, not {sorted(e["reasons"])}')
    for k, v in sorted(e.get('fields', {}).items()):
        if k not in a or same(a[k]) != same(v):
            problems.append(f'{where}: {k} is {a.get(k, "absent")!r}, not {v!r}')
    if 'reason_details' in e:
        problems += detail_problems(e['reason_details'], a.get('reasons'), where)
    return problems


def match(expected: list[dict], actual: list[dict], used: frozenset = frozenset()) -> bool:
    """Return True if each expected item matches a different actual item."""
    if not expected:
        return True
    return any(i not in used and isinstance(a, dict) and not item_problems(expected[0], a, '')
               and match(expected[1:], actual, used | {i}) for i, a in enumerate(actual))


def compare(expect: dict, actual, operation: str) -> list[str]:
    """Return the differences between an expected and an actual envelope. Handles in `expect` are replaced."""
    if not isinstance(actual, dict):
        return [f'the result is not a JSON object: {actual!r}']
    problems = [f'envelope has no field {f} (STS-ENV-1)' for f in ENVELOPE_FIELDS if f not in actual]
    if actual.get('operation') != operation:
        problems.append(f'operation is {actual.get("operation")!r}, not {operation!r}')
    if actual.get('status') != expect['status']:
        problems.append(f'status is {actual.get("status")!r}, not {expect["status"]!r}')
    got = codes(actual.get('reasons'), 'envelope', problems)
    if got != sorted(expect['reasons']):
        problems.append(f'reasons are {got}, not {sorted(expect["reasons"])}')
    if 'reason_details' in expect:
        problems += detail_problems(expect['reason_details'], actual.get('reasons'), 'envelope')
    items = actual.get('items')
    if 'items' in actual:
        if not isinstance(items, list):
            return problems + ['items is not a list']
        for i, a in enumerate(items):
            if not isinstance(a, dict) or 'status' not in a or 'reasons' not in a:
                problems.append(f'item {i} does not have status and reasons (STS-ENV-2)')
            elif 'items' not in expect:
                got_item = codes(a['reasons'], f'item {i}', problems)
                status = a['status']
                if not isinstance(status, str) or status not in ITEM_STATUSES or (status == 'ok') != (got_item == []):
                    problems.append(f'item {i} has status {status!r} with reasons {got_item} (STS-SET-1, STS-RSN-1)')
                elif wrong := [c for c in got_item if not check_spec.EXT_CODE.match(c) and REASON_STATUS.get(c) != status]:
                    problems.append(f'item {i} has status {status!r} with reasons {wrong} of other statuses (STS-RSN-3)')
    if 'items' in expect:
        if not isinstance(items, list) or len(items) != len(expect['items']):
            n = len(items) if isinstance(items, list) else 'no'
            return problems + [f'result has {n} items, not {len(expect["items"])}']
        if expect.get('unordered'):
            if not match(expect['items'], items):
                problems.append('the items do not match the expected items in any order')
            return problems
        for i, (e, a) in enumerate(zip(expect['items'], items)):
            if isinstance(a, dict):
                problems += item_problems(e, a, f'item {i}')
    return problems


def es_number(x: float) -> str:
    """Return the ECMAScript form of a finite number, as RFC 8785 section 3.2.2.3 requires."""
    if x == 0:
        return '0'
    if x < 0:
        return '-' + es_number(-x)
    _, digits, exp = Decimal(repr(x)).as_tuple()
    d = ''.join(map(str, digits)).rstrip('0')
    k, n = len(d), len(digits) + exp
    if k <= n <= 21:
        return d + '0' * (n - k)
    if 0 < n <= 21:
        return d[:n] + '.' + d[n:]
    if -6 < n <= 0:
        return '0.' + '0' * -n + d
    e = f'{n - 1:+d}'
    return (d if k == 1 else d[0] + '.' + d[1:]) + 'e' + e


class _Token(str):
    """A part of the canonical text that is already encoded."""


def canonical(value) -> str:
    """Return the JSON canonical form of RFC 8785 (IDN-CAN-1). It uses no recursion, so any depth that JSON parses is valid."""
    out, todo = [], [value]
    while todo:
        value = todo.pop()
        if isinstance(value, _Token):
            out.append(value)
        elif isinstance(value, dict):
            parts = []
            for k in sorted(value, key=lambda k: k.encode('utf-16-be')):
                parts += [_Token(','), _Token(json.dumps(k, ensure_ascii=False) + ':'), value[k]]
            todo += [_Token('}'), *reversed(parts[1:]), _Token('{')]
        elif isinstance(value, list):
            parts = [x for v in value for x in (_Token(','), v)]
            todo += [_Token(']'), *reversed(parts[1:]), _Token('[')]
        elif isinstance(value, bool) or value is None or isinstance(value, str):
            out.append(json.dumps(value, ensure_ascii=False))
        else:
            out.append(es_number(finite(value)))
    return ''.join(out)


def finite(value) -> float:
    try:
        x = float(value) if isinstance(value, (int, float)) else math.nan
    except OverflowError:
        x = math.inf
    if not math.isfinite(x):
        raise ValueError(f'{value!r} is not a finite JSON number (RFC 8785 section 3.2.2.3)')
    return x


def revision(descriptor, schemas: dict) -> str:
    """Return the descriptor revision: the digest of the canonical form (IDN-CAN-1, IDN-CAN-2)."""
    return digest(canonical(normal(descriptor, schemas)).encode('utf-8'))


def lock_descriptor(operation: str, request: dict) -> dict:
    """Return the lock that an ok lock operation writes for a request (LCK-EXE-4, LCK-BLD-3)."""
    kind, fields = {'execution_inputs': ('execution-lock', ('binding_set', 'context_inputs', 'requires', 'consumer', 'completeness')),
                    'build_inputs': ('build-lock', ('inputs', 'requires', 'consumer', 'completeness'))}[operation]
    return {'descriptor_profile': BASE_PROFILE, 'kind': kind, **{f: request[f] for f in fields if f in request}}


def snapshot_descriptor(request: dict) -> dict:
    """Return the snapshot that an ok create_snapshot writes for a request (PUB-SNP-1)."""
    return {**request, 'descriptor_profile': BASE_PROFILE, 'kind': 'snapshot'}


def candidate_identity(request: dict) -> str:
    """Return the identity of the candidate of a create_candidate request (PUB-CND-3)."""
    record = {'format': 'aavanam-candidate/v1', 'snapshot': request['snapshot'], 'checks': sorted(request.get('checks', [])),
              'policy': request['policy'], 'expected_head': request['expected_head']}
    return digest(canonical(record).encode('utf-8'))


def created(operation: str, request: dict) -> tuple[str, str]:
    """Return the item field and the value that an ok create_snapshot or create_candidate returns (PUB-SNP-3, PUB-CND-3)."""
    if operation == 'create_snapshot':
        return 'ref', revision(snapshot_descriptor(request), check_spec.schemas())
    return 'candidate', candidate_identity(request)


def created_problems(operation: str, request: dict, expect: dict, actual: dict) -> list[str]:
    """An ok create_snapshot has the revision of its snapshot, and an ok create_candidate has its identity (PUB-SNP-3, PUB-CND-3)."""
    if operation not in CREATE_OPERATIONS or expect['status'] != 'ok':
        return []
    field, want = created(operation, request)
    items = actual.get('items')
    got = items[0].get(field) if isinstance(items, list) and items and isinstance(items[0], dict) else None
    return [] if got == want else [f'item 0 has {field} {got!r}, not {want} (PUB-SNP-3, PUB-CND-3)']


def schema_of(descriptor, schemas: dict) -> dict[str, tuple[str, str]]:
    kind = descriptor.get('kind') if isinstance(descriptor, dict) else None
    return {**schemas[''], **(schemas.get(kind, {}) if isinstance(kind, str) else {})}


def payload_objects(descriptor, schemas: dict) -> list:
    """Return the values of the payload fields of a descriptor (KND-DEP-3). Other JSON is opaque."""
    if not isinstance(descriptor, dict):
        return []
    fields = schema_of(descriptor, schemas)
    return [descriptor[f] for f, (vtype, _) in sorted(fields.items()) if vtype == 'payload' and f in descriptor]


def normal(descriptor, schemas: dict):
    """Return the descriptor without empty optional lists and with sorted reference sets (KND-SCH-3, KND-SCH-5)."""
    if not isinstance(descriptor, dict):
        return descriptor
    out = dict(descriptor)
    for field, (_, count) in schema_of(descriptor, schemas).items():
        value = out.get(field)
        if not isinstance(value, list):
            continue
        if not value and count in ('0..n', '0..n list'):
            del out[field]
        elif count in ('0..n', '1..n') and all(isinstance(v, str) for v in value):
            out[field] = sorted(value)
    return out


def unique_keys(pairs: list) -> dict:
    keys = [k for k, _ in pairs]
    if len(set(keys)) != len(keys):
        raise ValueError('duplicate key')
    return dict(pairs)


def bundle_problems(operation: str, request: dict, expect: dict, actual: dict, stored: dict[str, str]) -> list[str]:
    """Check the bundle of an export result (BUN-MAN-1, BUN-EXP-1, BUN-EXP-2 and BUN-RED-5)."""
    if operation != 'export':
        return []
    bundle = actual.get('bundle')
    if expect['status'] != 'ok':
        return [] if bundle is None else ['a refused export has a bundle (BUN-EXP-1)']
    if not isinstance(bundle, dict) or set(bundle) - {'withheld'} != {'format', 'roots', 'descriptors', 'payloads'}:
        return ['bundle does not have the fields of BUN-MAN-1']
    problems = []
    withheld = bundle.get('withheld', [])
    if not isinstance(withheld, list) or ('withheld' in bundle and not withheld) or not all(isinstance(w, str) for w in withheld) \
            or len(set(withheld)) != len(withheld):
        return ['bundle withheld is not a list of one or more different digests (BUN-MAN-1)']
    if bundle['format'] != 'aavanam-bundle/v1':
        problems.append(f'bundle format is {bundle["format"]!r} (BUN-MAN-1)')
    if not isinstance(bundle['roots'], list) or sorted(map(str, bundle['roots'])) != sorted(map(str, request.get('roots', []))):
        problems.append('bundle roots are not the request roots (BUN-EXP-2)')
    texts, payloads = bundle['descriptors'], bundle['payloads']
    if not isinstance(texts, list) or not all(isinstance(t, str) for t in texts) or not isinstance(payloads, dict):
        return problems + ['bundle descriptors or payloads do not have the form of BUN-MAN-1']
    if len(set(texts)) != len(texts):
        problems.append('bundle has a descriptor more than one time (BUN-EXP-2)')
    try:
        values = [json.loads(t, object_pairs_hook=unique_keys) for t in texts]
        for v in values:
            canonical(v)
    except (ValueError, RecursionError):
        return problems + ['a bundle descriptor is not JSON, has a duplicate key or a number that is not finite (BUN-MAN-1, STS-MAL-3, IDN-CAN-1)']
    schemas = check_spec.schemas()
    refs = [i.get('fields', {}).get('ref') for i in expect.get('items', [])]
    if refs and all(r in stored for r in refs):
        mine = sorted(canonical(normal(v, schemas)) for v in values)
        if mine != sorted(canonical(normal(json.loads(stored[r]), schemas)) for r in refs):
            problems.append('bundle descriptors are not the descriptors of the closure (BUN-EXP-2)')
    elif isinstance(actual.get('items'), list) and len(texts) != len(actual['items']):
        problems.append(f'bundle has {len(texts)} descriptors for {len(actual["items"])} items (BUN-EXP-2)')
    wanted = {}
    for value in values:
        for p in payload_objects(value, schemas):
            if isinstance(p, dict) and isinstance(p.get('digest'), str):
                wanted[p['digest']] = p.get('size')
    if set(wanted) != set(payloads) | set(withheld) or set(payloads) & set(withheld):
        problems.append('bundle payloads and withheld digests are not the byte closure of its descriptors (BUN-EXP-2, BUN-RED-5)')
    flagged = [i.get('fields', {}) for i in expect.get('items', [])]
    if flagged and all('withheld' in f and f.get('ref') in stored for f in flagged):
        mine = {p['digest'] for f in flagged if f['withheld'] is True
                for p in payload_objects(json.loads(stored[f['ref']]), schemas)
                if isinstance(p, dict) and isinstance(p.get('digest'), str)}
        if mine != set(withheld):
            problems.append('bundle withheld digests are not the payload digests of the withheld items (BUN-RED-5)')
    for digest, text in payloads.items():
        try:
            data = base64.b64decode(text, validate=True)
        except (TypeError, ValueError):
            problems.append(f'bundle payload {digest} is not base64 (BUN-MAN-1)')
            continue
        if digest != 'sha256:' + hashlib.sha256(data).hexdigest() or (digest in wanted and wanted[digest] != len(data)):
            problems.append(f'bundle payload {digest} does not agree with its digest and size (KND-OPQ-1)')
    return problems


STORE_READS = {'snapshot_changes', 'item_history'}  # the operations that read no read context (LIF-ITM-4)


def context_problems(operation: str, step: dict, request: dict, expect: dict, actual: dict) -> list[str]:
    """Return the problems of the envelope field `context` (CTX-OPN-2, CTX-OPN-3)."""
    if NOT_READ & set(expect['reasons']):
        return []  # STS-MAL-4: the store does not read the input.
    context = actual.get('context')
    if operation == 'open_context':
        if expect['status'] == 'ok' and not isinstance(context, str):
            return [f'context is {context!r}, not a context identity (CTX-OPN-2)']
        if expect['status'] != 'ok' and context is not None:
            return [f'context is {context!r}, not null (CTX-OPN-2)']
    elif operation in STORE_READS:
        if context is not None:
            return [f'context is {context!r}, not null (LIF-ITM-4)']
    elif is_context_handle(step.get('request', {}).get('context')) and same(context) != same(request['context']):
        return [f'context is {context!r}, not the request context (CTX-OPN-3)']
    return []


ITEM_STATUSES = set(check_spec.STATUS_SET) - {'partial'}  # STS-SET-1
REASON_STATUS = check_spec.reason_statuses()  # STS-RSN-3
LOCK_OPERATIONS = {'execution_inputs', 'build_inputs'}
CREATE_OPERATIONS = {'create_snapshot', 'create_candidate'}


RENDER_FIELDS = {  # the fields of each entry type of a render item, without status and reasons (RND-SCP-1 to RND-SCP-4)
    'subject': {'entry', 'ref', 'kind'},
    'check': {'entry', 'subject', 'ref', 'check_profile', 'outcome', 'scope', 'origin'},
    'coverage': {'entry', 'subject', 'ref', 'unit', 'covered', 'total', 'omissions'},
    'gap': {'entry', 'subject', 'gap', 'ref'},
}
GAPS = {'no_check', 'no_coverage', 'values_form', 'unknown_total', 'omissions', 'withdrawn'}
RENDER_PROFILE = 'aavanam/render/v1'


def render_document(request: dict, items: list[dict]) -> dict:
    """Make the document of a render result from the request and the items (RND-DOC-3)."""
    entries = [{k: v for k, v in i.items() if k not in ('status', 'reasons')} for i in items if i.get('status') == 'ok']
    omitted = {i.get('ref') for i in items if i.get('status') != 'ok' and isinstance(i.get('ref'), str)}
    return {'format': 'aavanam-render/v1', 'render_profile': request.get('render_profile', RENDER_PROFILE),
            'context': request.get('context'), 'roots': sorted(set(request.get('roots', []))),
            'entries': sorted(entries, key=canonical), 'omitted': sorted(omitted)}


def render_problems(operation: str, request: dict, actual: dict) -> list[str]:
    """Check the items and the document of a render result (RND-DOC-2, RND-DOC-3, RND-SCP-5)."""
    if operation != 'render':
        return []
    problems = [f'the envelope has the field {f}, which render does not have (RND-DOC-3, RND-SCP-5)'
                for f in sorted(set(actual) - set(ENVELOPE_FIELDS) - {'items', 'document', 'document_id'})]
    problems += [f'the envelope has no field {f} (RND-DOC-3)' for f in ('document', 'document_id') if f not in actual]
    document, document_id = actual.get('document'), actual.get('document_id')
    items = actual.get('items')
    if not isinstance(items, list) or not all(isinstance(i, dict) for i in items):
        items = []  # compare() reports the malformed items.
    for n, item in enumerate(items):
        fields = set(item) - {'status', 'reasons'}
        if not isinstance(item.get('entry'), str) or not isinstance(item.get('gap', ''), str):
            problems.append(f'item {n} has an entry or a gap that is not a string (RND-SCP-1, RND-SCP-4)')
        elif item.get('status') != 'ok':
            if fields != {'entry', 'ref'} or item.get('entry') != 'subject':
                problems.append(f'item {n} is not ok, so it is a subject item with only entry and ref (RND-DOC-2)')
        elif RENDER_FIELDS.get(item.get('entry')) != fields:
            problems.append(f'item {n} has the fields {sorted(fields)}, not the fields of its entry (RND-SCP-1 to RND-SCP-5)')
        elif item['entry'] == 'gap' and item['gap'] not in GAPS:
            problems.append(f'item {n} has the gap {item["gap"]!r}, not a gap of RND-SCP-4')
    if actual.get('status') not in ('ok', 'partial'):
        if document is not None or document_id is not None:
            problems.append('a render that is not ok or partial has a document (RND-DOC-3)')
        return problems
    if problems:
        return problems
    try:
        want = render_document(request, items)
        want_id = digest(canonical(want).encode('utf-8'))
    except (TypeError, ValueError):
        return ['the items of the render result cannot make a document (RND-DOC-3)']
    if same(document) != same(want):
        problems.append('document is not the document of the request and the items (RND-DOC-3)')
    if document_id != want_id:
        problems.append(f'document_id is {document_id!r}, not {want_id} (RND-DOC-3)')
    return problems


def complete_operations() -> set[str]:
    """Return the operations whose row in the operation table does not permit `partial` (STS-ENV-4)."""
    cs = check_spec
    rows = cs.table(cs.STATUSES.read_text(), 'The operation table:', 3, cs.STATUSES.name, [])
    return {row[0] for row in rows if 'partial' not in {s.strip() for s in row[2].split(',')}}


def complete_problems(operation: str, actual: dict, complete: set[str]) -> list[str]:
    """A complete operation that is not ok returns no lock and no bundle (STS-ENV-4)."""
    if operation not in complete or actual.get('status') == 'ok':
        return []
    if actual.get('lock') is not None or actual.get('bundle') is not None:
        return ['a complete operation that is not ok returned a lock or a bundle (STS-ENV-4)']
    return []


def reserved_problems(operation: str, expect: dict, actual: dict, profiles: dict[str, str]) -> list[str]:
    """A reserved operation names its profile in the reason `reserved_profile` and returns no lock or bundle (RSV-RSP-1, STS-ENV-4)."""
    if operation not in profiles or 'reserved_profile' not in expect['reasons']:
        return []
    problems = []
    if actual.get('lock') is not None or actual.get('bundle') is not None:
        problems.append('a reserved operation returned a lock or a bundle (STS-ENV-4)')
    reasons = actual.get('reasons')
    if not isinstance(reasons, list):
        return problems  # compare() reports the malformed reasons.
    want = {'code': 'reserved_profile', 'refs': [], 'capabilities': [profiles[operation]]}
    found = [r for r in reasons if isinstance(r, dict) and r.get('code') == 'reserved_profile']
    if any({k: r.get(k) for k in want} != want for r in found):
        problems.append(f'the reason reserved_profile does not have capabilities [{profiles[operation]!r}] and no refs (RSV-RSP-1)')
    return problems


VALUE_FIELDS = {'query_data': ('rows', 'next', 'metadata')}


def value_problems(operation: str, actual: dict) -> list[str]:
    """An item that is not ok has no value field (QRY-DAT-3)."""
    items = actual.get('items') if isinstance(actual.get('items'), list) else []
    return [f'item {n} is {i.get("status")!r} but has the field {f} (QRY-DAT-3)'
            for n, i in enumerate(items) if isinstance(i, dict) and i.get('status') != 'ok'
            for f in VALUE_FIELDS.get(operation, ()) if f in i]


def lock_problems(operation: str, request: dict, expect: dict, actual: dict) -> list[str]:
    """An ok lock result has the reference of the lock. Any other result has no lock (LCK-EXE-4, LCK-EXE-6, LCK-BLD-3, LCK-BLD-5)."""
    if operation not in LOCK_OPERATIONS or NOT_READ & set(expect['reasons']):
        return []
    lock = actual.get('lock')
    if expect['status'] == 'ok' and lock != revision(lock_descriptor(operation, request), check_spec.schemas()):
        return [f'lock is {lock!r}, not the revision of the lock of the request (LCK-EXE-4, LCK-BLD-3, IDN-CAN-2)']
    if expect['status'] != 'ok' and lock is not None:
        return [f'lock is {lock!r}, but a result that is not ok has no lock (LCK-EXE-6, LCK-BLD-5)']
    return []


def stored_lock_problems(adapter, operation: str, request: dict, lock: str, store: str = 'writer') -> list[str]:
    """Read the lock in the store that keeps it: a put of a descriptor that the store has gives its ref and does
    not change the store (IDN-CAN-3). Thus the store has the lock (LCK-EXE-4, LCK-BLD-3)."""
    text = canonical(normal(lock_descriptor(operation, request), check_spec.schemas()))
    before = adapter.fingerprint(store=store)
    try:
        result = adapter.call('put', {'descriptors': [text], 'payloads': {}}, store=store)
    except Exception as e:  # noqa: BLE001
        return [f'the put of the lock raised {type(e).__name__}: {e}']
    expect = {'status': 'ok', 'reasons': [], 'items': [{'status': 'ok', 'reasons': [], 'fields': {'ref': lock}}]}
    if problems := compare(expect, result, 'put'):
        return [f'a put of the lock does not give ok with ref {lock} (IDN-CAN-3): {p}' for p in problems]
    if result.get('context') is not None:
        return [f'a put of the lock has the context {result["context"]!r}, not null (STS-ENV-1)']
    if adapter.fingerprint(store=store) != before:
        return ['a put of the lock changed the store, so the store did not have the lock (LCK-EXE-4, LCK-BLD-3, IDN-CAN-3)']
    return []


def revision_problems(operation: str, request: dict, expect: dict, actual: dict) -> list[str]:
    """Each ok item of an ok put has the revision of its descriptor in the field `ref` (IDN-CAN-2)."""
    if operation != 'put' or expect['status'] != 'ok' or not isinstance(actual.get('items'), list):
        return []
    schemas, problems = check_spec.schemas(), []
    for i, (text, item) in enumerate(zip(request['descriptors'], actual['items'])):
        want = revision(json.loads(text), schemas)
        if isinstance(item, dict) and item.get('ref') != want:
            problems.append(f'item {i} has ref {item.get("ref")!r}, not the revision {want} (IDN-CAN-2)')
    return problems


def is_context_handle(value) -> bool:
    return isinstance(value, dict) and '$context' in value


def register(adapter, expander: Expander, acceptance: dict, calls: list) -> None:
    """Register the test policy of a vector (PUB-RCP-1). It accepts only the listed pairs of candidate and receipt.

    Each call adds its pair, its decision and its activation to `calls`. A pair with a handle that a later step binds is not accepted yet.
    The decision does not depend on the activation. The activation is null if the request has none (PUB-RCP-2).
    """
    def decide(candidate, receipt) -> bool:
        for pair in acceptance['accept']:
            try:
                if expander.value(pair, {}) == [candidate, receipt]:
                    return True
            except VectorError:
                continue
        return False

    def validate(candidate: str, receipt: str, activation: str | None = None) -> bool:
        decision = decide(candidate, receipt)
        calls.append((candidate, receipt, decision, activation))
        return decision
    adapter.accept(acceptance['policy'], validate)


def expected_item(operation: str, expect: dict) -> dict:
    items = expect.get('items') if operation == 'publish' else None
    return items[0] if isinstance(items, list) and items and isinstance(items[0], dict) else {}


# The activation checks give these reasons before step 4 of PUB-HED-3, so the store does not call the validator (PUB-ACT-3).
ACTIVATION_REASONS = frozenset({'activation_required', 'activation_mismatch', 'lock_has_snapshot',
                                'lock_outside_snapshot', 'change_outside_lock', 'removal_refused'})


def validator_problems(operation: str, request: dict, expect: dict, calls: list) -> list[str]:
    """The store calls the validator only in step 4 of PUB-HED-3, with the pair of the request (PUB-RCP-2, PUB-HED-5).

    An ok publish with disposition `published` and an `incompatible` publish pass step 4, so they call it.
    Each other step does not call it. A publish that the validator did not accept is not `published`.
    The activation checks come before step 4, so a publish with one of their reasons does not call it (PUB-ACT-3).
    A vector declares each gate before its first step, so no gate starts while a validator runs.
    Each call has the activation of the request, or None if the request has none (PUB-RCP-2).
    """
    item = expected_item(operation, expect)
    published = item.get('fields', {}).get('disposition') == 'published'
    gated = bool(ACTIVATION_REASONS & set(item.get('reasons', [])))
    if gated or not (item.get('status') == 'incompatible' or published):
        if calls:
            clause = 'PUB-ACT-3' if gated else 'PUB-HED-3, PUB-HED-5'
            return [f'the store called the validator {len(calls)} time(s), but this step does not call it ({clause})']
        return []
    pair = (request.get('candidate'), request.get('receipt'))
    if not calls:
        return ['the store did not call the validator of the policy (PUB-RCP-2)']
    if any(call[:2] != pair for call in calls):
        return [f'the store called the validator with {[list(c[:2]) for c in calls]}, not with the pair of the request {list(pair)} (PUB-RCP-2)']
    if any(call[3] != request.get('activation') for call in calls):
        return [f'the store called the validator with the activation {[c[3] for c in calls]}, not with {request.get("activation")} (PUB-RCP-2)']
    accepted = calls[-1][2]
    if published and not accepted:
        return ['the validator did not accept the receipt, but the store published the candidate (PUB-RCP-2)']
    if 'receipt_rejected' in item.get('reasons', []) and accepted:
        return ['the validator accepted the receipt, but the result has receipt_rejected (PUB-HED-3 step 4)']
    if 'head_moved' in item.get('reasons', []) and not accepted:
        return ['the validator did not accept the receipt, but the result has head_moved (PUB-HED-3 steps 4 and 5)']
    return []


def run_vector(adapter, vid: str, vector: dict) -> list[str]:
    """Run each step of one vector. Return the problems of the first step that fails."""
    try:
        vector = check_spec.with_fixture(vector)
    except ValueError as e:
        raise VectorError(f'{vid}: {e}') from None
    adapter.reset()
    expander = Expander(vid, vector)
    profiles = check_spec.reserved()
    complete = complete_operations()
    calls = []  # the validator calls of the current step (PUB-RCP-2)
    if 'acceptance' in vector:
        register(adapter, expander, vector['acceptance'], calls)
    for collection in vector.get('gates', []):
        try:
            adapter.gate(collection)  # PUB-GAT-1: before the first call
        except Exception as e:  # noqa: BLE001  a missing gate fails this vector only
            return [f'the adapter did not declare the gated collection {collection}: {e!r} (PUB-GAT-1)']
    for collection, section in vector.get('sections', {}).items():
        try:
            adapter.section(collection, section)  # PUB-SEC-1: before the first call
        except Exception as e:  # noqa: BLE001  a missing declaration fails this vector only
            return [f'the adapter did not declare the section of {collection}: {e!r} (PUB-SEC-1)']
    kept = {s: set() for s in check_spec.STORES}  # the revisions that each store has, for the lock check (LCK-EXE-4, LCK-BLD-3)
    known = {s: set() for s in check_spec.STORES}  # the candidate identities that each store has (PUB-CND-4)
    for n, step in enumerate(vector['steps'], 1):
        if step.get('fresh_store'):
            adapter.reset()
            expander.contexts.clear()
            for s in check_spec.STORES:
                kept[s].clear()
                known[s].clear()
        operation = step['operation']
        store = step.get('store', 'writer')
        lock_store = step.get('lock_store', store)
        stored, candidates = kept[store], known[store]
        request, names = expander.request(operation, step.get('request', {}))
        expect = expander.value(step['expect'], {})
        prints = {s: adapter.fingerprint(store=s) for s in check_spec.STORES}
        before = prints[store]
        calls.clear()
        try:
            actual = adapter.call(operation, copy.deepcopy(request), store=store)  # the checks use the request of the vector
        except Exception as e:  # noqa: BLE001  STS-MAL-1: no raw exception crosses the API
            return [f'step {n} ({operation}): the adapter raised {type(e).__name__}: {e}']
        problems = compare(expect, actual, operation)
        if isinstance(actual, dict):
            problems += context_problems(operation, step, request, expect, actual)
            problems += bundle_problems(operation, request, expect, actual, expander.stored)
            problems += lock_problems(operation, request, expect, actual)
            problems += value_problems(operation, actual)
            problems += revision_problems(operation, request, expect, actual)
            problems += created_problems(operation, request, expect, actual)
            problems += reserved_problems(operation, expect, actual, profiles)
            problems += complete_problems(operation, actual, complete)
            problems += render_problems(operation, request, actual)
        problems += validator_problems(operation, request, expect, calls)
        if expect.get('store_unchanged'):
            problems += [f'the {s} store changed (STS-MAL-2)' for s in check_spec.STORES if adapter.fingerprint(store=s) != prints[s]]
        if operation in LOCK_OPERATIONS and expect['status'] == 'ok' and not problems:
            if lock_store != store and adapter.fingerprint(store=store) != prints[store]:
                problems.append(f'the {store} store changed, but the lock goes to the {lock_store} store (BUN-IMP-7)')
            if actual['lock'] not in kept[lock_store] and adapter.fingerprint(store=lock_store) == prints[lock_store]:
                problems.append(f'the {lock_store} store did not change, so it has no lock (LCK-EXE-4, LCK-BLD-3)')
            problems += stored_lock_problems(adapter, operation, request, actual['lock'], lock_store)
            kept[lock_store].add(actual['lock'])
        published = expected_item(operation, expect).get('fields', {}).get('disposition') == 'published'
        if published and not problems and adapter.fingerprint(store=store) == before:
            problems.append('the store did not change, so it did not record the publication (PUB-HED-3)')
        if operation in CREATE_OPERATIONS and expect['status'] == 'ok' and not problems:
            value = created(operation, request)[1]
            if value not in (stored if operation == 'create_snapshot' else candidates) and adapter.fingerprint(store=store) == before:
                problems.append('the store did not change, so it did not write the new record (PUB-SNP-3, PUB-CND-4)')
        if problems:
            return [f'step {n} ({operation}): {p}' for p in problems]
        if operation in ('put', 'import') and expect['status'] == 'ok' and isinstance(actual.get('items'), list):
            stored.update(i['ref'] for i in actual['items'] if isinstance(i, dict) and isinstance(i.get('ref'), str))
        if expect['status'] != 'ok':
            continue
        if 'as_context' in step:
            if not isinstance(actual.get('context'), str):
                return [f'step {n} ({operation}): no context identity for handle {step["as_context"]}']
            expander.contexts[step['as_context']] = actual['context']
        if 'as_bundle' in step:
            if not isinstance(actual.get('bundle'), dict):
                return [f'step {n} ({operation}): no bundle for handle {step["as_bundle"]} (BUN-EXP-2)']
            expander.bundles[step['as_bundle']] = actual['bundle']
        if operation in CREATE_OPERATIONS:
            field, value = created(operation, request)  # created_problems() checked it.
            if operation == 'create_snapshot':
                expander.stored[value] = json.dumps(snapshot_descriptor(request), sort_keys=True)
                stored.add(value)
            else:
                candidates.add(value)
            if 'as_ref' in step:
                expander.refs[step['as_ref']] = value
        for i, name in enumerate(names):
            if name is None:
                continue
            ref = actual['items'][i]['ref']  # revision_problems() checked it (IDN-CAN-2).
            expander.refs[name] = ref
            expander.stored[ref] = request['descriptors'][i]
    return []


def load_adapter(spec: str):
    module, _, attr = spec.partition(':')
    sys.path[:0] = [str(HERE), str(Path.cwd())]
    return getattr(importlib.import_module(module), attr or 'Adapter')()


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--adapter', required=True, help='module:class of the adapter')
    parser.add_argument('--vectors', type=Path, default=VECTORS)
    parser.add_argument('ids', nargs='*', help='run only these vector IDs')
    args = parser.parse_args(argv)
    try:
        invalid = check_spec.check(args.vectors)
        if invalid:
            raise VectorError('the spec or the vectors have problems; run tools/check_spec.py:\n  '
                              + '\n  '.join(invalid))
        vectors = load(args.vectors)
        unknown = set(args.ids) - set(vectors)
        if unknown:
            raise VectorError(f'unknown vector IDs: {sorted(unknown)}')
        adapter = load_adapter(args.adapter)
        failed = 0
        for vid, vector in vectors.items():
            if args.ids and vid not in args.ids:
                continue
            problems = run_vector(adapter, vid, vector)
            print(f'{"FAIL" if problems else "PASS"} {vid}')
            for p in problems:
                print(f'  {p}')
            failed += bool(problems)
    except (VectorError, KeyError) as e:
        print(f'ERROR {e}')
        return 2
    total = len(args.ids) or len(vectors)
    print(f'{total - failed} passed, {failed} failed')
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
