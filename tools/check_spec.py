"""Check the SPEC.md index, the clause IDs, the registry tables and the conformance vectors."""
from __future__ import annotations

import base64
import binascii
import hashlib
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
STATES = {'stub', 'draft', 'normative'}
INDEX_ROW = re.compile(r'^\| ([A-Z]{3}) \| \[[^\]]+\]\((docs/contracts/[^)]+\.md)\) \|')
HEADER = re.compile(r'^State: (\w+)\. Prefix: `([A-Z]{3})`\.', re.M)
TOPIC_ROW = re.compile(r'^\| `([A-Z]{2,4})` \|', re.M)
CLAUSE = re.compile(r'^\*\*([A-Z]{3})-([A-Z]{2,4})-([1-9][0-9]*)\.\*\*', re.M)
LOOSE_ID = re.compile(r'\b[A-Z]{3}-[A-Z]{2,4}-[0-9]+\b')
REGISTRY = ROOT / 'docs/contracts/02-kinds-and-typed-references.md'
COUNTS = {'1', '0..1', '0..n', '1..n', '0..n list', '1..n list'}
REF_TYPES = {'ref', 'ref-map'}
FORBIDDEN_FIELDS = {'refs', 'dependencies'}
STATUSES = ROOT / 'docs/contracts/03-statuses-and-reasons.md'
RESERVED = ROOT / 'docs/contracts/16-reserved-profiles.md'
RESERVED_PROFILE = re.compile(r'^aavanam/[a-z][a-z-]*/v[1-9][0-9]*$')
STATUS_SET = ['ok', 'partial', 'not_found', 'unresolved', 'ambiguous', 'incompatible', 'unsupported', 'failed']
CODE_LIST = r'((?:`[a-z_]+`(?:,? (?:and|or) |, )?)+)'
CITED_PAIR = re.compile(r'status (?:is )?`([a-z_]+)`,? (?:and|with) reasons? ' + CODE_LIST)
CITED_REASON = re.compile(r'reasons? ' + CODE_LIST)
CODE = re.compile(r'`([a-z_]+)`')
SEPARATOR = re.compile(r'^\|(\s*:?-+:?\s*\|)+$')
WITHDRAWN = re.compile(r'^\*\*([A-Z]{3}-[A-Z]{2,4}-[1-9][0-9]*)\.\*\* \(withdrawn\)', re.M)
VECTORS = ROOT / 'conformance/vectors'
FIXTURES = ROOT / 'fixtures'
FIXTURE_FIELDS = {'format', 'collection', 'root', 'payloads', 'descriptors'}
REQUIRED_COUNTS = {'1', '1..n', '1..n list'}
FIXTURE_EXCLUDED_KINDS = {'supersession'}
VECTOR_FIELDS = {'format', 'title', 'clauses', 'provenance', 'profile', 'fixture', 'payloads', 'steps', 'pair', 'acceptance', 'gates', 'sections'}
PROVENANCE = {'spec', 'spike', 'fault'}
EXPECT_FIELDS = {'status', 'reasons', 'items', 'store_unchanged', 'unordered', 'reason_details'}
UNORDERED_OPERATIONS = {'evidence', 'observations', 'coverage', 'execution_inputs', 'build_inputs', 'export', 'import', 'render'}  # REL-EVD-4, OBS-SUB-4, OBS-COV-3, LCK-EXE-4, LCK-BLD-3, BUN-EXP-3, BUN-IMP-3, RND-DOC-5
ITEM_FIELDS = {'status', 'reasons', 'fields', 'reason_details'}
STEP_FIELDS = {'operation', 'request', 'expect', 'as_context', 'as_bundle', 'as_ref', 'fresh_store', 'store', 'lock_store'}
STORES = ('writer', 'device', 'output')  # the stores of one vector (conformance/README.md)
PRECEDENCE = ['failed', 'unsupported', 'incompatible', 'ambiguous', 'unresolved', 'not_found']
EXT_CODE = re.compile(r'^ext:[^/]+/[^/]+/v[1-9][0-9]*#[a-z_]+$')


def cells(line: str) -> list[str]:
    return [c.strip().replace('`', '') for c in line.strip().strip('|').split('|')]


def table(text: str, lead: str, width: int, name: str, errors: list[str]) -> list[list[str]]:
    """Return the body rows of the table after the line `lead`, with backticks removed.

    The table ends at the first blank line. Add an error to `errors` if the
    table is missing, if its header or separator line does not have `width`
    cells, or if a row has no leading pipe or a number of cells other than `width`.
    """
    where = f'{name}: the table after "{lead}"'
    lines = text.splitlines()
    if lead not in lines:
        errors.append(f'{name}: no table after "{lead}"')
        return []
    block = []
    for line in lines[lines.index(lead) + 1:]:
        if not line.strip():
            if block:
                break
            continue
        if not line.startswith('|'):
            if not block:
                break
            errors.append(f'{where} has a row with no leading pipe: {line}')
            continue
        block.append(line)
    if (len(block) < 2 or not SEPARATOR.match(block[1].strip())
            or len(cells(block[0])) != width or len(cells(block[1])) != width):
        errors.append(f'{where} has no header of {width} cells and separator line')
        return []
    rows = []
    for line in block[2:]:
        row = cells(line)
        if len(row) != width:
            errors.append(f'{where} has a row with {len(row)} cells, not {width}: {line}')
            continue
        rows.append(row)
    if not rows:
        errors.append(f'{where} has no rows')
    return rows


def check_registry() -> list[str]:
    if not REGISTRY.exists():
        return []
    text = REGISTRY.read_text()
    name = REGISTRY.name
    errors = []
    kinds = set()
    for row in table(text, 'The kind table:', 3, name, errors):
        if row[0] in kinds:
            errors.append(f'{name}: kind {row[0]} is listed twice')
        kinds.add(row[0])
    types = {row[0] for row in table(text, 'The value types:', 2, name, errors)}
    groups = {}
    for row in table(text, 'The target groups:', 2, name, errors):
        if row[0] in kinds:
            errors.append(f'{name}: group {row[0]} has the name of a kind')
        if row[0] in groups:
            errors.append(f'{name}: group {row[0]} is listed twice')
        groups[row[0]] = {k.strip() for k in row[1].split(',')}
        for k in groups[row[0]] - kinds:
            errors.append(f'{name}: group {row[0]} names unknown kind {k}')
    common = set()
    for field, vtype, count, _ in table(text, 'The common fields:', 4, name, errors):
        where = f'{name}: common field {field}'
        if field in common:
            errors.append(f'{where} is listed twice')
        common.add(field)
        if field in FORBIDDEN_FIELDS:
            errors.append(f'{where}: a second dependency list is not permitted')
        if vtype in REF_TYPES:
            errors.append(f'{where}: a common field cannot be a reference, because it has no targets')
        elif vtype not in types:
            errors.append(f'{where} has unknown type {vtype}')
        if count not in COUNTS:
            errors.append(f'{where} has unknown count {count}')
    fields = set()
    lead = 'The field table. The targets column applies only to the value types `ref` and `ref-map`.'
    for kind, field, vtype, count, targets in table(text, lead, 5, name, errors):
        where = f'{name}: {kind}.{field}'
        if kind not in kinds:
            errors.append(f'{where}: kind is not registered')
        if (kind, field) in fields:
            errors.append(f'{where}: field is listed twice')
        fields.add((kind, field))
        if field in common:
            errors.append(f'{where}: the field has the name of a common field')
        if field in FORBIDDEN_FIELDS:
            errors.append(f'{where}: a second dependency list is not permitted')
        if vtype not in types:
            errors.append(f'{where}: unknown value type {vtype}')
        if count not in COUNTS:
            errors.append(f'{where}: unknown count {count}')
        if vtype in REF_TYPES:
            for target in (x.strip() for x in targets.split(',')):
                if target not in kinds and target not in groups:
                    errors.append(f'{where}: unknown target {target}')
        elif targets != '-':
            errors.append(f'{where}: only ref and ref-map fields have targets')
    for kind in sorted(kinds - {k for k, _ in fields}):
        errors.append(f'{name}: kind {kind} has no fields')
    return errors


def schemas() -> dict[str, dict[str, tuple[str, str]]]:
    """Return the value type and count of each field of each kind, with the common fields under the key ''."""
    text, out = REGISTRY.read_text(), {'': {}}
    for field, vtype, count, _ in table(text, 'The common fields:', 4, REGISTRY.name, []):
        out[''][field] = (vtype, count)
    lead = 'The field table. The targets column applies only to the value types `ref` and `ref-map`.'
    for kind, field, vtype, count, _ in table(text, lead, 5, REGISTRY.name, []):
        out.setdefault(kind, {})[field] = (vtype, count)
    return out


def check_statuses(docs: list[Path]) -> list[str]:
    if not STATUSES.exists():
        return []
    text = STATUSES.read_text()
    name = STATUSES.name
    errors = []
    statuses = [row[0] for row in table(text, 'The status table:', 2, name, errors)]
    if statuses != STATUS_SET:
        errors.append(f'{name}: the status table is not the closed set {STATUS_SET}')
    reasons = {}
    for code, status, _ in table(text, 'The reason table:', 3, name, errors):
        if code in reasons:
            errors.append(f'{name}: reason {code} is listed twice')
        if status not in STATUS_SET:
            errors.append(f'{name}: reason {code} has unknown status {status}')
        reasons[code] = status
    ops = set()
    for op, _, listed in table(text, 'The operation table:', 3, name, errors):
        if op in ops:
            errors.append(f'{name}: operation {op} is listed twice')
        ops.add(op)
        for s in (x.strip() for x in listed.split(',')):
            if s not in STATUS_SET:
                errors.append(f'{name}: operation {op} has unknown status {s}')
    for path in docs:
        if not path.exists():
            continue
        body = ' '.join(path.read_text().split())
        for status, listed in CITED_PAIR.findall(body):
            for code in CODE.findall(listed):
                if code in reasons and reasons[code] != status:
                    errors.append(f'{path.name}: reason {code} has status {reasons[code]}, not {status}')
        for code in {c for listed in CITED_REASON.findall(body) for c in CODE.findall(listed)}:
            if code not in reasons:
                errors.append(f'{path.name}: reason {code} is not in the reason table')
    return errors


def reserved(errors: list[str] | None = None) -> dict[str, str]:
    """Return the profile of each reserved operation in the reserved profile table (RSV-LST-1)."""
    errors = [] if errors is None else errors
    out, profiles = {}, set()
    for profile, _, listed in table(RESERVED.read_text(), 'The reserved profile table:', 3, RESERVED.name, errors):
        if not RESERVED_PROFILE.match(profile) or profile == 'aavanam/base/v1':
            errors.append(f'{RESERVED.name}: {profile} is not a reserved profile name')
        if profile in profiles:
            errors.append(f'{RESERVED.name}: profile {profile} is listed twice')
        profiles.add(profile)
        for op in (x.strip() for x in listed.split(',')):
            if op in out:
                errors.append(f'{RESERVED.name}: operation {op} is listed twice (RSV-LST-2)')
            out[op] = profile
    return out


def check_reserved() -> list[str]:
    """Each reserved operation has a row in document 03 with document 16 and only `unsupported` (RSV-LST-2)."""
    if not (RESERVED.exists() and STATUSES.exists()):
        return []
    errors = []
    ops = reserved(errors)
    rows = {op: (doc, {s.strip() for s in listed.split(',')})
            for op, doc, listed in table(STATUSES.read_text(), 'The operation table:', 3, STATUSES.name, [])}
    for op in sorted(ops):
        if rows.get(op) != ('16', {'unsupported'}):
            errors.append(f'{RESERVED.name}: {op} has no row in document 03 with document 16 and only unsupported (RSV-LST-2)')
    for op, (doc, listed) in sorted(rows.items()):
        if (doc == '16' or listed == {'unsupported'}) and op not in ops:
            errors.append(f'{STATUSES.name}: {op} is reserved but not in the reserved profile table (RSV-LST-2)')
    return errors


def handles(value, found: dict[str, set]) -> None:
    """Add each $ref, $context, $bundle and $payload name in `value` to `found`. Add a name that is not a string to `bad`."""
    if isinstance(value, list):
        for x in value:
            handles(x, found)
    elif isinstance(value, dict):
        for key in ('$ref', '$context', '$bundle', '$payload'):
            if key in value:
                found[key if isinstance(value[key], str) else 'bad'].add(str(value[key]))
        for x in value.values():
            handles(x, found)


def is_codes(value) -> bool:
    return isinstance(value, list) and all(isinstance(c, str) for c in value)


def is_entry(value) -> bool:
    return isinstance(value, dict) and isinstance(value.get('status'), str) and is_codes(value.get('reasons'))


# The reasons of the whole-operation outcomes that STS-ENV-3 rule 1 names, with STS-MAL-4
WHOLE_OUTCOMES = {'context_expired', 'deadline_exceeded', 'storage_error', 'unsupported_operation', 'reserved_profile',
                  'read_only_store'}


def check_reduction(where: str, operation: str, status: str, codes: list[str], items: list[dict],
                    errors: list[str], permits_partial: bool = False) -> None:
    """Check that the expected envelope agrees with its expected items (STS-ENV-3, STS-ENV-5)."""
    if status == 'ok' and any(i['status'] != 'ok' for i in items):
        errors.append(f'{where}: an ok envelope has an item that is not ok (STS-ENV-3)')
    if operation == 'put' and status != 'ok' and any(i['status'] == 'ok' for i in items):
        errors.append(f'{where}: a put that is not ok has an ok item (STS-ENV-5)')
    if status == 'partial':
        not_ok = any(i['status'] != 'ok' for i in items)
        want = {c for c, on in (('bounds_reached', 'bounds_reached' in codes), ('items_not_ok', not_ok)) if on}
        if not any(i['status'] == 'ok' for i in items) or not want or set(codes) != want:
            errors.append(f'{where}: a partial envelope with these items has the reasons {sorted(want)} '
                          f'and one or more ok items, not {sorted(codes)} (STS-ENV-3 rules 2 and 5)')
        return
    counted = [i for i in items if i['status'] != 'ok' and i['reasons'] != ['batch_aborted']]
    item_codes = {c for i in items for c in i['reasons']}
    # rule 1: the items do not change a whole-operation outcome. The base profile does not give the scope of an
    # extension reason (STS-RSN-2), so it can be one
    if status in ('ok', 'partial') or all(c in WHOLE_OUTCOMES or EXT_CODE.match(c) for c in codes):
        return
    if permits_partial and any(i['status'] == 'ok' for i in items):
        # an ok item shows that the operation completed, so rule 1 does not apply
        errors.append(f'{where}: the items give status partial with reason items_not_ok, '
                      f'not {status} with {sorted(codes)} (STS-ENV-3 rule 5)')
        return
    if not set(codes) <= item_codes:
        return  # rule 1, for example a malformed request (STS-MAL-1)
    if not counted:
        errors.append(f'{where}: the envelope reasons come only from batch_aborted items (STS-ENV-5)')
        return
    first = next(s for s in PRECEDENCE if any(i['status'] == s for i in counted))
    want = sorted(c for i in counted if i['status'] == first for c in i['reasons'])
    if status != first or sorted(codes) != want:
        errors.append(f'{where}: the items give status {first} with reasons {want}, '
                      f'not {status} with {sorted(codes)} (STS-ENV-3 rule 6)')


def check_expect(where: str, expect, ops: dict[str, set[str]], reasons: dict[str, str], operation: str,
                 errors: list[str]) -> str | None:
    """Check one expected envelope against the status, reason and operation tables. Return its status."""
    if not is_entry(expect):
        errors.append(f'{where}: expect needs a status and a list of reason codes')
        return None
    for f in sorted(set(expect) - EXPECT_FIELDS):
        errors.append(f'{where}: unknown expect field {f}')
    for f in ('store_unchanged', 'unordered'):
        if not isinstance(expect.get(f, False), bool):
            errors.append(f'{where}: {f} is not true or false')
    if expect.get('unordered') is True and operation not in UNORDERED_OPERATIONS:
        errors.append(f'{where}: the clauses of {operation} give an item order, so it cannot be unordered')
    status = expect['status']
    items = expect.get('items')
    if 'items' in expect and not (isinstance(items, list) and all(is_entry(i) for i in items)):
        errors.append(f'{where}: items is not a list of objects with a status and a list of reason codes')
        items = None
    for n, item in enumerate(items or []):
        for f in sorted(set(item) - ITEM_FIELDS):
            errors.append(f'{where} item {n}: unknown item field {f}')
        if not isinstance(item.get('fields', {}), dict):
            errors.append(f'{where} item {n}: fields is not a JSON object')
        check_details(f'{where} item {n}', item, errors)
    check_details(where, expect, errors)
    entries = [(where, status, expect['reasons'])]
    entries += [(f'{where} item {n}', i['status'], i['reasons']) for n, i in enumerate(items or [])]
    valid = True
    for n, item in enumerate(items or []):
        if item['status'] == 'partial':
            errors.append(f'{where} item {n}: an item cannot be partial (STS-SET-1)')
            valid = False
    for at, s, codes in entries:
        if s not in STATUS_SET:
            errors.append(f'{at}: unknown status {s}')
            valid = False
            continue
        if (s == 'ok') != (not codes):
            errors.append(f'{at}: status {s} with reasons {codes} (STS-RSN-1)')
        for code in codes:
            if EXT_CODE.match(code):
                continue
            if code not in reasons:
                errors.append(f'{at}: reason {code} is not in the reason table')
            elif reasons[code] != s:
                errors.append(f'{at}: reason {code} has status {reasons[code]}, not {s} (STS-RSN-3)')
    if operation in ops:
        if status not in ops[operation]:
            errors.append(f'{where}: {operation} cannot return {status} (STS-SET-3)')
        elif ops[operation] == {'unsupported'} and expect['reasons'] != ['reserved_profile']:
            errors.append(f'{where}: {operation} is reserved, so its reason is reserved_profile (STS-MAL-4)')
    elif (status, expect['reasons']) != ('unsupported', ['unsupported_operation']):
        errors.append(f'{where}: operation {operation} has no row, so it is unsupported (STS-SET-3)')
    if valid and items is not None:
        check_reduction(where, operation, status, expect['reasons'], items, errors,
                        'partial' in ops.get(operation, set()))
    return status


def reason_statuses() -> dict[str, str]:
    """Return the status of each reason code in the reason table (STS-RSN-3)."""
    if not STATUSES.exists():
        return {}
    return {row[0]: row[1] for row in table(STATUSES.read_text(), 'The reason table:', 3, STATUSES.name, [])}


def check_gates(where: str, gates) -> list[str]:
    """The gated collections of a vector are a set of collection names (PUB-GAT-1, KND-SCH-5)."""
    if not (isinstance(gates, list) and gates and all(isinstance(g, str) and g for g in gates)):
        return [f'{where}: gates needs a list of one or more collection names']
    if len(set(gates)) != len(gates):
        return [f'{where}: gates names a collection twice']
    return []


def check_sections(where: str, sections) -> list[str]:
    """The sections of a vector map collection names to the sections of PUB-SEC-1."""
    if not (isinstance(sections, dict) and sections and all(s in ('serving', 'evidence') for s in sections.values())):
        return [f'{where}: sections needs an object from a collection name to serving or evidence']
    return []


def check_acceptance(where: str, acceptance, bound: set[str]) -> list[str]:
    """The test policy of a vector has a name and the accepted pairs of candidate and receipt (PUB-RCP-1)."""
    if not (isinstance(acceptance, dict) and set(acceptance) == {'policy', 'accept'} and isinstance(acceptance['policy'], str)
            and isinstance(acceptance['accept'], list)
            and all(isinstance(p, list) and len(p) == 2 for p in acceptance['accept'])):
        return [f'{where}: acceptance needs a policy and a list of accepted [candidate, receipt] pairs']
    members = [m for p in acceptance['accept'] for m in p]
    if not all(isinstance(m, str) or (isinstance(m, dict) and set(m) == {'$ref'} and isinstance(m['$ref'], str)) for m in members):
        return [f'{where}: each acceptance pair member is a string or a {{"$ref": name}} handle']
    refs = {m['$ref'] for m in members if isinstance(m, dict)}
    return [f'{where}: acceptance handle {h} is not bound by a step' for h in sorted(refs - bound)]


def check_vectors(clauses: set[str], withdrawn: set[str], root: Path = VECTORS) -> list[str]:
    if not STATUSES.exists():
        return []
    if not root.is_dir():
        return [f'{root}: not a folder of vectors']
    errors: list[str] = []
    text = STATUSES.read_text()
    reasons = reason_statuses()
    ops = {row[0]: {s.strip() for s in row[2].split(',')} for row in table(text, 'The operation table:', 3, STATUSES.name, [])}
    vectors, final = {}, {}
    for path in sorted(root.rglob('*.json')):
        vid = path.relative_to(root).with_suffix('').as_posix()
        try:
            vectors[vid] = json.loads(path.read_text())
        except json.JSONDecodeError as e:
            errors.append(f'vector {vid}: not valid JSON: {e}')
    if not vectors:
        errors.append(f'{root}: no vectors')
    for vid, v in vectors.items():
        where = f'vector {vid}'
        if not isinstance(v, dict):
            errors.append(f'{where}: not a JSON object')
            continue
        try:
            v = with_fixture(v)
        except ValueError as e:
            errors.append(f'{where}: {e}')
            continue
        for f in sorted(set(v) - VECTOR_FIELDS):
            errors.append(f'{where}: unknown field {f}')
        if v.get('format') != 'aavanam-vector/v1':
            errors.append(f'{where}: format is not aavanam-vector/v1')
        if v.get('profile') != 'aavanam/base/v1':
            errors.append(f'{where}: profile is not aavanam/base/v1')
        if not isinstance(v.get('title'), str) or not v['title']:
            errors.append(f'{where}: no title')
        cited = v.get('clauses')
        if not is_codes(cited) or not cited:
            errors.append(f'{where}: clauses is not a list of clause IDs')
            cited = []
        for cid in cited:
            if cid not in clauses:
                errors.append(f'{where}: clause {cid} is not defined')
            elif cid in withdrawn:
                errors.append(f'{where}: clause {cid} is withdrawn')
        prov = v.get('provenance')
        if (not isinstance(prov, dict) or not isinstance(prov.get('type'), str) or prov['type'] not in PROVENANCE
                or not isinstance(prov.get('note'), str)):
            errors.append(f'{where}: provenance needs a type from {sorted(PROVENANCE)} and a note')
        elif prov['type'] == 'spike' and not isinstance(prov.get('source'), str):
            errors.append(f'{where}: a spike vector needs a provenance source')
        payloads = v.get('payloads', {})
        if not isinstance(payloads, dict) or not all(
                isinstance(p, dict) and isinstance(p.get('media_type'), str) and isinstance(p.get('stored', True), bool)
                and (isinstance(p.get('text'), str)) != ('base64' in p) and ('base64' not in p or is_base64(p['base64']))
                for p in payloads.values()):
            errors.append(f'{where}: each payload needs media_type and one of text or base64')
            payloads = {}
        steps = v.get('steps')
        if not isinstance(steps, list) or not steps:
            errors.append(f'{where}: no steps')
            continue
        bound: set[str] = set()
        contexts: dict[str, str] = {}  # each context handle and the store that binds it
        bundles: set[str] = set()
        for n, step in enumerate(steps, 1):
            at = f'{where} step {n}'
            if not isinstance(step, dict) or not isinstance(step.get('operation'), str):
                errors.append(f'{at}: no operation')
                final[vid] = None
                continue
            for f in sorted(set(step) - STEP_FIELDS):
                errors.append(f'{at}: unknown step field {f}')
            if 'as_ref' in step and step['operation'] not in ('create_snapshot', 'create_candidate'):
                errors.append(f'{at}: only create_snapshot and create_candidate have as_ref')
            for f in ('as_context', 'as_bundle', 'as_ref'):
                if not isinstance(step.get(f, ''), str):
                    errors.append(f'{at}: {f} is not a string')
            expect = step.get('expect') if isinstance(step.get('expect'), dict) else {}
            if step['operation'] == 'publish' and expect.get('status') == 'ok':
                items = expect.get('items')
                fields = items[0].get('fields') if isinstance(items, list) and len(items) == 1 and isinstance(items[0], dict) else None
                if not (isinstance(fields, dict) and fields.get('disposition') in ('published', 'already_published', 'superseded')
                        and 'head' in fields):
                    errors.append(f'{at}: an ok publish expects one item with a disposition and a head (PUB-HED-4)')
            if not isinstance(step.get('fresh_store', False), bool):
                errors.append(f'{at}: fresh_store is not true or false')
            if step.get('fresh_store') is True:
                contexts.clear()
            store = step.get('store', 'writer')
            for f in ('store', 'lock_store'):
                if step.get(f, 'writer') not in STORES:
                    errors.append(f'{at}: {f} is not one of {", ".join(STORES)}')
            if 'lock_store' in step and step['operation'] not in ('execution_inputs', 'build_inputs'):
                errors.append(f'{at}: only execution_inputs and build_inputs have lock_store')
            elif step['operation'] in ('execution_inputs', 'build_inputs') and \
                    step.get('lock_store', store) != ('output' if store == 'device' else store):
                errors.append(f'{at}: the lock of a step on the {store} store goes to the '
                              f'{"output" if store == "device" else store} store (BUN-IMP-7)')
            op, request = step['operation'], step.get('request', {})
            if not isinstance(request, dict):
                errors.append(f'{at}: request is not a JSON object')
                request = {}
            found: dict[str, set] = {'$ref': set(), '$context': set(), '$bundle': set(), '$payload': set(), 'bad': set()}
            handles([request, step.get('expect')], found)
            for h in sorted(found['bad']):
                errors.append(f'{at}: handle or payload name {h} is not a string')
            for h in sorted(found['$ref'] - bound):
                errors.append(f'{at}: handle {h} is not bound by an earlier step')
            for h in sorted(found['$context'] - set(contexts)):
                errors.append(f'{at}: context handle {h} is not bound by an earlier step')
            for h in sorted(h for h in found['$context'] & set(contexts) if contexts[h] != store):
                errors.append(f'{at}: context handle {h} is bound in the {contexts[h]} store, not in the {store} store')
            for h in sorted(found['$bundle'] - bundles):
                errors.append(f'{at}: bundle handle {h} is not bound by an earlier step')
            for p in sorted(found['$payload'] - set(payloads)):
                errors.append(f'{at}: payload {p} is not declared')
            expect = step.get('expect')
            status = check_expect(at, expect, ops, reasons, op, errors)
            if op == 'put':
                entries = request.get('descriptors')
                if not isinstance(entries, list) or not entries or not all(
                        isinstance(e, dict) and len({'value', 'raw'} & set(e)) == 1
                        and isinstance(e.get('as', ''), str) and isinstance(e.get('raw', ''), str)
                        for e in entries):
                    errors.append(f'{at}: put needs descriptors, each with one of value or raw, and a string as')
                    entries = []
                items = expect.get('items') if isinstance(expect, dict) else None
                refused = isinstance(expect, dict) and expect.get('reasons') == ['read_only_store']
                if refused and items != []:
                    errors.append(f'{at}: a put that gives read_only_store has no items (BUN-IMP-6)')
                elif not refused and (not isinstance(items, list) or len(items) != len(entries)):
                    errors.append(f'{at}: put needs one expected item for each descriptor')
                if status == 'ok':
                    bound |= {e['as'] for e in entries if 'as' in e}
            if status == 'ok' and isinstance(step.get('as_ref'), str):
                bound.add(step['as_ref'])
            if status == 'ok' and isinstance(step.get('as_context'), str):
                contexts[step['as_context']] = store
            if status == 'ok' and isinstance(step.get('as_bundle'), str):
                bundles.add(step['as_bundle'])
            if n < len(steps) and status != 'ok':
                errors.append(f'{at}: only the last step can expect a status other than ok')
            final[vid] = status
        if 'acceptance' in v:
            errors += check_acceptance(where, v['acceptance'], bound)
        if 'gates' in v:
            errors += check_gates(where, v['gates'])
        if 'sections' in v:
            errors += check_sections(where, v['sections'])
    for vid, v in vectors.items():
        if not isinstance(v, dict) or final.get(vid) is None:
            continue
        pair = v.get('pair')
        if final[vid] == 'ok':
            if pair is not None:
                errors.append(f'vector {vid}: only a refusal vector has a pair')
        elif not isinstance(pair, str) or pair not in vectors:
            errors.append(f'vector {vid}: a refusal vector needs a pair that names a vector')
        elif final.get(pair) != 'ok':
            errors.append(f'vector {vid}: its pair {pair} does not end with ok')
    return errors


def with_fixture(vector: dict, root: Path = FIXTURES) -> dict:
    """Return the vector with the put steps and payloads of its fixtures before its own (conformance/README.md)."""
    if 'fixture' not in vector:
        return vector
    if not isinstance(vector.get('steps'), list) or not vector['steps']:
        raise ValueError('a scenario needs its own steps')
    if not isinstance(vector.get('payloads', {}), dict):
        raise ValueError('payloads is not a JSON object')
    names = vector['fixture']
    if isinstance(names, list):
        if len(names) < 2 or not all(isinstance(n, str) for n in names) or len(set(names)) != len(names):
            raise ValueError('a list of fixtures has two or more different names')
        loaded = [fixture_steps(n, root, f'{n}:') for n in names]
    else:
        loaded = [fixture_steps(names, root, '')]
    payloads, steps = {}, []
    for p, s in loaded:
        payloads.update(p)
        steps += s
    own = vector.get('payloads', {})
    for clash in sorted(set(own) & set(payloads)):
        raise ValueError(f'payload {clash} is also a payload of a fixture')
    return {**vector, 'payloads': {**payloads, **own}, 'steps': steps + vector['steps']}


def fixture_steps(name, root: Path, prefix: str) -> tuple[dict, list]:
    """Return the payloads and the put steps of one fixture. Each handle and payload name gets the prefix."""
    folder = root / name if isinstance(name, str) and re.fullmatch(r'[a-z0-9-]+', name) else None
    if folder is None or not (folder / 'fixture.json').is_file():
        raise ValueError(f'fixture {name!r} is not a folder in fixtures/')
    try:
        fixture = json.loads((folder / 'fixture.json').read_text())
        payloads = {prefix + p: {'media_type': d['media_type'], 'text': (folder / d['file']).read_bytes().decode('utf-8')}
                    for p, d in fixture['payloads'].items()}
        entries = [(prefix + e['as'], e['value']) for e in fixture['descriptors']]
    except (KeyError, TypeError, AttributeError, OSError) as e:
        raise ValueError(f'fixture {name} is not valid ({type(e).__name__}); check_fixtures gives the problems') from None

    def expand(value, path: str):
        if is_handle(value, '$text'):
            if path in payloads:
                raise ValueError(f'the payload name {path} of a $text handle is also a fixture payload')
            payloads[path] = {'media_type': value['media_type'], 'text': value['$text']}
            return {'$payload': path}
        if is_handle(value, '$ranges'):
            data, part = payloads[prefix + value['$ranges']]['text'].encode(), value['text'].encode()
            if data.count(part) != 1:
                raise ValueError(f'the text of $ranges in {path} does not occur one time')
            return [{'start': data.find(part), 'end': data.find(part) + len(part)}]
        for mark in ('$ref', '$payload'):
            if is_handle(value, mark):
                return {**value, mark: prefix + value[mark]}
        if isinstance(value, dict):
            return {k: expand(v, f'{path}{"." if "#" in path else "#"}{k}') for k, v in value.items()}
        if isinstance(value, list):
            return [expand(v, f'{path}.{n}') for n, v in enumerate(value)]
        return value

    try:
        steps = [{'operation': 'put', 'request': {'descriptors': [{'as': handle, 'value': expand(value, handle)}]},
                  'expect': {'status': 'ok', 'reasons': [], 'items': [{'status': 'ok', 'reasons': []}]}}
                 for handle, value in entries]
    except (KeyError, TypeError, AttributeError) as e:
        raise ValueError(f'fixture {name} is not valid ({type(e).__name__}); check_fixtures gives the problems') from None
    return payloads, steps


def targets() -> dict[tuple[str, str], set[str]]:
    """Return the target kinds of each ref and ref-map field, with the groups expanded."""
    text = REGISTRY.read_text()
    groups = {row[0]: {k.strip().strip('`') for k in row[1].split(',')}
              for row in table(text, 'The target groups:', 2, REGISTRY.name, [])}
    lead = 'The field table. The targets column applies only to the value types `ref` and `ref-map`.'
    out = {}
    for kind, field, vtype, _, names in table(text, lead, 5, REGISTRY.name, []):
        if vtype in REF_TYPES:
            out[(kind, field)] = set().union(*(groups.get(n, {n}) for n in (x.strip() for x in names.split(','))))
    return out


def is_handle(value, mark: str) -> bool:
    return isinstance(value, dict) and mark in value and isinstance(value[mark], str)


def check_details(where: str, entry: dict, errors: list[str]) -> None:
    """Check the field `reason_details` of an envelope or an item against its reasons."""
    if 'reason_details' not in entry:
        return
    rr = entry['reason_details']
    if not is_reason_details(rr):
        errors.append(f'{where}: reason_details is not a list of objects with a code, and refs or capabilities')
    elif sorted(r['code'] for r in rr) != sorted(entry['reasons']):
        errors.append(f'{where}: the codes of reason_details are not the reasons')
    elif len({frozenset(r) for r in rr}) > 1:
        errors.append(f'{where}: each object of reason_details must have the same fields')


def is_reason_details(value) -> bool:
    """The item field `reason_details`: objects of a code with `refs` (references or `$ref` handles), `capabilities` or both."""
    def is_ref(x):
        return is_handle(x, '$ref') or (isinstance(x, str) and re.fullmatch(r'sha256:[0-9a-f]{64}', x) is not None)
    return isinstance(value, list) and all(
        isinstance(r, dict) and isinstance(r.get('code'), str) and {'code'} < set(r) <= {'code', 'refs', 'capabilities'}
        and all(isinstance(x, list) and all(is_ref(y) for y in x) for x in [r.get('refs', [])])
        and all(isinstance(x, list) and all(isinstance(y, str) for y in x) for x in [r.get('capabilities', [])]) for r in value)


def check_fixture_field(at: str, kind: str, field: str, vtype: str, count: str, value,
                        kind_of: dict, wanted: set[str] | None, identity: dict[str, str]) -> list[str]:
    """Check one fixture field value against its value type and count (KND-SCH-3 to KND-SCH-6, KND-REF-3)."""
    where = f'{at}: field {field}'
    if vtype == 'selector':
        return [f'{where}: a fixture cannot use a selector field, because check_spec.py does not check selectors']
    many = count.startswith(('0..n', '1..n')) and vtype != 'ref-map'
    if vtype == 'ref-map':
        if not isinstance(value, dict):
            return [f'{where} is not one JSON object (KND-SCH-6)']
        values = list(value.values())
    elif many:
        if not isinstance(value, list):
            return [f'{where} is not a list (KND-SCH-3)']
        values = value
    else:
        values = [value]
    errors = []
    if count.startswith('1..n') and not values:
        errors.append(f'{where} is empty (KND-SCH-3)')
    ids = [identity.get(v['$ref'], v['$ref']) if is_handle(v, '$ref') else json.dumps(v, sort_keys=True) for v in values]
    if many and 'list' not in count and len(set(ids)) != len(ids):
        errors.append(f'{where} has a duplicate (KND-SCH-5)')
    for v in values:
        if vtype in REF_TYPES:
            if not is_handle(v, '$ref'):
                errors.append(f'{where}: a reference is not a $ref handle')
            elif wanted is not None and v['$ref'] in kind_of and kind_of[v['$ref']] not in wanted:
                errors.append(f'{where}: {v["$ref"]} is a {kind_of[v["$ref"]]}, not a target of the field (KND-REF-3)')
        elif vtype == 'payload' and not (is_handle(v, '$payload') or is_handle(v, '$text')):
            errors.append(f'{where} is not a $payload or $text handle')
        elif vtype == 'string' and not isinstance(v, str):
            errors.append(f'{where} is not a string (KND-SCH-4)')
    return errors


def no_constant(name: str):
    raise ValueError(f'{name} is not an I-JSON number')


def json_strings(value):
    """Yield each string and each object key of a JSON value."""
    if isinstance(value, str):
        yield value
    elif isinstance(value, dict):
        for k, v in value.items():
            yield k
            yield from json_strings(v)
    elif isinstance(value, list):
        for v in value:
            yield from json_strings(v)


def valid_text(text: str) -> bool:
    """I-JSON (RFC 7493): no lone surrogates and no Unicode noncharacters."""
    try:
        text.encode('utf-8')
    except UnicodeEncodeError:
        return False
    return not any(0xFDD0 <= ord(c) <= 0xFDEF or ord(c) & 0xFFFE == 0xFFFE for c in text)


def is_base64(value) -> bool:
    """A base64 text of RFC 4648 section 4, in its canonical form."""
    try:
        return isinstance(value, str) and base64.b64encode(base64.b64decode(value, validate=True)).decode() == value
    except (binascii.Error, ValueError):
        return False


def payload_object(data: bytes, media_type: str) -> dict:
    return {'digest': 'sha256:' + hashlib.sha256(data).hexdigest(), 'media_type': media_type, 'size': len(data)}


def resolve(value, identity: dict[str, str], objects: dict[str, dict], data: dict[str, bytes]):
    """Return the value with each handle replaced by what the store gets: the identity of a
    descriptor for $ref, the payload object for $payload and $text, and the byte ranges
    for $ranges (BUN-EXC-1). 1.0 becomes 1."""
    if is_handle(value, '$ref'):
        return {'$revision': identity.get(value['$ref'], value['$ref'])}
    if is_handle(value, '$payload'):
        return objects.get(value['$payload'], value)
    if is_handle(value, '$ranges') and isinstance(value.get('text'), str) and value['$ranges'] in data:
        part = value['text'].encode('utf-8', 'replace')
        start = data[value['$ranges']].find(part)
        return [{'start': start, 'end': start + len(part)}] if start >= 0 else value
    if is_handle(value, '$text') and isinstance(value.get('media_type'), str):
        return payload_object(value['$text'].encode('utf-8', 'replace'), value['media_type'])
    if isinstance(value, dict):
        return {k: resolve(v, identity, objects, data) for k, v in value.items()}
    if isinstance(value, list):
        return [resolve(v, identity, objects, data) for v in value]
    if isinstance(value, float) and value.is_integer():
        return int(value)
    return value


def canonical_identity(value: dict, fields: dict) -> str:
    """The canonical form of a resolved descriptor (KND-SCH-3, KND-SCH-5)."""
    out = dict(value)
    for field, (_, count) in fields.items():
        v = out.get(field)
        if isinstance(v, list) and not v and count in ('0..n', '0..n list'):
            del out[field]
        elif isinstance(v, list) and count in ('0..n', '1..n'):
            out[field] = sorted(v, key=lambda x: json.dumps(x, sort_keys=True))
    return json.dumps(out, sort_keys=True)


def unique_pairs(pairs: list) -> dict:
    keys = [k for k, _ in pairs]
    if len(set(keys)) != len(keys):
        raise ValueError('duplicate key')
    return dict(pairs)


def fixture_handles(where: str, value, bound: set[str], texts: dict[str, str | None], errors: list[str]) -> None:
    """Check the handles of a fixture descriptor value: $ref, $payload, $ranges and $text."""
    if isinstance(value, list):
        for v in value:
            fixture_handles(where, v, bound, texts, errors)
        return
    if not isinstance(value, dict):
        return
    marks = [k for k in value if k.startswith('$')]
    if not marks:
        for v in value.values():
            fixture_handles(where, v, bound, texts, errors)
        return
    mark = marks[0]
    if not isinstance(value.get(mark), str):
        errors.append(f'{where}: the handle {mark} does not have a string value')
        return
    shapes = {'$ref': {'$ref'}, '$payload': {'$payload'}, '$ranges': {'$ranges', 'text'}, '$text': {'$text', 'media_type'}}
    if len(marks) > 1 or mark not in shapes or set(value) != shapes[mark]:
        errors.append(f'{where}: bad handle {sorted(value)}')
        return
    name = value[mark]
    if mark == '$ref' and name not in bound:
        errors.append(f'{where}: handle {name} is not bound by an earlier descriptor')
    elif mark in ('$payload', '$ranges') and name not in texts:
        errors.append(f'{where}: payload {name} is not declared')
    elif mark == '$ranges':
        text = texts[name]
        if not isinstance(value['text'], str) or not value['text']:
            errors.append(f'{where}: $ranges needs a text')
        elif text is None or text.count(value['text']) != 1:
            errors.append(f'{where}: the text of $ranges does not occur one time in payload {name}')
    elif mark == '$text' and not (isinstance(name, str) and isinstance(value['media_type'], str)):
        errors.append(f'{where}: $text needs a string and a media_type')


def check_excerpt(at: str, value: dict, kind_of: dict) -> list[str]:
    """The excerpt text must come from the payload of its parent reading (BUN-EXC-3)."""
    ranges, payload, parent = value.get('ranges'), value.get('payload'), value.get('parent')
    if not (is_handle(ranges, '$ranges') and is_handle(payload, '$text')):
        return [f'{at}: an excerpt needs a $ranges handle and a $text payload']
    if payload['$text'] != ranges.get('text'):
        return [f'{at}: the $text of the payload is not the text of $ranges (BUN-EXC-3)']
    reading = kind_of.get(parent['$ref'], (None, {}))[1] if is_handle(parent, '$ref') else {}
    source = reading.get('payload') if isinstance(reading, dict) else None
    if not is_handle(source, '$payload') or source['$payload'] != ranges['$ranges']:
        return [f'{at}: $ranges does not name the payload of the parent reading (BUN-EXC-3)']
    return []


def check_fixtures(root: Path = FIXTURES) -> list[str]:
    """Check each fixture definition against the kind table."""
    if not root.exists():
        return []
    errors, collections, kinds, field_targets = [], {}, schemas(), targets()
    for path in sorted(root.glob('*/fixture.json')):
        where = str(path.relative_to(root.parent))
        try:
            fixture = json.loads(path.read_text(), object_pairs_hook=unique_pairs, parse_constant=no_constant)
        except ValueError as e:
            errors.append(f'{where}: not JSON without duplicate keys: {e}')
            continue
        if not all(valid_text(t) for t in json_strings(fixture)):
            errors.append(f'{where}: a string is not valid I-JSON Unicode (STS-MAL-3)')
        if not isinstance(fixture, dict) or set(fixture) != FIXTURE_FIELDS:
            errors.append(f'{where}: a fixture has exactly the fields {sorted(FIXTURE_FIELDS)}')
            continue
        if fixture['format'] != 'aavanam-fixture/v1':
            errors.append(f'{where}: format is not aavanam-fixture/v1')
        collection = fixture['collection']
        if not isinstance(collection, str) or collection in collections:
            errors.append(f'{where}: collection is not a string, or another fixture uses it')
        collections[collection if isinstance(collection, str) else ''] = where
        texts: dict[str, str | None] = {}
        objects: dict[str, dict] = {}
        data: dict[str, bytes] = {}
        payloads = fixture['payloads'] if isinstance(fixture['payloads'], dict) else {}
        files = set()
        for name, decl in payloads.items():
            if not (isinstance(decl, dict) and set(decl) == {'media_type', 'file'}
                    and isinstance(decl['media_type'], str) and isinstance(decl['file'], str)):
                errors.append(f'{where}: payload {name} needs media_type and file')
                continue
            file = path.parent / decl['file']
            if '/' in decl['file'] or decl['file'].startswith('.') or not file.is_file():
                errors.append(f'{where}: payload {name}: {decl["file"]} is not a file in the fixture folder')
                continue
            files.add(decl['file'])
            data[name] = file.read_bytes()
            objects[name] = payload_object(data[name], decl['media_type'])
            try:
                texts[name] = file.read_bytes().decode('utf-8')
            except UnicodeDecodeError:
                texts[name] = None
        for extra in sorted({f.name for f in path.parent.iterdir()} - files - {'fixture.json', 'README.md'}):
            errors.append(f'{where}: {extra} is not a declared payload')
        bound, kind_of, used, identity = set(), {}, set(), {}
        entries = fixture['descriptors'] if isinstance(fixture['descriptors'], list) else []
        if not entries:
            errors.append(f'{where}: descriptors is not a list of entries')
        for n, entry in enumerate(entries):
            at = f'{where}: descriptor {n}'
            if not (isinstance(entry, dict) and set(entry) == {'as', 'value'} and isinstance(entry['as'], str)):
                errors.append(f'{at}: an entry has exactly the fields as and value')
                continue
            handle, value = entry['as'], entry['value']
            at = f'{where}: descriptor {handle}'
            if handle in bound:
                errors.append(f'{at}: the handle is bound two times')
            kind = value.get('kind') if isinstance(value, dict) else None
            if not isinstance(kind, str) or kind not in kinds or kind == '':
                errors.append(f'{at}: unknown kind {kind!r}')
            else:
                fields = {**kinds[''], **kinds[kind]}
                for field in sorted(set(value) - set(fields)):
                    errors.append(f'{at}: field {field} is not in the schema of {kind} (KND-SCH-2)')
                for field, (vtype, count) in sorted(fields.items()):
                    if count in REQUIRED_COUNTS and field not in value:
                        errors.append(f'{at}: required field {field} is absent (KND-SCH-1)')
                    elif field in value:
                        errors += check_fixture_field(at, kind, field, vtype, count, value[field],
                                                      {h: k for h, (k, _) in kind_of.items()},
                                                      field_targets.get((kind, field)), identity)
                if kind == 'excerpt':
                    errors += check_excerpt(at, value, kind_of)
                if value.get('descriptor_profile') != 'aavanam/base/v1':
                    errors.append(f'{at}: descriptor_profile is not aavanam/base/v1')
            if kind in FIXTURE_EXCLUDED_KINDS:
                errors.append(f'{at}: a fixture cannot use the kind {kind}, because check_spec.py does not check its rules')
            fixture_handles(at, value, bound, texts, errors)
            fields = {**kinds[''], **kinds.get(kind, {})} if isinstance(kind, str) else {}
            identity[handle] = canonical_identity(resolve(value, identity, objects, data), fields) if isinstance(value, dict) else ''
            used |= {m for m in re.findall(r'"\$(?:payload|ranges)": "([^"]+)"', json.dumps(value))}
            bound.add(handle)
            kind_of[handle] = (kind, value)
        for name in sorted(set(payloads) - used):
            errors.append(f'{where}: payload {name} is not used')
        top = fixture['root']
        kind, value = kind_of.get(top, (None, {})) if isinstance(top, str) else (None, {})
        if kind != 'snapshot':
            errors.append(f'{where}: root is not the handle of a snapshot')
        elif value.get('collection') != collection:
            errors.append(f'{where}: the root snapshot has a different collection')
    return errors


def check(vectors: Path = VECTORS) -> list[str]:
    errors = []
    index = {}
    for line in (ROOT / 'SPEC.md').read_text().splitlines():
        m = INDEX_ROW.match(line)
        if m:
            if m[1] in index:
                errors.append(f'SPEC.md: prefix {m[1]} is listed twice')
            index[m[1]] = ROOT / m[2]
    listed = set(index.values())
    for path in sorted((ROOT / 'docs/contracts').glob('*.md')):
        if path not in listed:
            errors.append(f'{path.name}: not listed in SPEC.md')
    seen = {}
    withdrawn = set()
    for prefix, path in index.items():
        if not path.exists():
            errors.append(f'SPEC.md: {path.relative_to(ROOT)} does not exist')
            continue
        text = path.read_text()
        header = HEADER.search(text)
        if not header:
            errors.append(f'{path.name}: no state and prefix line')
            continue
        state, own = header.groups()
        if state not in STATES:
            errors.append(f'{path.name}: unknown state {state}')
        if own != prefix:
            errors.append(f'{path.name}: prefix {own} does not match index prefix {prefix}')
        topics = set(TOPIC_ROW.findall(text))
        clauses = CLAUSE.findall(text)
        withdrawn |= set(WITHDRAWN.findall(text))
        if state == 'stub' and clauses:
            errors.append(f'{path.name}: a stub must not contain clauses')
        for p, topic, n in clauses:
            cid = f'{p}-{topic}-{n}'
            if p != prefix:
                errors.append(f'{path.name}: {cid} uses another document prefix')
            if topic not in topics:
                errors.append(f'{path.name}: {cid} uses an unlisted topic')
            if cid in seen:
                errors.append(f'{path.name}: {cid} is also defined in {seen[cid]}')
            seen[cid] = path.name
    for path in [ROOT / 'SPEC.md', *index.values()]:
        if not path.exists():
            continue
        for cid in set(LOOSE_ID.findall(path.read_text())):
            if cid not in seen and path.name != 'SPEC.md':
                errors.append(f'{path.name}: {cid} is cited but not defined')
    return (errors + check_registry() + check_statuses(list(index.values())) + check_reserved()
            + check_vectors(set(seen), withdrawn, vectors) + check_fixtures())


if __name__ == '__main__':
    problems = check()
    for p in problems:
        print(p)
    print(f'{len(problems)} problem(s)')
    sys.exit(1 if problems else 0)
