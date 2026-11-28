"""The reference implementation over SQLite (constitution principle 8).

A call takes a JSON request and returns the result envelope of STS-ENV-1. Payload bytes in a request
are base64 text (RFC 4648 section 4), as in a bundle (BUN-MAN-1). The store keeps each payload as
opaque bytes. It never parses, loads or runs an artifact, runtime profile or receipt (KND-OPQ-2).
"""
from __future__ import annotations

import base64
import binascii
import contextlib
import hashlib
import io
import json
import os
import re
import sqlite3
import threading
import uuid
from collections import deque
from datetime import date, datetime, timedelta, timezone
from decimal import Decimal

from . import descriptors as d
from .jsontext import FormError, canonical, detached, digest, is_digest, is_ijson, number_value, parse, revision, valid_string
from .kinds import BASE_PROFILE, HASHED_TERMS, INDEX_PROFILE, LAYER_PROFILE, PROFILES, registered
from .statuses import ORDER, REASON_STATUS, RESERVED

BUNDLE_FORMAT = 'aavanam-bundle/v1'
RAW_FORMAT = 'aavanam-bundle-raw/v1'  # BUN-RAW-1
RAW_PIECE = 1 << 20  # the most bytes of one read of a payload section (BUN-RAW-4)
COVERAGE = 'aavanam/coverage/v1'
REDISTRIBUTION = 'aavanam/redistribution/v1'
DECLARED = {'redistributable', 'no_redistribution', 'unknown'}  # BUN-RED-1
RENDER_PROFILE = 'aavanam/render/v1'
# The operations whose row in the operation table has `partial` (STS-ENV-3)
PARTIAL = {'query_data', 'search', 'context_bundle', 'evidence', 'coverage', 'observations', 'render', 'compare', 'impact',
           'snapshot_changes', 'list_items', 'search_text', 'semantic_search', 'item_history'}
LOCK_KINDS = {'execution_inputs': 'execution-lock', 'build_inputs': 'build-lock'}
# The capability table (CTX-PRJ-1): each capability and the operations that need it
CAPABILITIES = {'aavanam/data/v1': ('query_data',), 'aavanam/lexical/v1': ('search',),
                'aavanam/locks/v1': ('execution_inputs', 'build_inputs'), 'aavanam/export/v1': ('export',),
                'aavanam/text/v1': ('search_text',)}
# The capability that a profile adds for the stores that declare it (IDX-PRF-4)
PROFILE_CAPABILITIES = {INDEX_PROFILE: {INDEX_PROFILE: ('semantic_search',)}}
SKEW_POLICIES = ('reject', 'permit')  # CTX-SKW-3
ROLES = ('writer', 'device', 'output')  # BUN-IMP-5
# The fields of the publication record (PUB-LOG-2), after `candidate` and `receipt`. The rowid order is the head update order.
LOG_COLUMNS = ('activation', 'collection', 'previous_head', 'head')
# The member fields of a snapshot (PUB-ACT-2). Check 5 of PUB-ACT-3 does not apply to the last field.
MEMBER_FIELDS = ('units', 'schemas', 'relation_sets', 'overlay_sets', 'observations', 'native_mappings')
# The operations that write to a store, refused on a device store (BUN-IMP-6)
WRITES = ('put', 'create_snapshot', 'create_candidate', 'publish')
# The operations that read the store and its catalog, not a read context (LIF-ITM-4)
STORE_READS = ('snapshot_changes', 'item_history')
# The kinds that `remove` takes from the Evidence section (REL-RMV-3, PUB-SEC-3)
EVIDENCE_KINDS = ('source', 'reading', 'excerpt', 'evidence')

SCHEMA = """
CREATE TABLE IF NOT EXISTS objects (ref TEXT PRIMARY KEY, kind TEXT NOT NULL, descriptor TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS edges (source TEXT NOT NULL, target TEXT NOT NULL, PRIMARY KEY (source, target));
CREATE TABLE IF NOT EXISTS blobs (digest TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS deferred (digest TEXT NOT NULL, ref TEXT NOT NULL, PRIMARY KEY (digest, ref));
CREATE TABLE IF NOT EXISTS contexts (id TEXT PRIMARY KEY, roots TEXT NOT NULL, capabilities TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS context_views (context TEXT PRIMARY KEY, view TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS context_ends (context TEXT PRIMARY KEY, expires TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS context_members (context TEXT NOT NULL, ref TEXT NOT NULL, PRIMARY KEY (context, ref));
CREATE TABLE IF NOT EXISTS context_removed (context TEXT NOT NULL, ref TEXT NOT NULL, PRIMARY KEY (context, ref));
CREATE TABLE IF NOT EXISTS candidates (id TEXT PRIMARY KEY, snapshot TEXT NOT NULL, collection TEXT NOT NULL,
    policy TEXT NOT NULL, expected_head TEXT);
CREATE TABLE IF NOT EXISTS heads (collection TEXT PRIMARY KEY, snapshot TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS publications (candidate TEXT PRIMARY KEY, receipt TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS gates (collection TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS sections (collection TEXT PRIMARY KEY, section TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS producer_objects (producer TEXT NOT NULL, key TEXT NOT NULL, data BLOB NOT NULL,
    PRIMARY KEY (producer, key));
CREATE TABLE IF NOT EXISTS producer_entries (producer TEXT NOT NULL, log TEXT NOT NULL, sequence INTEGER NOT NULL,
    entry BLOB NOT NULL, PRIMARY KEY (producer, log, sequence));
CREATE TABLE IF NOT EXISTS producer_removals (producer TEXT NOT NULL, sequence INTEGER NOT NULL, key TEXT NOT NULL,
    time TEXT NOT NULL, reason TEXT NOT NULL, PRIMARY KEY (producer, sequence));
CREATE TABLE IF NOT EXISTS removals (sequence INTEGER PRIMARY KEY, ref TEXT NOT NULL, kind TEXT NOT NULL, time TEXT NOT NULL,
    reason TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS imports (seq INTEGER PRIMARY KEY, roots TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS removal_epoch (id INTEGER PRIMARY KEY CHECK (id = 1), epoch INTEGER NOT NULL);
INSERT OR IGNORE INTO removal_epoch VALUES (1, 0);
"""


class Stale(Exception):
    """A different open of the store file removed content, or wrote during a removal, after the start of the call.
    The call runs again from its start."""


ATTEMPTS = 8  # the number of runs of one call before the result is storage_error


class Failure(Exception):
    """An outcome of the operation as a whole (STS-ENV-3 rule 1)."""

    def __init__(self, status: str, reasons: list[dict]):
        super().__init__(status)
        self.status = status
        self.reasons = reasons


SECTIONS = ('serving', 'evidence')  # PUB-SEC-1
NOT_IN_SERVING = ('source', 'reading', 'excerpt', 'evidence')  # PUB-SEC-3
# The field that names the collection of a snapshot, and the base collection of a layer (LYR-DEF-1)
COLLECTION_FIELD = {'snapshot': 'collection', 'layer': 'base'}


class DeferredFailure(Exception):
    """A deferred check failed before a write added bytes (BUN-IMP-10, BUN-IMP-11)."""

    def __init__(self, found: dict[str, list[str]]):
        super().__init__('deferred check')
        self.found = found  # the reason codes of each descriptor in the original batch


class Members(set):
    """The members of a read context (CTX-VIW-1). `view` is the base snapshot and the layer of a layered
    context (LYR-PRE-2), or None. `removed` has the objects that the context reported in `removed` when it
    opened (REL-RMV-9). They stay out of the context, also after a later `put` adds them again (REL-RMV-13)."""
    view: list[str] | None = None
    removed: frozenset[str] = frozenset()


class SectionViolation(Exception):
    """The check of PUB-SEC-3 failed in the write transaction."""

    def __init__(self, found: dict[str, list[str]]):
        super().__init__('section_violation')
        self.found = found  # the descriptors that each revision of the batch must not have in its closure


def reason(code: str, refs=(), capabilities=()) -> dict:
    return {'code': code, 'refs': list(refs), 'capabilities': list(capabilities)}


def fail(*codes: str, refs=()) -> Failure:
    return Failure(REASON_STATUS[codes[0]], [reason(c, refs) for c in codes])


def item(status: str = 'ok', reasons=(), **fields) -> dict:
    return {'status': status, 'reasons': list(reasons), **fields}


def refused(codes: list[str], refs=()) -> dict:
    """An item with the status of its first code (STS-RSN-3)."""
    return item(REASON_STATUS[codes[0]], [reason(c, refs) for c in codes])


def first_status(outcomes: list[dict]) -> tuple[str, list[dict]]:
    """Rule 6 of STS-ENV-3: the first status in ORDER, with the reasons of the outcomes that have it."""
    status = next(s for s in ORDER if any(o['status'] == s for o in outcomes))
    return status, [r for o in outcomes if o['status'] == status for r in o['reasons']]


def summary(items: list[dict], operation: str) -> tuple[str, list[dict]]:
    """Rules 4 to 6 of STS-ENV-3."""
    if all(i['status'] == 'ok' for i in items):
        return 'ok', []
    if operation in PARTIAL and any(i['status'] == 'ok' for i in items):
        return 'partial', [reason('items_not_ok')]
    return first_status([i for i in items if i['status'] != 'ok'])


def joined(outcomes: list[dict]) -> dict:
    """One item from the outcomes of two targets: the first status, with one reason for each code (CHG-SEL-1, CHG-SEL-3)."""
    status, reasons = first_status(outcomes)
    codes = list(dict.fromkeys(r['code'] for r in reasons))
    return item(status, [reason(c, [x for r in reasons if r['code'] == c for x in r['refs']]) for c in codes])


def is_count(value, least: int = 0) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value >= least


DECIMAL = re.compile(r'-?(0|[1-9][0-9]*)(\.[0-9]+)?', re.ASCII)
DAY = re.compile(r'([0-9]{4})-([0-9]{2})-([0-9]{2})', re.ASCII)


def typed(kind: str, value):
    """The value in the order of its declared type, or None if it is not a value of the type (DAT-DEC-1, DAT-DAY-1).

    A decimal compares by its exact numeric value (DAT-DEC-2). A date compares in calendar order (DAT-DAY-2).
    """
    if not isinstance(value, str):
        return None
    if kind == 'decimal':
        return Decimal(value) if len(value) <= 64 and DECIMAL.fullmatch(value) else None
    found = DAY.fullmatch(value)
    try:
        return date(*map(int, found.groups())) if found else None
    except ValueError:
        return None


def is_range_clause(clause) -> bool:
    """The form of a range clause (DAT-RNG-1)."""
    if not isinstance(clause, dict) or clause.get('type') not in ('decimal', 'date'):
        return False
    match = clause.get('match')
    values = {'contains': ('value',), 'overlaps': ('start', 'end')}.get(match) if isinstance(match, str) else None
    if values is None or set(clause) != {'lower', 'upper', 'type', 'match', *values}:
        return False
    return (all(isinstance(clause[f], str) for f in ('lower', 'upper'))
            and all(typed(clause['type'], clause[f]) is not None for f in values))


def range_matches(clause: dict, lower, upper) -> bool:
    """The half-open match of DAT-RNG-2 and DAT-RNG-3. An empty range or interval gives False (DAT-RNG-4)."""
    kind = clause['type']
    if clause['match'] == 'contains':
        return lower <= typed(kind, clause['value']) < upper
    return max(lower, typed(kind, clause['start'])) < min(upper, typed(kind, clause['end']))


def is_filter(value) -> bool:
    """The form of a filter (DSC-FLT-1)."""
    if not isinstance(value, dict) or not {'equals', 'ranges'} & set(value) or set(value) - {'equals', 'ranges', 'precedence'}:
        return False
    equals, ranges, precedence = value.get('equals', {'-': 0}), value.get('ranges', [{}]), value.get('precedence', [])
    return (isinstance(equals, dict) and bool(equals) and is_ijson(equals) and all(v is not None for v in equals.values())
            and isinstance(ranges, list) and bool(ranges) and ('ranges' not in value or all(map(is_range_clause, ranges)))
            and isinstance(precedence, list) and all(isinstance(p, str) for p in precedence))


def form(request: dict, fields: dict, code: str = 'field_type') -> None:
    """Check the request fields. `fields` maps each field to (required, test). Each problem gives `code`."""
    if not (set(request) <= set(fields) and all(f in request for f, (required, _) in fields.items() if required)
            and all(test(request[f]) for f, (_, test) in fields.items() if f in request)):
        raise fail(code)


def strict(request: dict, fields: dict, sets=(), many=()) -> None:
    """Check the request form: unknown_field, then field_count, then field_type (PUB-CND-1, PUB-HED-2, RND-DOC-1).

    `fields` maps each field to (required, test). A field in `sets` with a duplicate gives field_count (KND-SCH-5).
    A field in `many` has a count of 1..n, so an empty list gives field_count (KND-SCH-3).
    """
    if set(request) - set(fields):
        raise fail('unknown_field')
    if any(required and f not in request for f, (required, _) in fields.items()):
        raise fail('field_count')
    if any(request.get(f) == [] for f in many):
        raise fail('field_count')
    if any(isinstance(v := request.get(f), list) and len(set(map(repr, v))) != len(v) for f in sets):
        raise fail('field_count')
    if not all(test(request[f]) for f, (_, test) in fields.items() if f in request):
        raise fail('field_type')


def counted(request: dict, field: str, many: bool = False) -> None:
    """A set field of a request: an empty list (if `many`) or a duplicate gives field_count (KND-SCH-3, KND-SCH-5)."""
    value = request.get(field)
    if isinstance(value, list) and ((many and not value) or len(set(value)) != len(value)):
        raise fail('field_count')


def is_text(value) -> bool:
    """A string that the store keeps in a descriptor, so it must agree with I-JSON (KND-SCH-4)."""
    return isinstance(value, str) and valid_string(value)


def is_str(value) -> bool:
    return isinstance(value, str)


WORD = re.compile(rb'[A-Za-z0-9\x80-\xff]+')  # CRD-TXT-3 rule 1: a maximal sequence of word bytes


def words(data: bytes) -> list[tuple[int, int, bytes]]:
    """The start, the end and the folded form of each word (CRD-TXT-3). bytes.lower folds only A to Z."""
    return [(m.start(), m.end(), m.group().lower()) for m in WORD.finditer(data)]


def is_query(value) -> bool:
    """A query of CRD-TXT-1: 1 to 1024 Unicode scalar values, with 1 to 32 words."""
    if not (isinstance(value, str) and 1 <= len(value) <= 1024) or any(0xD800 <= ord(c) <= 0xDFFF for c in value):
        return False
    return 1 <= len(WORD.findall(value.encode('utf-8'))) <= 32


def buckets(data: bytes, dimensions: int) -> set[int]:
    """The bucket set of the terms of `data` under the method hashed-terms (IDX-MTH-4). A term is the folded form
    of a word (CRD-TXT-3). Its bucket is the first four bytes of its SHA-256 digest, as an integer, modulo `dimensions`."""
    return {int.from_bytes(hashlib.sha256(w).digest()[:4], 'big') % dimensions for _, _, w in words(data)}


def is_text_media(media_type: str) -> bool:
    """CRD-TXT-2: the part before the first `;`, without ASCII spaces at its ends, with A to Z folded, is text/<more>."""
    part = media_type.split(';', 1)[0].strip(' ').encode('utf-8').lower()
    return part.startswith(b'text/') and len(part) > len(b'text/')


def is_names(value) -> bool:
    """A list of capability names (LCK-CAP-1)."""
    return isinstance(value, list) and all(is_text(v) for v in value)


def is_label(value) -> bool:
    """A completeness label (LCK-CMPL-1)."""
    return isinstance(value, str) and d.LABEL.fullmatch(value) is not None


def is_refs(value, empty: bool = False) -> bool:
    return isinstance(value, list) and (empty or bool(value)) and all(is_digest(r) for r in value)


def is_name(most: int):
    """A string of 1 to `most` characters that the store can keep (PRD-SEC-3)."""
    return lambda value: is_text(value) and 1 <= len(value) <= most


def is_b64(value) -> bool:
    """Base64 text (RFC 4648 section 4) (PRD-SEC-3)."""
    try:
        base64.b64decode(value, validate=True)
        return isinstance(value, str)
    except (binascii.Error, ValueError, TypeError):
        return False


def is_log(value) -> bool:
    """A log name: a list of 1 to 8 strings of 1 to 512 characters (PRD-SEC-3)."""
    return isinstance(value, list) and 1 <= len(value) <= 8 and all(map(is_name(512), value))


# The request fields of the producer section (PRD-SEC-3)
PRODUCER = {'producer': (True, is_name(256))}
PRODUCER_KEY = {**PRODUCER, 'key': (True, is_name(512))}
PRODUCER_LOG = {**PRODUCER, 'log': (True, is_log)}


ROW = 'aavanam/row/v1'  # CRD-SEL-1


def is_read_selector(value) -> bool:
    """The form of a read selector (CRD-SEL-1, REL-SEL-1, REL-SEL-3, ADP-SEL-4)."""
    if isinstance(value, dict) and value.get('selector_profile') == ROW:
        return set(value) == {'selector_profile', 'index'} and is_count(value['index'])
    return d.is_selector(value)


def b64(data: bytes) -> str:
    return base64.b64encode(data).decode('ascii')


def is_manifest(bundle, name: str) -> bool:
    """The fields that a bundle and the manifest of a raw bundle share (BUN-MAN-1, BUN-RAW-1). `payloads` is not checked."""
    return (isinstance(bundle, dict) and set(bundle) - {'withheld', 'filter', 'base'} == {'format', 'roots', 'descriptors', 'payloads'}
            and bundle['format'] == name and ('filter' not in bundle or is_filter(bundle['filter']))  # BUN-FLT-4
            and ('base' not in bundle or is_refs(bundle['base']))  # BUN-DLT-3
            and is_refs(bundle['roots']) and isinstance(bundle['descriptors'], list)
            and all(isinstance(t, str) for t in bundle['descriptors'])
            and ('withheld' not in bundle or (is_refs(bundle['withheld']) and len(set(bundle['withheld'])) == len(bundle['withheld']))))


def is_raw(source) -> bool:
    """A raw bundle is a byte sequence, or a binary stream that gives one (BUN-RAW-1)."""
    return isinstance(source, (bytes, bytearray)) or callable(getattr(source, 'readline', None))


def raw_form(bundle: dict) -> bytes:
    """A raw form of a bundle (BUN-RAW-3). The entries are in ascending digest order. It needs no store."""
    data = {k: unb64(v) for k, v in sorted(bundle['payloads'].items())}
    manifest = {**bundle, 'format': RAW_FORMAT, 'payloads': [{'digest': k, 'size': len(v)} for k, v in data.items()]}
    return canonical(manifest).encode('utf-8') + b'\n' + b''.join(data.values())


def raw_parts(source) -> tuple[dict, dict[str, bytes]]:
    """Check a raw bundle in the order of BUN-RAW-4. Return its manifest and the bytes of each entry.

    The function reads the manifest line, then one entry at a time. It checks the digest of an entry before it
    reads the next entry.
    """
    reader = io.BytesIO(bytes(source)) if isinstance(source, (bytes, bytearray)) else source
    line = reader.readline()
    if not isinstance(line, bytes) or not line.endswith(b'\n'):
        raise fail('malformed_json')  # step 1: no line feed byte
    try:
        manifest = parse(line[:-1].decode('utf-8'))
    except UnicodeDecodeError:
        raise fail('malformed_json') from None
    except FormError as e:
        raise fail(e.codes[0]) from None  # one reason (BUN-RAW-4)
    entries = manifest.get('payloads') if isinstance(manifest, dict) else None
    if not (is_manifest(manifest, RAW_FORMAT) and isinstance(entries, list)
            and all(isinstance(e, dict) and set(e) == {'digest', 'size'} and is_digest(e['digest']) and is_count(e['size'])
                    for e in entries)
            and len({e['digest'] for e in entries}) == len(entries)
            and not set(manifest.get('withheld', [])) & {e['digest'] for e in entries}):
        raise fail('field_type')  # step 2
    data = {}
    for e in entries:  # step 3
        parts, left = [], e['size']
        while left:  # a read can give fewer bytes, and a size of the input can be more than the section has
            part = reader.read(min(left, RAW_PIECE))
            if not isinstance(part, bytes) or not part:
                raise fail('field_type')
            parts.append(part)
            left -= len(part)
        piece = b''.join(parts)
        if digest(piece) != e['digest']:
            raise fail('digest_mismatch')
        data[e['digest']] = piece
    if reader.read(1):
        raise fail('field_type')  # step 4
    return manifest, data


def unb64(text) -> bytes:
    if not isinstance(text, str):
        raise fail('field_type')
    try:
        return base64.b64decode(text, validate=True)
    except (binascii.Error, ValueError):
        raise fail('field_type') from None


class Store:
    """One store with one role (BUN-IMP-5). A device store creates its local output store at `output`, with
    `output_path`. A local output store reads the objects and bytes of its `device` also (BUN-IMP-8)."""

    def __init__(self, path: str = ':memory:', debug: bool = False, role: str = 'writer', output_path: str = ':memory:',
                 device: Store | None = None, profiles=PROFILES):
        if role not in ROLES or (role == 'output') != (device is not None):
            raise ValueError('a local output store has one device store, and only a local output store has one')
        if role == 'output' and (device.role != 'device' or device.output is not None):
            raise ValueError('only a device store creates its local output store (BUN-IMP-5)')
        if role == 'device' and path != ':memory:' and os.path.realpath(path) == os.path.realpath(output_path):
            raise ValueError('a device store and its local output store have separate databases (BUN-IMP-7)')
        if set(profiles) - set(PROFILES):
            raise ValueError(f'a store declares only these profiles: {", ".join(PROFILES)} (LYR-PRF-1)')
        self.profiles = tuple(profiles)  # the declared profiles, for the life of this open
        # The capability table of this store, and the capability that each operation needs (CTX-PRJ-1, IDX-PRF-4)
        self.capabilities = {**CAPABILITIES, **{c: ops for p in self.profiles for c, ops in PROFILE_CAPABILITIES.get(p, {}).items()}}
        self.needs = {op: cap for cap, ops in self.capabilities.items() for op in ops}
        self.role, self.device, self.output = role, device, None
        self.debug = debug  # raise in place of storage_error, for tests
        self.broken = False  # a rollback failed, so the connection can show a partial write
        self.db = sqlite3.connect(path, timeout=60, isolation_level=None, check_same_thread=False)
        self.db.executescript(SCHEMA)
        self.db.execute('PRAGMA secure_delete = ON')  # a removed producer object leaves no bytes in a free page (PRD-RMV-3)
        self.clock = lambda: datetime.now(timezone.utc)  # the store clock of a removal record (PRD-RMV-1)
        columns = [c for (_, c, *_) in self.db.execute('PRAGMA table_info(contexts)')]
        if 'capabilities' not in columns:  # a context of an older store file has each capability (CTX-PRJ-2)
            self.db.execute('ALTER TABLE contexts ADD COLUMN capabilities TEXT NOT NULL DEFAULT ' +
                            "'" + json.dumps(sorted(CAPABILITIES)) + "'")
        self.migrate_publications()
        self.gone: set[str] | None = None  # the removed objects, read at the first use (REL-RMV-7)
        self.version = None  # the data version of the file at the last call (REL-RMV-7)
        self.wrote = True  # False from the start of a call to its first write transaction: begin() checks only then
        self.epoch = None  # the removal epoch of the file at the start of the call
        self.mutex = threading.RLock()  # one connection, one call at a time
        self.called = False  # a gate is declared before the first call (PUB-GAT-1)
        self.parts = None  # the parts of the raw bundle of the current call, from its first run
        self.cache: dict[str, dict] = {}  # descriptors do not change (IDN-CAN-1)
        self.policies: dict[str, object] = {}  # the acceptance policies of the application (PUB-RCP-1)
        self.output = Store(output_path, debug, 'output', device=self, profiles=profiles) if role == 'device' else None
        if self.output and ':memory:' not in (path, output_path) and os.path.samefile(path, output_path):
            self.close()  # a hard link names one file with two paths (BUN-IMP-7)
            raise ValueError('a device store and its local output store have separate databases (BUN-IMP-7)')

    def migrate_publications(self) -> None:
        """Add the fields of PUB-LOG-2 to the table of a store before PUB-LOG-2, in one write transaction.

        Each record gets its fields from its candidate. A second open that waits for the lock reads the new form.
        """
        if 'head' in [c for (_, c, *_) in self.db.execute('PRAGMA table_info(publications)')]:
            return
        self.db.execute('BEGIN IMMEDIATE')
        try:
            if 'head' not in [c for (_, c, *_) in self.db.execute('PRAGMA table_info(publications)')]:
                for column in LOG_COLUMNS:
                    self.db.execute(f'ALTER TABLE publications ADD COLUMN {column} TEXT')
                self.db.execute('UPDATE publications SET (collection, previous_head, head) = (SELECT collection, '
                                'expected_head, snapshot FROM candidates WHERE id = publications.candidate)')
            self.db.execute('COMMIT')
        except BaseException:
            self.db.execute('ROLLBACK')
            raise

    def accept(self, policy: str, validate) -> None:
        """Register the validator of an acceptance policy: validate(candidate, receipt, activation) -> bool (PUB-RCP-1).

        `activation` is the activation reference of the request, or None (PUB-RCP-2). A validator raises
        TimeoutError if it does not answer before its deadline. Then `publish` gives `deadline_exceeded` (PUB-RCP-4).
        """
        self.policies[policy] = validate

    def gate(self, collection: str) -> None:
        """Declare a gated collection (PUB-GAT-1). The store keeps the declaration for its life (PUB-GAT-2).

        The declaration is configuration of this open. Thus it comes before the first call.
        """
        if not isinstance(collection, str):
            raise TypeError('a gated collection is a collection name, a string (PUB-GAT-1)')
        if self.called:
            raise ValueError('declare a gated collection before the first call of this open (PUB-GAT-1)')
        with self.mutex:
            self.db.execute('INSERT OR IGNORE INTO gates VALUES (?)', (collection,))

    def gated(self, collection: str) -> bool:
        return self.db.execute('SELECT 1 FROM gates WHERE collection = ?', (collection,)).fetchone() is not None

    def section(self, collection: str, section: str) -> None:
        """Declare the storage section of a collection (PUB-SEC-1). The store keeps it for its life (PUB-SEC-2)."""
        if not isinstance(collection, str) or section not in SECTIONS:
            raise TypeError('a section declaration is a collection name and a section name (PUB-SEC-1)')
        if self.role != 'writer':
            raise ValueError('only a writer store has sections (PUB-SEC-1)')
        if self.called:
            raise ValueError('declare a section before the first call of this open (PUB-SEC-1)')
        with self.mutex:
            self.db.execute('BEGIN IMMEDIATE')  # atomic with a snapshot write of a different open (PUB-SEC-2)
            try:
                declared = self.section_of(collection)
                if declared not in (None, section):
                    raise ValueError(f'the collection already has the section {declared} (PUB-SEC-2)')
                if declared is None:
                    for kind, text in self.db.execute("SELECT kind, descriptor FROM objects WHERE kind IN ('snapshot', 'layer')"):
                        if json.loads(text, parse_float=number_value)[COLLECTION_FIELD[kind]] == collection:
                            raise ValueError(f'the collection already has a {kind} (PUB-SEC-2, LYR-DEF-5)')
                    self.db.execute('INSERT INTO sections VALUES (?, ?)', (collection, section))
                self.db.execute('COMMIT')
            except BaseException:
                self.rollback()
                raise

    def section_of(self, collection: str) -> str | None:
        row = self.db.execute('SELECT section FROM sections WHERE collection = ?', (collection,)).fetchone()
        return row[0] if row else None

    def close(self) -> None:
        if self.output:
            self.output.close()
        self.db.close()

    # Reads of stored content

    def descriptor(self, ref: str) -> dict | None:
        if ref not in self.cache:
            row = self.db.execute('SELECT descriptor FROM objects WHERE ref = ?', (ref,)).fetchone()
            if row is None:
                return self.device.descriptor(ref) if self.device else None  # BUN-IMP-8
            self.cache[ref] = json.loads(row[0], parse_float=number_value)  # the same values as parse()
        return self.cache[ref]

    def kind(self, ref: str) -> str | None:
        desc = self.descriptor(ref)
        return desc['kind'] if desc else None

    def blob(self, key: str, *, ref: str) -> bytes | None:
        """Read bytes after the deferred checks of this descriptor pass (BUN-IMP-12)."""
        data = self.raw_blob(key)
        if data is not None:
            stores = (self, self.device) if self.device else (self,)
            if any(store.db.execute('SELECT 1 FROM deferred WHERE ref = ? LIMIT 1', (ref,)).fetchone()
                   for store in stores):
                # An excerpt record can name only its absent parent digest (BUN-IMP-10).
                desc = self.descriptor(ref)
                try:
                    self.step_payloads(desc, self.descriptor, self.raw_blob, False)
                    self.step_content(desc, self.descriptor, self.raw_blob, False)
                except d.Refusal:
                    return None  # failed and incomplete checks use the absent-byte read rules
        return data

    def raw_blob(self, key: str) -> bytes | None:
        """Stored bytes for integrity checks. Read operations use blob() (BUN-IMP-10 to BUN-IMP-12)."""
        row = self.db.execute('SELECT data FROM blobs WHERE digest = ?', (key,)).fetchone()
        if row is None:
            return self.device.raw_blob(key) if self.device else None  # BUN-IMP-8
        return bytes(row[0])

    def targets(self, ref: str) -> set[str]:
        """The `refs` of an object (KND-DEP-2), from the edges table of this store and of its device store."""
        out = {t for (t,) in self.db.execute('SELECT target FROM edges WHERE source = ?', (ref,))}
        return out | self.device.targets(ref) if self.device else out

    def sources(self, ref: str) -> set[str]:
        """The objects whose `refs` have the object, in this store and in its device store (BUN-IMP-8)."""
        out = {s for (s,) in self.db.execute('SELECT source FROM edges WHERE target = ?', (ref,))}
        return out | self.device.sources(ref) if self.device else out

    def closure(self, roots, references: bool = False) -> set[str]:
        """The roots and each object that their references reach (KND-DEP-3). The edges table is `refs` (KND-DEP-2).
        The walk follows the kept `refs` of a removed object, and the result does not have the removed objects
        (REL-RMV-8). With `references`, the result has them also, as references."""
        seen, todo = set(roots), deque(roots)
        while todo:
            for target in self.targets(todo.popleft()):
                if target not in seen:
                    seen.add(target)
                    todo.append(target)
        return seen if references else seen - self.removed()

    def fresh(self) -> None:
        """Empty the caches if a different open of the store file wrote to it. A different open can remove an object
        (REL-RMV-7, LIF-GC-5), and this open MUST NOT give a descriptor that the store does not have."""
        while True:  # the data version and the removal epoch of one state of the file
            version, epoch = self.data_version(), self.removal_epoch()
            if self.data_version() == version:
                break
        if version != self.version:
            self.version, self.gone = version, None
            self.cache.clear()
        self.epoch = epoch

    def data_version(self) -> int:
        """A number that changes when a different open of the store file commits a write."""
        return self.db.execute('PRAGMA data_version').fetchone()[0]

    def removal_epoch(self) -> int:
        """A number that each `remove` and each `gc_collect` that removes content increments."""
        return self.db.execute('SELECT epoch FROM removal_epoch').fetchone()[0]

    def begin(self, strict: bool = False) -> None:
        """Start a write transaction of a call. The checks of a call read the store before this point, and a different
        open cannot write to the file after it. If a different open removed content from the file of the store or of
        its device store after the start of the call, the call runs again, so no write names content that the checks
        read and the store does not have (STS-MAL-2, REL-RMV-4, LIF-GC-5). A `strict` caller removes content: it runs
        again after each write of a different open, because a new context, head or object can retain content."""
        self.db.execute('BEGIN IMMEDIATE')
        if self.wrote:  # not in a call, or not the first write transaction of the call
            return
        stores = [s for s in (self, self.device) if s]
        if any(s.data_version() != s.version for s in stores) if strict else any(s.removal_epoch() != s.epoch for s in stores):
            self.rollback()
            raise Stale
        self.wrote = True

    def removed(self) -> set[str]:
        """The references that name a removed object: a removal record has the reference, and the store does not
        have the object (REL-RMV-7). Only a writer store has removal records (REL-RMV-1)."""
        if self.gone is None:
            self.gone = {r for (r,) in self.db.execute(
                'SELECT ref FROM removals WHERE ref NOT IN (SELECT ref FROM objects)')}
        return self.gone

    def gone_in(self, roots) -> list[str]:
        """The removed objects in the closure of the roots, in ascending order (REL-RMV-9, REL-RMV-10)."""
        return sorted(self.closure(roots, True) & self.removed()) if self.removed() else []

    def checked_closure(self, roots, members: Members) -> set[str]:
        """The closure of the roots for a bundle or a lock of a read context (REL-RMV-10). A closure with a removed
        object gives `removed`. A closure with an object that the context reported in `removed`, and that a later
        `put` added again, gives the reason of an object that is not in the context (REL-RMV-13, LYR-ISO-2)."""
        closure = self.closure(roots, True)
        out = sorted(closure & (self.removed() | members.removed))
        if out:
            outside = 'not_in_context' if members.view is None else 'unknown_ref'
            raise Failure('not_found', [reason('removed' if r in self.removed() else outside, [r]) for r in out])
        return closure  # it has no removed object, so each reference names an object

    def fingerprint(self) -> str:
        h = hashlib.sha256()
        for table, key in (('objects', 'ref'), ('blobs', 'digest'), ('deferred', "digest || ' ' || ref"), ('candidates', 'id'),
                           ('heads', "collection || ' ' || snapshot"), ('publications', "candidate || ' ' || receipt || ' ' || coalesce(activation, '')"),
                           ('producer_objects', "producer || ' ' || key || ' ' || hex(data)"),
                           ('producer_entries', "producer || ' ' || log || ' ' || sequence || ' ' || hex(entry)"),
                           ('producer_removals', "producer || ' ' || sequence || ' ' || key || ' ' || time || ' ' || reason"),
                           ('removals', "sequence || ' ' || ref || ' ' || kind || ' ' || time || ' ' || reason")):
            for (value,) in self.db.execute(f'SELECT {key} FROM {table} ORDER BY {key}'):
                h.update(f'{table}:{value}\n'.encode())
        return h.hexdigest()

    def write(self, objects: dict[str, dict], blobs: dict[str, bytes], withheld=frozenset(), imported=None) -> None:
        """Write the objects and bytes in one transaction: all or none (STS-MAL-2). `imported` has the roots of an
        import on a device store. The transaction also records them, so no `gc_collect` of a different open reads the
        content with no imported root (BUN-RMV-1)."""
        self.begin()
        try:
            batch = objects  # BUN-IMP-10: keep each descriptor for failure attribution
            deferred = set()
            if withheld:  # BUN-IMP-11: bytes can arrive after the first absence check
                lookup = lambda r: batch.get(r) or self.descriptor(r)
                data_of = lambda k: blobs[k] if k in blobs else self.raw_blob(k)
                found = {}
                for ref, desc in batch.items():
                    try:
                        self.step_payloads(desc, lookup, data_of, True, withheld)
                        self.step_content(desc, lookup, data_of, True, withheld)
                    except d.Refusal as e:
                        found[ref] = e.codes
                    own = {p['digest'] for p in d.payloads(desc)}
                    missing = {k for k in own if data_of(k) is None}
                    deferred |= {(k, ref) for k in missing}
                    if desc['kind'] == 'excerpt':
                        needed = own | {lookup(desc['parent'])['payload']['digest']}
                        absent = {k for k in needed if data_of(k) is None}
                        if absent & withheld:
                            deferred |= {(k, ref) for k in absent}
                if found:
                    raise DeferredFailure(found)
            found = self.deferred_failures(batch, blobs)  # before the device descriptor filter (BUN-IMP-10)
            if found:
                raise DeferredFailure(found)
            if self.role == 'output':  # the objects and bytes of the device store are already here (BUN-IMP-8)
                objects = {r: o for r, o in objects.items() if self.device.descriptor(r) is None}
                blobs = {k: b for k, b in blobs.items() if self.device.raw_blob(k) is None}
            # A second connection can declare a section after the check of call(), so check again here (PUB-SEC-2)
            found = {ref: self.section_violations(desc, lambda r: objects.get(r) or self.descriptor(r)) for ref, desc in objects.items()}
            if any(found.values()):
                raise SectionViolation(found)
            for ref, desc in objects.items():
                self.db.execute('INSERT OR IGNORE INTO objects VALUES (?, ?, ?)', (ref, desc['kind'], canonical(desc)))
                self.db.executemany('INSERT OR IGNORE INTO edges VALUES (?, ?)', {(ref, t) for _, t in d.references(desc)})
            self.db.executemany('INSERT OR IGNORE INTO blobs VALUES (?, ?)', blobs.items())
            self.db.executemany('DELETE FROM deferred WHERE digest = ?', [(k,) for k in blobs])
            self.db.executemany('INSERT OR IGNORE INTO deferred VALUES (?, ?)', deferred)
            if imported is not None:
                self.db.execute('INSERT INTO imports (roots) VALUES (?)', (json.dumps(imported),))
            self.db.execute('COMMIT')
            self.gone = None  # a put of a removed descriptor adds the object again (REL-RMV-11)
        except BaseException:
            self.rollback()
            raise

    def deferred_refs(self, key: str) -> set[str]:
        """The descriptors with deferred checks that need `key` (BUN-IMP-10)."""
        out = {r for (r,) in self.db.execute('SELECT ref FROM deferred WHERE digest = ?', (key,))}
        return out | self.device.deferred_refs(key) if self.device else out

    def deferred_failures(self, objects: dict[str, dict], blobs: dict[str, bytes]) -> dict[str, list[str]]:
        """Check stored descriptors before new bytes arrive, with the original batch (BUN-IMP-10)."""
        new = {k: b for k, b in blobs.items() if self.raw_blob(k) is None}
        lookup = lambda r: objects.get(r) or self.descriptor(r)
        data_of = lambda k: new[k] if k in new else self.raw_blob(k)
        found: dict[str, list[str]] = {}
        for key in new:
            for ref in sorted(self.deferred_refs(key)):
                desc = lookup(ref)
                needed = {p['digest'] for p in d.payloads(desc)}
                if desc['kind'] == 'excerpt':
                    needed.add(lookup(desc['parent'])['payload']['digest'])
                absent = {k for k in needed if data_of(k) is None}
                try:
                    self.step_payloads(desc, lookup, data_of, False, absent)
                    self.step_content(desc, lookup, data_of, False, absent)
                except d.Refusal as e:
                    for carrier, c in objects.items():
                        if key in {p['digest'] for p in d.payloads(c)}:
                            found.setdefault(carrier, []).extend(e.codes)
        return found

    def rollback(self) -> None:
        """End an open transaction. A failure of the rollback does not cross the API (STS-MAL-1)."""
        try:
            if self.db.in_transaction:
                self.db.execute('ROLLBACK')
        except sqlite3.Error:
            self.broken = True  # each later call gives storage_error (STS-MAL-2)

    # The API

    def call(self, operation: str, request) -> dict:
        """Return the envelope of one call. No raw exception crosses the API (STS-MAL-1)."""
        if not isinstance(operation, str):
            return self.envelope(None, 'unsupported', [reason('unsupported_operation')])
        if operation in RESERVED and RESERVED[operation] not in self.profiles:  # STS-MAL-4, RSV-RSP-1, RSV-LST-4
            return self.envelope(operation, 'unsupported', [reason('reserved_profile', (), [RESERVED[operation]])])
        method = getattr(self, 'op_' + operation, None)
        if method is None:
            return self.envelope(operation, 'unsupported', [reason('unsupported_operation')])
        self.called = True
        context = (request.get('context') if isinstance(request, dict) and isinstance(request.get('context'), str)
                   and operation not in STORE_READS else None)  # LIF-ITM-4
        # A local output store reads its device store, so it waits for a device call, for example an import (CTX-OPN-2).
        # The order is always the device store, then the output store.
        with self.device.mutex if self.device else contextlib.nullcontext(), self.mutex:
            outer, self.parts = self.parts, None  # a call in a call has its own raw bundle
            try:
                return self.attempts(operation, method, request, context)
            finally:
                self.parts = outer

    def attempts(self, operation: str, method, request, context) -> dict:
        """Run the call until a run does not raise Stale. The caller has the mutexes."""
        for _ in range(ATTEMPTS):
            try:
                return self.attempt(operation, method, request)
            except Stale:
                continue
            except Failure as f:
                return self.marked(self.envelope(operation, f.status, f.reasons, context=context, **self.empty(operation)), request)
            except Exception:  # noqa: BLE001  STS-MAL-1
                if self.debug:
                    raise
                self.rollback()
                break
            finally:
                self.wrote = True
        return self.envelope(operation, 'failed', [reason('storage_error')], context=context, **self.empty(operation))

    def attempt(self, operation: str, method, request) -> dict:
        """Run the call one time. The caller has the mutexes."""
        if self.broken:
            raise sqlite3.DatabaseError('the connection is not usable')
        self.wrote = False
        self.fresh()
        if self.device:  # the store reads the descriptors of its device store (BUN-IMP-8), also from its cache
            self.device.fresh()
        if self.role == 'device' and operation in WRITES:  # an import is permitted, also a second one (BUN-UPD-1)
            raise fail('read_only_store')  # BUN-IMP-6: after STS-MAL-4, before the request
        if operation.startswith('producer_') and self.role != 'writer':
            raise fail('producer_section_absent')  # PRD-SEC-1: after STS-MAL-4, before the request
        if (operation in ('remove', 'removals') and self.role != 'writer') or (operation == 'gc_collect' and self.role == 'output'):
            raise fail('unsupported_role')  # REL-RMV-1, LIF-GC-1: after STS-MAL-4, before the request
        if not isinstance(request, dict) and not (operation == 'import' and is_raw(request)):  # BUN-RAW-4
            raise fail('field_type')
        return self.marked(detached(method(request)), request)  # a caller cannot change the cached descriptors (CTX-OPN-2)

    @staticmethod
    def empty(operation: str) -> dict:
        """The added fields of a result that did not complete (STS-ENV-3 rule 1, RND-DOC-3)."""
        return {'items': [], 'document': None, 'document_id': None} if operation == 'render' else {'items': []}

    def marked(self, out: dict, request) -> dict:
        """Give the reason `removed` in place of `unknown_ref` and `not_in_context` for a reference that names a
        removed object (REL-RMV-7). In a layered context, the reason stays `unknown_ref` (LYR-ISO-2)."""
        if self.role != 'writer' or not self.removed():
            return out
        context = request.get('context') if isinstance(request, dict) else None
        if isinstance(context, str) and self.db.execute('SELECT 1 FROM context_views WHERE context = ?', (context,)).fetchone():
            return out
        for entry in (out, *out.get('items', [])):
            for r in entry['reasons']:
                if r['code'] in ('unknown_ref', 'not_in_context') and r['refs'] and set(r['refs']) <= self.removed():
                    r['code'] = 'removed'
        return out

    @staticmethod
    def envelope(operation, status, reasons, items=None, context=None, **fields) -> dict:
        out = {'status': status, 'reasons': reasons, 'operation': operation, 'operation_profile': BASE_PROFILE,
               'context': context, **fields}
        if items is not None:
            out['items'] = items
        return out

    def result(self, operation: str, items: list[dict], context=None, bounded: bool = False, **fields) -> dict:
        """The envelope of the items. `bounded` states that a declared bound stopped the operation (STS-ENV-3 rules 2 and 3)."""
        if bounded and any(i['status'] == 'ok' for i in items):
            extra = [reason('items_not_ok')] if any(i['status'] != 'ok' for i in items) else []
            return self.envelope(operation, 'partial', [reason('bounds_reached'), *extra], items, context, **fields)
        if bounded:
            return self.envelope(operation, 'failed', [reason('incomplete_result')], [], context, **fields)
        status, reasons = summary(items, operation)
        return self.envelope(operation, status, reasons, items, context, **fields)

    # Read contexts (document 08)

    def members(self, request: dict, operation: str | None = None) -> tuple[str, set[str]]:
        """The identity and the content of the read context of the request (CTX-OPN-3). If the operation
        needs a capability that the context does not have, raise capability_unavailable (CTX-PRJ-4)."""
        context, members, capabilities = self.context_of(request)
        if operation in self.needs and self.needs[operation] not in capabilities:
            raise Failure('unsupported', [reason('capability_unavailable', (), [self.needs[operation]])])
        return context, members

    def context_of(self, request: dict) -> tuple[str, Members, list[str]]:
        """The identity, the content and the capabilities of the read context of the request (CTX-OPN-3, CTX-PRJ-2)."""
        context = request.get('context')
        if not isinstance(context, str):
            raise fail('field_type')
        row = self.db.execute('SELECT capabilities FROM contexts WHERE id = ?', (context,)).fetchone()
        # The table `contexts` keeps its three columns, so the end of a declared life is in a table of its own (CTX-LIF-1).
        end = self.db.execute('SELECT expires FROM context_ends WHERE context = ?', (context,)).fetchone()
        if row is None or (end is not None and self.now() >= end[0]):  # CTX-LIF-2, CTX-LIF-5
            raise fail('context_expired')
        members = Members(r for (r,) in self.db.execute('SELECT ref FROM context_members WHERE context = ?', (context,)))
        view = self.db.execute('SELECT view FROM context_views WHERE context = ?', (context,)).fetchone()
        members.view = json.loads(view[0]) if view else None  # a context with no row has no layer (LYR-OPN-3)
        members.removed = frozenset(r for (r,) in self.db.execute('SELECT ref FROM context_removed WHERE context = ?', (context,)))
        return context, members, json.loads(row[0])

    def base_roots(self, context: str) -> list[str]:
        """The base roots of a delta export: the roots of the base context and, for a layered context, its layer (BUN-DLT-1)."""
        row = self.db.execute('SELECT roots FROM contexts WHERE id = ?', (context,)).fetchone()
        if row is None:
            raise fail('context_expired')
        view = self.db.execute('SELECT view FROM context_views WHERE context = ?', (context,)).fetchone()
        return sorted(set(json.loads(row[0])) | set(json.loads(view[0]) if view else ()))

    def locate(self, ref: str, members: set[str]) -> str | None:
        """The reason code of a reference that is not in the read context (QRY-GET-2, CTX-OPN-4). A layered
        context does not show that the store has the object (LYR-ISO-2)."""
        if ref in members:
            return None
        layered = getattr(members, 'view', None) is not None
        return 'not_in_context' if self.descriptor(ref) and not layered else 'unknown_ref'

    def layer_item(self, layer: dict, root: dict) -> dict:
        """The item of the layer of an open_context request: the first check that fails, in order (LYR-OPN-2)."""
        ref, desc = layer['ref'], self.descriptor(layer['ref'])
        if desc is None:
            return refused(['unknown_ref'], [ref])
        if desc['kind'] != 'layer':
            return refused(['unsupported_target_kind'], [ref])
        if desc['scope'] != layer['scope']:
            return refused(['layer_scope_mismatch'], [ref])
        base = self.descriptor(root['ref']) if root['status'] == 'ok' else None
        if base is not None and (base['kind'] != 'snapshot' or base['collection'] != desc['base']):
            return refused(['layer_base_mismatch'], [ref])
        return item(ref=ref)

    def layered(self, base: str, layer: str) -> set[str]:
        """The members of a layered context (LYR-PRE-2): the base snapshot, the layer, and each object that a kept
        member or a layer member reaches on a path with no replaced member (LYR-PRE-1)."""
        added = self.members_of(layer)
        keys = {(self.kind(r), self.descriptor(r)['logical_id']) for r in added if 'logical_id' in self.descriptor(r)}
        replaced = {r for r in self.members_of(base) - added
                    if (self.kind(r), self.descriptor(r).get('logical_id')) in keys}
        seen, todo = set(), deque((self.members_of(base) - replaced) | added)
        while todo:
            ref = todo.popleft()
            if ref not in seen and ref not in replaced:
                seen.add(ref)
                todo.extend(self.targets(ref))
        return seen | {base, layer}

    def open_form(self, request: dict) -> None:
        """The request form of open_context: CTX-OPN-5, in order, with one reason for each affected value.
        Only a store that declares the layer profile has the field `layer` (LYR-PRF-1, LYR-OPN-1)."""
        fields = ('roots', 'heads', 'skew_policy', 'capabilities', 'life') + (('layer',) if LAYER_PROFILE in self.profiles else ())
        unknown = [f for f in request if f not in fields]
        if unknown:
            raise fail(*['unknown_field'] * len(unknown))
        types = [f for f in ('roots', 'heads', 'capabilities') if f in request and not isinstance(request[f], list)]
        if 'skew_policy' in request and request['skew_policy'] not in SKEW_POLICIES:
            types.append('skew_policy')
        if 'life' in request and not is_count(request['life'], 1):
            types.append('life')  # CTX-LIF-1
        tests = {'roots': is_digest, 'heads': is_str, 'capabilities': is_str}
        types += [v for f, test in tests.items() if isinstance(request.get(f), list) for v in request[f] if not test(v)]
        layer = request.get('layer')
        if 'layer' in request and not (isinstance(layer, dict) and set(layer) == {'ref', 'scope'} and is_digest(layer['ref'])
                                       and is_text(layer['scope'])):
            types.append('layer')  # LYR-OPN-1
        if types:
            raise fail(*['field_type'] * len(types))
        counts = [] if request.get('roots') or request.get('heads') else ['roots']
        repeats = sum(len(v) - len(set(v)) for v in (request.get('heads', []), request.get('capabilities', [])))
        if 'layer' in request and not counts and len(request.get('roots', [])) + len(request.get('heads', [])) != 1:
            counts.append('layer')  # LYR-OPN-1: a layered context has one root
        if counts or repeats:
            raise fail(*['field_count'] * (len(counts) + repeats))

    def op_open_context(self, request: dict) -> dict:
        self.open_form(request)
        capabilities = request.get('capabilities', list(self.capabilities))  # CTX-PRJ-2
        unknown = [c for c in capabilities if c not in self.capabilities]
        if unknown:  # CTX-PRJ-3
            raise Failure('unsupported', [reason('unsupported_profile', (), [c]) for c in unknown])
        roots, heads = request.get('roots', []), request.get('heads', [])
        found = self.heads(heads)  # CTX-VIW-3
        items = []
        for root in roots:  # CTX-OPN-1
            kind = self.kind(root)
            if kind is None:
                items.append(refused(['unknown_ref'], [root]))
            elif kind not in ('knowledge-unit', 'snapshot') and not (kind == 'retrieval-index' and INDEX_PROFILE in self.profiles):
                items.append(refused(['unsupported_target_kind'], [root]))  # IDX-DEF-3: an index root with the profile
            else:
                items.append(item(ref=root))
        items += [item(ref=found[n]) if found[n] else refused(['no_head']) for n in heads]  # CTX-VIW-2
        resolved = [i['ref'] for i in items if i['status'] == 'ok']
        layer = request.get('layer')
        if layer:
            items.append(self.layer_item(layer, items[0]))  # LYR-OPN-2
        self.composed(items)
        if all(i['status'] == 'ok' for i in items) and request.get('skew_policy', 'reject') == 'reject':
            self.skewed(items)
        status, reasons = summary(items, 'open_context')
        if status != 'ok':
            return self.envelope('open_context', status, reasons, items)
        for i in items:
            i['removed'] = self.gone_in([i['ref']])  # REL-RMV-9
        context = 'ctx:' + uuid.uuid4().hex
        view = [resolved[0], layer['ref']] if layer else None
        closure = self.layered(*view) - self.removed() if layer else self.closure(resolved)  # LYR-OPN-3, CTX-OPN-2, REL-RMV-8
        expires = self.now(request['life']) if 'life' in request else None  # CTX-LIF-1
        self.begin()
        try:
            self.db.execute('INSERT INTO contexts VALUES (?, ?, ?)', (context, json.dumps(resolved), json.dumps(sorted(capabilities))))
            if expires:
                self.db.execute('INSERT INTO context_ends VALUES (?, ?)', (context, expires))
            if view:
                self.db.execute('INSERT INTO context_views VALUES (?, ?)', (context, json.dumps(view)))
            self.db.executemany('INSERT INTO context_members VALUES (?, ?)', ((context, r) for r in closure))
            # REL-RMV-13: the objects that the items report stay out of the context, also after a later `put`
            self.db.executemany('INSERT INTO context_removed VALUES (?, ?)', ((context, r) for r in {r for i in items for r in i['removed']}))
            self.db.execute('COMMIT')
        except BaseException:
            self.rollback()
            raise
        return self.envelope('open_context', 'ok', [], items, context)  # CTX-OPN-2

    def now(self, later: int = 0) -> str:
        """The time of the store clock plus a number of seconds, in a form whose string order is the time order (CTX-LIF-2)."""
        return (self.clock().astimezone(timezone.utc) + timedelta(seconds=later)).strftime('%Y-%m-%dT%H:%M:%S.%fZ')

    def end_contexts(self, where: str, values=()) -> None:
        """Remove the rows of the read contexts that the condition selects. The caller has the transaction."""
        ended = [(c,) for (c,) in self.db.execute(f'SELECT id FROM contexts WHERE {where}', values)]
        for table, column in (('context_members', 'context'), ('context_removed', 'context'), ('context_views', 'context'),
                              ('context_ends', 'context'), ('contexts', 'id')):
            self.db.executemany(f'DELETE FROM {table} WHERE {column} = ?', ended)

    def transaction(self, change, output=None, strict: bool = False):
        """Run one change of the store in one transaction: all or none (STS-MAL-2). With the local output store of a
        device store, no open writes to the output file before the change ends. If a different open wrote to the
        output file after the caller read it, the call runs again (BUN-RMV-1)."""
        self.begin(strict)
        try:
            if output:  # the caller has the mutex of the output store and called its fresh()
                output.db.execute('BEGIN IMMEDIATE')
                if output.data_version() != output.version:
                    raise Stale
            out = change()
            self.db.execute('COMMIT')
            return out
        except BaseException:
            self.rollback()
            raise
        finally:
            if output:
                output.rollback()  # the change does not write to the output file

    def op_close_context(self, request: dict) -> dict:
        strict(request, {'context': (True, is_str)})  # CTX-LIF-3
        context, _, _ = self.context_of(request)  # CTX-LIF-5: a context that is not open gives context_expired
        self.transaction(lambda: self.end_contexts('id = ?', (context,)))
        return self.envelope('close_context', 'ok', [], [], context)

    def snapshots(self, items: list[dict]) -> list[tuple[int, str]]:
        """The position and the reference of each ok item with a snapshot root."""
        return [(n, i['ref']) for n, i in enumerate(items) if i['status'] == 'ok' and self.kind(i['ref']) == 'snapshot']

    def composed(self, items: list[dict]) -> None:
        """Two different snapshot roots of one collection give collection_conflict to each (CTX-CMP-1)."""
        roots = self.snapshots(items)
        collections: dict[str, set[str]] = {}
        for _, ref in roots:
            collections.setdefault(self.descriptor(ref)['collection'], set()).add(ref)
        for n, ref in roots:
            if len(collections[self.descriptor(ref)['collection']]) > 1:
                items[n] = refused(['collection_conflict'], [ref])

    def pins(self, root: str) -> dict[str, set[str]]:
        """The pins of a snapshot root for each logical ID (CTX-SKW-1)."""
        closure = self.closure([root])
        bound = {r for b in closure if self.kind(b) == 'binding-set' for r in
                 [*self.descriptor(b).get('bindings', {}).values(), *self.descriptor(b).get('runtime_profiles', [])]}
        out: dict[str, set[str]] = {}
        for ref in bound & closure:
            if 'logical_id' in (desc := self.descriptor(ref)):
                out.setdefault(desc['logical_id'], set()).add(ref)
        return out

    def skewed(self, items: list[dict]) -> None:
        """Under `reject`, each snapshot root with pins for a logical ID with skew is incompatible (CTX-SKW-2, CTX-SKW-4)."""
        roots = self.snapshots(items)
        pins = {ref: self.pins(ref) for ref in {ref for _, ref in roots}}
        skew = {lid for lid in {lid for p in pins.values() for lid in p}
                if len({frozenset(p[lid]) for p in pins.values() if lid in p}) > 1}
        for n, ref in roots:
            reasons = [reason('binding_skew', sorted(pins[ref][lid])) for lid in sorted(skew & set(pins[ref]))]
            if reasons:
                items[n] = item('incompatible', reasons)

    # Writes (documents 01, 02, 06 and 11)

    def check_batch(self, texts: list[str], data: dict[str, bytes], incomplete: bool, withheld=frozenset()) -> list:
        """Check each descriptor text in the steps of STS-MAL-3. Return a normal descriptor or a refused item for each.

        A target or payload can be in the batch or in the store. If `incomplete`, a target or payload that is in
        neither gives `bundle_incomplete`, except for withheld bytes (BUN-IMP-2, BUN-IMP-9).
        """
        out, batch = [], {}
        for text in texts:
            try:
                desc = d.check(parse(text) if isinstance(text, str) else text, self.profiles)
                batch[revision(desc)] = desc
                out.append(desc)
            except (FormError, d.Refusal) as e:
                out.append(refused(e.codes))

        def lookup(ref):
            return batch.get(ref) or self.descriptor(ref)

        def data_of(key):
            return data[key] if key in data else self.raw_blob(key)

        for n, desc in enumerate(out):
            if 'status' in desc:
                continue
            for step in (self.step_references, self.step_payloads, self.step_content, self.step_section):
                try:
                    step(desc, lookup, data_of, incomplete, withheld)
                except d.Refusal as e:
                    out[n] = refused(e.codes, e.refs)
                    break
        return out

    @staticmethod
    def step_references(desc, lookup, data_of, incomplete, withheld=frozenset()):
        """STS-MAL-3 step 4 (KND-REF-2, KND-REF-3, REL-RET-2)."""
        refs = d.references(desc)
        missing = [r for _, r in refs if lookup(r) is None]
        if missing:
            raise d.Refusal(['bundle_incomplete' if incomplete else 'missing_target'] * len(missing), missing)
        wrong = [r for f, r in refs if lookup(r)['kind'] not in d.targets(desc['kind'], f)]
        if wrong:
            raise d.Refusal(['unsupported_target_kind'] * len(wrong), wrong)
        if desc['kind'] == 'supersession' and lookup(desc['target'])['kind'] != lookup(desc['replacement'])['kind']:
            raise d.Refusal(['replacement_kind'], [desc['replacement']])

    @staticmethod
    def step_payloads(desc, lookup, data_of, incomplete, withheld=frozenset()):
        """STS-MAL-3 step 5 (KND-OPQ-1). Defer only absent withheld bytes (BUN-IMP-9)."""
        objects = [p for p in d.payloads(desc) if p['digest'] not in withheld or data_of(p['digest']) is not None]
        missing = [p['digest'] for p in objects if data_of(p['digest']) is None]
        if missing:
            raise d.Refusal(['bundle_incomplete' if incomplete else 'payload_missing'] * len(missing))
        wrong = [p for p in objects if digest(data_of(p['digest'])) != p['digest']]
        if wrong:
            raise d.Refusal(['digest_mismatch'] * len(wrong))
        sizes = [p for p in objects if len(data_of(p['digest'])) != p['size']]
        if sizes:
            raise d.Refusal(['size_mismatch'] * len(sizes))

    @staticmethod
    def step_content(desc, lookup, data_of, incomplete, withheld=frozenset()):
        """STS-MAL-3 step 6 (BUN-EXC-3, BUN-EXC-4, IDN-NAT-3)."""
        if desc['kind'] == 'excerpt':
            parent = lookup(desc['parent'])['payload']
            if any(k in withheld and data_of(k) is None for k in (parent['digest'], desc['payload']['digest'])):
                return  # BUN-IMP-9: defer the excerpt check until its bytes arrive
            whole = data_of(parent['digest'])
            if whole is None:
                raise d.Refusal(['bundle_incomplete' if incomplete else 'payload_missing'])
            parts = [whole[r['start']:r['end']] for r in desc['ranges'] if r['end'] <= len(whole)]
            if len(parts) != len(desc['ranges']) or b''.join(parts) != data_of(desc['payload']['digest']):
                raise d.Refusal(['excerpt_mismatch'])
        if desc['kind'] in ('snapshot', 'layer'):  # IDN-NAT-3, LYR-DEF-4
            subjects: dict[tuple, set] = {}
            for m in desc.get('native_mappings', []):
                mapping = lookup(m)
                subjects.setdefault((mapping['native_scheme'], mapping['native_id']), set()).add(mapping['subject'])
            conflicts = [k for k, v in subjects.items() if len(v) > 1]
            if conflicts:
                raise d.Refusal(['native_id_conflict'] * len(conflicts))

    def step_section(self, desc, lookup, data_of, incomplete, withheld=frozenset()):
        """STS-MAL-3 step 6: the closure of a snapshot in a `serving` collection (PUB-SEC-3)."""
        found = self.section_violations(desc, lookup)
        if found:
            raise d.Refusal(['section_violation'] * len(found), found)

    def section_violations(self, desc, lookup) -> list[str]:
        """The descriptors in the closure of a snapshot, or of a layer, that its `serving` collection does not
        permit (PUB-SEC-3, LYR-DEF-5)."""
        if desc['kind'] not in COLLECTION_FIELD or self.section_of(desc[COLLECTION_FIELD[desc['kind']]]) != 'serving':
            return []
        seen, todo, found = set(), [r for _, r in d.references(desc)], []
        while todo:
            ref = todo.pop()
            if ref not in seen:
                seen.add(ref)
                target = lookup(ref)
                if target is None:  # a member of this batch that fails its own step 4 (STS-ENV-5)
                    continue
                if target['kind'] in NOT_IN_SERVING:
                    found.append(ref)
                todo += [r for _, r in d.references(target)]
        return sorted(found)

    @staticmethod
    def revisions(texts: list[str]) -> set[str]:
        """The revision of each text: of its normal form, or of its JSON value if a check fails (BUN-IMP-2)."""
        out = set()
        for text in texts:
            try:
                value = parse(text)
            except FormError:
                continue
            try:
                out.add(revision(d.check(value)))
            except d.Refusal:
                out.add(revision(value))
        return out

    def commit_batch(self, operation: str, checked: list, data: dict[str, bytes], whole: list[dict] = (), withheld=frozenset(),
                     imported=None) -> dict:
        """Write the batch if each descriptor passed, or return the refusal (STS-ENV-5, STS-MAL-2)."""
        bad = [c for c in checked if 'status' in c] + list(whole)
        if bad:
            status, reasons = first_status(bad)
            return {'status': status, 'reasons': reasons,
                    'items': [c if 'status' in c else refused(['batch_aborted']) for c in checked]}
        objects = {revision(c): c for c in checked}
        blobs = {p['digest']: data[p['digest']] for c in checked for p in d.payloads(c) if p['digest'] in data}
        try:
            if imported is not None:
                self.write(objects, blobs, withheld, imported)
            elif withheld:
                self.write(objects, blobs, withheld)
            else:
                self.write(objects, blobs)
        except DeferredFailure as e:  # STS-ENV-4: the store did not change
            return self.commit_batch(operation, [refused(e.found[revision(c)]) if revision(c) in e.found else c for c in checked], data, whole)
        except SectionViolation as e:  # STS-ENV-4: the store did not change
            return self.commit_batch(operation, [refused(['section_violation'] * len(e.found[revision(c)]), e.found[revision(c)]) if e.found.get(revision(c)) else c for c in checked], data, whole)
        except (Failure, Stale):  # the call runs again after a removal of a different open (begin())
            raise
        except Exception:
            if self.debug:
                raise
            return {'status': 'failed', 'reasons': [reason('storage_error')],
                    'items': [refused(['batch_aborted']) for _ in checked]}  # STS-ENV-3 rule 1, STS-ENV-5
        return {'status': 'ok', 'reasons': [], 'items': [item(ref=revision(c)) for c in checked]}

    def op_put(self, request: dict) -> dict:
        texts, encoded = request.get('descriptors'), request.get('payloads', {})
        if not (set(request) <= {'descriptors', 'payloads'} and isinstance(texts, list) and texts
                and all(isinstance(t, str) for t in texts) and isinstance(encoded, dict)):
            raise fail('field_type')
        data = {k: unb64(v) for k, v in encoded.items()}
        out = self.commit_batch('put', self.check_batch(texts, data, incomplete=False), data)
        return self.envelope('put', out['status'], out['reasons'], out['items'])

    # Exact reads (documents 06, 07, 09 and 12)

    def op_get(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'refs': (True, is_refs)})
        context, members = self.members(request)
        refs = request['refs']
        items = [item(ref=r) if (code := self.locate(r, members)) is None else refused([code], [r]) for r in refs]
        return self.result('get', items, context)  # QRY-GET-1, QRY-GET-2

    def op_read(self, request: dict) -> dict:
        strict(request, {'context': (True, is_str), 'ref': (True, is_digest), 'selector': (False, is_read_selector)})  # CRD-READ-1
        context, members = self.members(request)
        ref, selector = request['ref'], request.get('selector')
        return self.result('read', [self.read_item(ref, selector, members)], context)

    def read_item(self, ref: str, selector: dict | None, members: set[str]) -> dict:
        """The checks of CRD-READ-2, in order, and the item of CRD-READ-3."""
        code = self.locate(ref, members)
        if code:
            return refused([code], [ref])
        desc = self.descriptor(ref)
        profile = selector['selector_profile'] if selector else None
        target = {d.BYTES: 'payload' in desc, d.NFC_TEXT: desc['kind'] == 'reading',
                  ROW: desc['kind'] == 'data-resource', None: True}
        if not target.get(profile, False):
            return refused(['unsupported_selector'], [ref])  # CRD-SEL-2
        if profile == ROW and desc['data_profile'] != 'aavanam/table/v1':
            return refused(['unsupported_profile'], [ref])  # CRD-SEL-5, DAT-PRF-2
        data = self.blob(desc['payload']['digest'], ref=ref) if 'payload' in desc else None
        if 'payload' in desc and data is None:
            return refused(['payload_missing'], [ref])
        out = item(ref=ref, kind=desc['kind'], descriptor=desc, selector=selector, data=None, row=None)
        if profile == ROW:
            table = self.table(desc)
            if table is None:
                return refused(['payload_form'], [ref])  # DAT-TBL-3
            if selector['index'] >= len(table['rows']):
                return refused(['selector_range'], [ref])
            out['row'] = dict(zip(table['columns'], table['rows'][selector['index']]))  # CRD-SEL-5
            return out
        if profile is not None:
            data = self.select(ref, selector)  # CRD-SEL-3, CRD-SEL-4
            if isinstance(data, dict):
                return data
        out['data'] = b64(data) if data is not None else None  # CRD-READ-3, CRD-READ-5
        return out

    def subject_of(self, request: dict) -> tuple[str, set[str], str]:
        """The context and the subject of an evidence or observations request (REL-EVD-1, OBS-SUB-1)."""
        form(request, {'context': (True, is_str), 'subject': (True, is_digest)})
        context, members = self.members(request)
        subject = request['subject']
        code = self.locate(subject, members)
        if code:
            raise fail(code, refs=[subject])
        return context, members, subject

    def records(self, members: set[str], kinds) -> list[tuple[str, dict]]:
        return [(r, self.descriptor(r)) for r in sorted(members) if self.kind(r) in kinds]

    def op_evidence(self, request: dict) -> dict:
        context, members, subject = self.subject_of(request)
        reachable = {m for _, s in self.records(members, {'relation-set'}) for m in s.get('members', [])}  # REL-RET-3
        reachable &= members  # CTX-OPN-4, REL-RMV-13: a record that is in the store but not in the context gives no item
        supports = [r for r, e in self.records(reachable, {'evidence'}) if e['subject'] == subject]  # REL-EVD-2
        items = [item(relation='support', ref=r) for r in supports]
        items += [item(relation='withdrawal', ref=r, target=w['target'], origin=w['origin'])
                  for r, w in self.records(reachable, {'retraction', 'supersession'}) if w['target'] in supports]  # REL-EVD-3
        return self.result('evidence', items, context)

    def op_observations(self, request: dict) -> dict:
        context, members, subject = self.subject_of(request)
        items = [item(ref=r) for r, o in self.records(members, {'observation'}) if subject in o['subjects']]  # OBS-SUB-2
        return self.result('observations', items, context)

    def select(self, ref: str, selector: dict) -> bytes | dict:
        """Apply a selector to a target (REL-SEL-2 to REL-SEL-4, ADP-SEL-4)."""
        desc = self.descriptor(ref)
        profile = selector['selector_profile']
        if profile not in (d.BYTES, d.NFC_TEXT) or (profile == d.NFC_TEXT and desc['kind'] != 'reading'):
            return refused(['unsupported_selector'], [ref])
        if 'payload' not in desc:
            return refused(['unsupported_selector'], [ref])
        data = self.blob(desc['payload']['digest'], ref=ref)
        if data is None:
            return refused(['payload_missing'], [ref])
        if profile == d.NFC_TEXT:
            try:
                text = d.decode_nfc(data)
            except ValueError:
                return refused(['payload_form'], [ref])
            if selector['end'] > len(text):
                return refused(['selector_range'], [ref])
            return text[selector['start']:selector['end']].encode('utf-8')
        if selector['end'] > len(data):
            return refused(['selector_range'], [ref])
        return data[selector['start']:selector['end']]

    def op_compare(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'before': (True, is_digest), 'after': (True, is_digest), 'selector': (True, d.is_selector)})
        context, members = self.members(request)
        before, after, selector = request['before'], request['after'], request['selector']
        located = [refused([code], [r]) for r in (before, after) if (code := self.locate(r, members))]  # CHG-SEL-1
        if located:
            return self.result('compare', [joined(located)], context)
        selected = [self.select(r, selector) for r in (before, after)]
        bad = [s for s in selected if isinstance(s, dict)]
        if bad:  # CHG-SEL-3
            return self.result('compare', [joined(bad)], context)
        return self.result('compare', [item(change='unchanged' if selected[0] == selected[1] else 'changed')], context)

    # Input locks (document 10)

    def checked_refs(self, refs: list[tuple[str, tuple]], members: set[str]) -> None:
        """LCK-EXE-1, LCK-EXE-2, LCK-BLD-1 and LCK-BLD-2. Each reference has its permitted kinds."""
        located = [(r, code) for r, _ in refs if (code := self.locate(r, members))]
        if located:
            raise Failure('not_found', [reason(code, [r]) for r, code in located])
        wrong = [r for r, permitted in refs if self.kind(r) not in permitted]
        if wrong:
            raise Failure('unsupported', [reason('unsupported_target_kind', [r]) for r in wrong])

    def bindings_problems(self, binding_set: dict) -> None:
        """LCK-EXE-3, BND-ROL-2 and BND-RUL-2, in this order."""
        bindings = binding_set.get('bindings', {})
        artifacts = [(r, a) for r in bindings.values() if (a := self.descriptor(r))['kind'] == 'artifact']
        unbound = [role for _, a in artifacts for role in a.get('declared_roles', {}) if role not in bindings]
        if unbound:
            raise Failure('unresolved', [reason('unbound_role') for _ in unbound])
        mismatch = []
        for _, a in artifacts:
            for role, schema in a.get('declared_roles', {}).items():
                bound = self.descriptor(bindings[role])
                if not (bindings[role] == schema or (bound['kind'] in ('data-resource', 'artifact') and bound.get('schema') == schema)):
                    mismatch.append(bindings[role])
        if mismatch:
            raise Failure('incompatible', [reason('role_schema_mismatch', [r]) for r in mismatch])
        profiles = set(binding_set.get('runtime_profiles', []))
        wrong = [r for r, a in artifacts if 'runtime_profile' in a and a['runtime_profile'] not in profiles]
        if wrong:
            raise Failure('incompatible', [reason('runtime_profile_mismatch', [r]) for r in sorted(set(wrong))])

    def required(self, request: dict, capabilities: list[str]) -> None:
        """LCK-CAP-2: one reason for each name of `requires` that is not in the table or not in the context."""
        reasons = [reason('unsupported_profile' if c not in self.capabilities else 'capability_unavailable', (), [c])
                   for c in request.get('requires', []) if c not in capabilities]
        if reasons:
            raise Failure('unsupported', reasons)

    def lock(self, operation: str, request: dict, context: str, covered: list[str], members: Members) -> dict:
        """Write the lock of the request and return the closure that it covers (LCK-EXE-4, LCK-BLD-3).
        A device store writes the lock to its local output store (BUN-IMP-7). In a layered context, the lock
        has the base snapshot and the layer in `view` (LYR-LCK-1)."""
        fields = {k: v for k, v in request.items() if k != 'context'}
        if members.view:
            fields['view'] = members.view
        desc = d.check({'descriptor_profile': BASE_PROFILE, 'kind': LOCK_KINDS[operation], **fields}, self.profiles)
        ref = revision(desc)
        # REL-RMV-10, REL-RMV-13: no lock over a closure with a removed object. The read comes before the write (STS-ENV-4).
        items = [item(ref=r) for r in sorted(self.checked_closure(covered, members))]
        target = self.output or self
        with target.mutex:  # the local output store can have calls of its own
            if target is not self:  # begin() of the output store also reads the file of this device store (BUN-RMV-1)
                target.fresh()
                target.wrote = False
            try:
                target.write({ref: desc}, {})
            finally:
                target.wrote = True
        return self.envelope(operation, 'ok', [], items, context, lock=ref)

    def indexes_form(self) -> dict:
        """The field `indexes` of a lock request, only for a store that declares the retrieval index profile (IDX-LCK-1)."""
        return {'indexes': (False, lambda v: is_refs(v, empty=True))} if INDEX_PROFILE in self.profiles else {}

    @staticmethod
    def indexes_of(request: dict) -> list[tuple[str, tuple]]:
        """Each index of a lock request with its permitted kind (IDX-LCK-2)."""
        return [(r, ('retrieval-index',)) for r in request.get('indexes', [])]

    def stale(self, index: str, context: str) -> str | None:
        """The snapshot root of the context that makes the index stale, or None (IDX-STL-1): a root of the
        collection of the index snapshot that is a different snapshot. A context has at most one (CTX-CMP-1)."""
        snapshot = self.descriptor(index)['snapshot']
        collection = self.descriptor(snapshot)['collection']
        roots = json.loads(self.db.execute('SELECT roots FROM contexts WHERE id = ?', (context,)).fetchone()[0])
        return next((r for r in sorted(set(roots)) if r != snapshot and self.kind(r) == 'snapshot'
                     and self.descriptor(r)['collection'] == collection), None)

    def fresh_indexes(self, request: dict, context: str) -> None:
        """IDX-LCK-2: one index_stale reason for each stale index of a lock request."""
        reasons = [reason('index_stale', [r, root]) for r in request.get('indexes', []) if (root := self.stale(r, context))]
        if reasons:
            raise Failure('incompatible', reasons)

    def lock_context(self, request: dict, operation: str) -> tuple[str, Members, list[str]]:
        """The read context of a lock request, with the capability of the operation (CTX-PRJ-4)."""
        context, members = self.members(request, operation)
        return context, members, self.context_of(request)[2]

    def op_execution_inputs(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'binding_set': (True, is_digest), 'context_inputs': (False, lambda v: is_refs(v, empty=True)),
                       'requires': (False, is_names), 'consumer': (True, is_text), 'completeness': (True, is_label),
                       **self.indexes_form()})
        counted(request, 'context_inputs')  # LCK-EXE-1
        counted(request, 'requires')  # LCK-CAP-1
        counted(request, 'indexes')  # IDX-LCK-1
        context, members, capabilities = self.lock_context(request, 'execution_inputs')
        inputs = request.get('context_inputs', [])
        binding_set, permitted = request['binding_set'], d.targets('execution-lock', 'context_inputs')
        self.checked_refs([(binding_set, ('binding-set',))] + [(r, permitted) for r in inputs] + self.indexes_of(request), members)
        self.required(request, capabilities)
        self.fresh_indexes(request, context)
        self.bindings_problems(self.descriptor(binding_set))
        return self.lock('execution_inputs', request, context, [binding_set, *inputs], members)

    def op_build_inputs(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'inputs': (True, lambda v: is_refs(v, empty=True)),
                       'requires': (False, is_names), 'consumer': (True, is_text), 'completeness': (True, is_label),
                       **self.indexes_form()})
        counted(request, 'inputs', many=True)  # LCK-BLD-1
        counted(request, 'requires')  # LCK-CAP-1
        counted(request, 'indexes')  # IDX-LCK-1
        context, members, capabilities = self.lock_context(request, 'build_inputs')
        inputs = request['inputs']
        self.checked_refs([(r, d.targets('build-lock', 'inputs')) for r in inputs] + self.indexes_of(request), members)
        self.required(request, capabilities)
        self.fresh_indexes(request, context)
        return self.lock('build_inputs', request, context, inputs, members)

    # Bundles (document 11)

    @staticmethod
    def redistribution_values(values) -> bool:
        """The form of BUN-RED-1."""
        return (isinstance(values, dict) and set(values) == {'license', 'redistribution'}
                and (values['license'] is None or (isinstance(values['license'], str) and values['license'] != ''))
                and isinstance(values['redistribution'], str) and values['redistribution'] in DECLARED)

    def redistribution(self, ref: str, statements: list[dict]) -> tuple[str, str | None]:
        """The redistribution value and the license value of an object (BUN-RED-2). No value is inherited (BUN-RED-3)."""
        if not d.payloads(self.descriptor(ref)):
            return 'no_payload', None
        mine = [o['values'] for o in statements if ref in o['subjects']]
        if not all(self.redistribution_values(v) for v in mine):
            return 'invalid', None
        if not mine:
            return 'undeclared', None
        if len({(v['license'], v['redistribution']) for v in mine}) > 1:
            return 'conflict', None
        return mine[0]['redistribution'], mine[0]['license']

    def op_export(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'roots': (True, is_refs),
                       'permit': (False, lambda v: isinstance(v, list) and all(x in DECLARED for x in v if isinstance(x, str))
                                  and all(isinstance(x, str) for x in v)),  # BUN-RED-4
                       'filter': (False, is_filter),  # BUN-FLT-1
                       'base_context': (False, is_str)})  # BUN-DLT-1
        permit = request.get('permit')
        if permit is not None and len(set(permit)) != len(permit):
            raise fail('field_count')  # KND-SCH-5
        context, members = self.members(request, 'export')
        base = self.base_roots(request['base_context']) if 'base_context' in request else None  # BUN-DLT-1: before the roots
        roots = request['roots']
        located = [(r, code) for r in roots if (code := self.locate(r, members))]
        if located:  # BUN-EXP-1
            return self.envelope('export', 'not_found', [reason(code, [r]) for r, code in located], [], context)
        flt = request.get('filter')
        if flt is not None:  # BUN-FLT-1 to BUN-FLT-3: the roots of the bundle are the units that match
            # LYR-EXP-2: a candidate is a member, so a replaced unit of a layered context is not a candidate
            checked = self.filtered(flt, sorted(r for r in self.closure(roots) & members if self.kind(r) == 'knowledge-unit'), members)
            failed = [o for _, o in checked if isinstance(o, dict)]
            if failed:
                return self.envelope('export', *first_status(failed), [], context)
            roots = [r for r, o in checked if o]
            if not roots:
                return self.envelope('export', 'unresolved', [reason('no_match', request['roots'])], [], context)
            inside = sorted(r for r in self.closure(roots) if self.kind(r) == 'knowledge-unit' and r not in roots)
            if inside:  # BUN-FLT-2 rule 3: the closure of a match has a unit that does not match
                return self.envelope('export', 'incompatible', [reason('unmatched_dependency', [r]) for r in inside], [], context)
        objects = sorted(self.checked_closure(roots, members))  # REL-RMV-10, REL-RMV-13: no bundle of a closure with a removed object
        statements = [o for _, o in self.records(set(objects), {'observation'}) if o['observation_type'] == REDISTRIBUTION]
        values = {r: self.redistribution(r, statements) for r in objects}  # BUN-RED-2
        # BUN-RED-4 and BUN-RED-5: the bytes of a withheld object
        withheld = {p['digest'] for r in objects if permit is not None and values[r][0] != 'no_payload' and values[r][0] not in permit
                    for p in d.payloads(self.descriptor(r))}
        # BUN-DLT-2: a delta bundle has the objects that are not in the base closure, and no bytes of a base digest
        omitted = self.closure(base) if base is not None else set()
        kept = [r for r in objects if r not in omitted]
        base_digests = {p['digest'] for r in omitted for p in d.payloads(self.descriptor(r))}
        named = {p['digest'] for r in kept for p in d.payloads(self.descriptor(r))}
        # BUN-DLT-2 rule 3: the check of an excerpt reads the bytes of its parent (BUN-EXC-3), also for a parent of the base closure
        named |= {p['digest'] for r in kept if self.kind(r) == 'excerpt' for p in d.payloads(self.descriptor(self.descriptor(r)['parent']))}
        # BUN-IMP-12: only descriptors in this closure can withhold a shared digest.
        payloads = {}
        for ref in objects:
            for p in d.payloads(self.descriptor(ref)):
                key = p['digest']
                if key in withheld or key not in named:
                    continue
                data = self.blob(key, ref=ref)
                if data is None:
                    withheld.add(key)  # BUN-RED-7: any failed descriptor withholds the shared bytes
                    payloads.pop(key, None)
                else:
                    payloads[key] = data
        bundle = {'format': BUNDLE_FORMAT, 'roots': roots, 'descriptors': [canonical(self.descriptor(r)) for r in kept],
                  'payloads': {k: b64(data) for k, data in payloads.items()
                               if data is not None and k not in base_digests}}  # BUN-EXP-2, BUN-DLT-2 rule 2
        if withheld & named:
            bundle['withheld'] = sorted(withheld & named)  # BUN-RED-5, BUN-DLT-2 rule 3
        if base is not None:
            bundle['base'] = base  # BUN-DLT-2 rule 4
        items = [item(ref=r, redistribution=values[r][0], license=values[r][1],
                      withheld=any(p['digest'] in withheld for p in d.payloads(self.descriptor(r)))) for r in kept]  # BUN-RED-6
        if flt is not None:
            bundle['filter'] = flt  # BUN-FLT-4
            for i in items:
                i['matched'] = i['ref'] in roots  # BUN-FLT-3
        return self.envelope('export', 'ok', [], items, context, bundle=bundle)

    def op_import(self, request) -> dict:
        if is_raw(request):
            if self.parts is None:  # a stream gives its bytes one time, so a later run of the call uses these parts
                self.parts = raw_parts(request)  # BUN-RAW-4; then the import of its bundle (BUN-RAW-5)
            bundle, data = detached(self.parts[0]), dict(self.parts[1])
        else:
            bundle = request.get('bundle')
            if not (set(request) == {'bundle'} and is_manifest(bundle, BUNDLE_FORMAT) and isinstance(bundle['payloads'], dict)
                    and all(is_digest(k) for k in bundle['payloads'])
                    and not set(bundle.get('withheld', [])) & set(bundle['payloads'])):
                raise fail('field_type')  # BUN-IMP-1, BUN-MAN-1
            data = {k: unb64(v) for k, v in bundle['payloads'].items()}
        withheld = set(bundle.get('withheld', []))  # BUN-IMP-9; keep the full set for BUN-IMP-11
        checked = self.check_batch(bundle['descriptors'], data, incomplete=True, withheld=withheld)
        present = self.revisions(bundle['descriptors'])  # a root with a failed check is present (BUN-IMP-2)
        if 'base' in bundle:  # BUN-DLT-4: the store has each base root, and each root that the bundle does not have
            absent = [refused(['bundle_incomplete'], [r]) for r in bundle['roots'] if r not in present and self.descriptor(r) is None]
            absent += [refused(['bundle_incomplete'], [r]) for r in bundle['base'] if self.descriptor(r) is None]
        else:
            absent = [refused(['bundle_incomplete'], [r]) for r in bundle['roots'] if r not in present]
        imported = bundle['roots'] if self.role == 'device' else None  # BUN-RMV-1: the roots of each ok import
        out = self.commit_batch('import', checked, data, absent, withheld, imported)
        if out['status'] != 'ok':  # BUN-IMP-4
            return self.envelope('import', out['status'], out['reasons'], [])
        return self.envelope('import', 'ok', [], out['items'])

    # Lookup and discovery (document 09)

    def table(self, desc: dict) -> dict | None:
        """The table of a data resource under aavanam/table/v1, or None if the payload does not agree with DAT-TBL-1."""
        try:
            value = parse(self.blob(desc['payload']['digest'], ref=revision(desc)).decode('utf-8'))
        except (FormError, UnicodeDecodeError):
            return None
        if not (isinstance(value, dict) and set(value) == {'columns', 'key', 'rows'}):
            return None
        columns, key, rows = value['columns'], value['key'], value['rows']
        if not (isinstance(columns, list) and columns and all(isinstance(c, str) for c in columns)
                and len(set(columns)) == len(columns) and isinstance(key, list) and key
                and all(isinstance(k, str) and k in columns for k in key) and len(set(key)) == len(key)
                and isinstance(rows, list) and all(isinstance(r, list) and len(r) == len(columns) for r in rows)):
            return None
        positions = [columns.index(k) for k in key]
        keys = {canonical([r[i] for i in positions]) for r in rows}
        return value if len(keys) == len(rows) else None

    def overlay_value(self, candidates: list[dict], key: str, precedence: list[str]):
        """BND-OVL-3. Return (True, value), or (False, None) for overlay_conflict."""
        values = {canonical(o['values'][key]) for o in candidates}
        if len(values) == 1:
            return True, candidates[0]['values'][key]
        tier = [precedence.index(o['precedence']) if o.get('precedence') in precedence else len(precedence) for o in candidates]
        kept = [o for o, t in zip(candidates, tier) if t == min(tier)]
        if len({canonical(o['values'][key]) for o in kept}) == 1:
            return True, kept[0]['values'][key]
        return False, None

    def filtered(self, flt: dict, refs: list[str], members: set[str]) -> list[tuple[str, object]]:
        """Each reference with True for a match, False for no match, or the item of a failed check (DSC-FLT-2 to DSC-FLT-4)."""
        equals, ranges, precedence = flt.get('equals', {}), flt.get('ranges', []), flt.get('precedence', [])
        keys = list(dict.fromkeys([*equals, *(c[f] for c in ranges for f in ('lower', 'upper'))]))
        about: dict[str, list[dict]] = {}
        for _, o in self.records(members, {'overlay'}):  # only the overlays of the read context give a value
            if isinstance(o['values'], dict):
                about.setdefault(o['subject'], []).append(o)
        out = []
        for ref in refs:
            found = {k: [o for o in about.get(ref, []) if k in o['values']] for k in keys}
            absent = sum(not v for v in found.values())
            if absent:
                out.append((ref, refused(['no_metadata'] * absent, [ref])))
                continue
            applied = {k: self.overlay_value(v, k, precedence) for k, v in found.items()}
            conflicts = sum(not ok for ok, _ in applied.values())
            if conflicts:
                out.append((ref, refused(['overlay_conflict'] * conflicts, [ref])))
                continue
            value = {k: v for k, (_, v) in applied.items()}
            missing = sum(v is None for v in value.values())
            if missing:
                out.append((ref, refused(['missing_value'] * missing, [ref])))  # DAT-MIS-2
                continue
            wrong = sum(typed(c['type'], value[c[f]]) is None for c in ranges for f in ('lower', 'upper'))
            if wrong:
                out.append((ref, refused(['value_type'] * wrong, [ref])))  # DAT-RNG-5
                continue
            out.append((ref, all(canonical(value[k]) == canonical(v) for k, v in equals.items())
                        and all(range_matches(c, typed(c['type'], value[c['lower']]), typed(c['type'], value[c['upper']]))
                                for c in ranges)))
        return out

    def query_item(self, ref: str, request: dict, members: set[str]) -> dict:
        """The checks of QRY-DAT-2, in order, and the item of QRY-DAT-3."""
        code = self.locate(ref, members)
        if code:
            return refused([code], [ref])
        desc = self.descriptor(ref)
        if desc['kind'] != 'data-resource':
            return refused(['unsupported_target_kind'], [ref])
        if 'schema' in request and desc['schema'] != request['schema']:
            return refused(['schema_mismatch'], [ref])
        if desc['data_profile'] != 'aavanam/table/v1':
            return refused(['unsupported_profile'], [ref])
        if self.blob(desc['payload']['digest'], ref=ref) is None:
            return refused(['payload_missing'], [ref])
        table = self.table(desc)
        if table is None:
            return refused(['payload_form'], [ref])
        where, ranges = request.get('where', {}), request.get('ranges', [])
        unknown = [c for c in [*where, *(r[f] for r in ranges for f in ('lower', 'upper'))] if c not in table['columns']]
        if unknown:
            return refused(['unknown_column'] * len(unknown), [ref])
        metadata = {}
        if 'metadata' in request:
            overlays = [o for _, o in self.records(members, {'overlay'}) if o['subject'] == ref and isinstance(o['values'], dict)]
            found = {k: [o for o in overlays if k in o['values']] for k in request['metadata']}
            absent = [k for k, v in found.items() if not v]
            if absent:
                return refused(['no_metadata'] * len(absent), [ref])
            applied = {k: self.overlay_value(v, k, request.get('precedence', [])) for k, v in found.items()}
            conflicts = [k for k, (ok, _) in applied.items() if not ok]
            if conflicts:
                return refused(['overlay_conflict'] * len(conflicts), [ref])
            metadata = {k: v for k, (_, v) in applied.items()}
        columns = table['columns']
        rows = [dict(zip(columns, r)) for r in table['rows']
                if all(canonical(r[columns.index(c)]) == canonical(v) for c, v in where.items())]
        # QRY-DAT-5: the ranges of the rows that match where. A missing value stops before a value type.
        cells = [(r[c[f]], c['type']) for r in rows for c in ranges for f in ('lower', 'upper')]
        missing = sum(v is None for v, _ in cells)
        if missing:
            return refused(['missing_value'] * missing, [ref])  # DAT-MIS-2
        wrong = sum(typed(kind, v) is None for v, kind in cells)
        if wrong:
            return refused(['value_type'] * wrong, [ref])  # DAT-RNG-5
        rows = [r for r in rows
                if all(range_matches(c, typed(c['type'], r[c['lower']]), typed(c['type'], r[c['upper']])) for c in ranges)]
        offset, limit = request.get('offset', 0), request.get('limit')
        rest = rows[offset:]
        more = limit is not None and len(rest) > limit
        out = item(ref=ref, rows=rest[:limit] if limit is not None else rest, next=offset + limit if more else None)
        if 'metadata' in request:
            out['metadata'] = metadata
        return out

    def op_query_data(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'resources': (True, is_refs),
                       'where': (False, lambda v: isinstance(v, dict) and is_ijson(v)),
                       'ranges': (False, lambda v: isinstance(v, list) and v and all(map(is_range_clause, v))),
                       'metadata': (False, lambda v: isinstance(v, list) and v and all(isinstance(k, str) for k in v)
                                    and len(set(v)) == len(v)),
                       'precedence': (False, lambda v: isinstance(v, list) and all(isinstance(k, str) for k in v)),
                       'schema': (False, is_digest), 'offset': (False, is_count), 'limit': (False, lambda v: is_count(v, 1))})
        context, members = self.members(request, 'query_data')
        items = [self.query_item(r, request, members) for r in request['resources']]
        bounded = any(i['status'] == 'ok' and i['next'] is not None for i in items)  # QRY-DAT-4
        return self.result('query_data', items, context, bounded)

    def op_search(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'query': (True, lambda v: isinstance(v, str) and v != ''),
                       'search_profile': (False, is_str), 'offset': (False, is_count),
                       'limit': (False, lambda v: is_count(v, 1))})
        context, members = self.members(request, 'search')  # the capability comes before the profile (CTX-PRJ-4)
        if request.get('search_profile', 'aavanam/lexical/v1') != 'aavanam/lexical/v1':
            raise fail('unsupported_profile')  # QRY-LEX-1
        query, matches = request['query'].encode('utf-8'), []
        for ref, desc in self.records(members, {'reading', 'excerpt'}):  # QRY-LEX-2
            data = self.blob(desc['payload']['digest'], ref=ref)
            count = data.count(query) if data is not None else 0
            if count:
                matches.append((-count, ref, desc['kind']))
        matches.sort()
        offset, limit = request.get('offset', 0), request.get('limit')
        rest = matches[offset:]
        bounded = limit is not None and len(rest) > limit
        items = [item(ref=r, kind=k, count=-c) for c, r, k in (rest[:limit] if limit is not None else rest)]  # QRY-LEX-3
        return self.result('search', items, context, bounded)

    def op_search_text(self, request: dict) -> dict:
        strict(request, {'context': (True, is_str), 'query': (True, is_query), 'offset': (False, is_count),
                         'limit': (False, lambda v: is_count(v, 1))})  # CRD-TXT-1 steps 1 to 3
        context, members = self.members(request, 'search_text')  # steps 4 and 5
        roots = json.loads(self.db.execute('SELECT roots FROM contexts WHERE id = ?', (context,)).fetchone()[0])
        closures = {s: self.closure([s]) for s in sorted(set(roots)) if self.kind(s) == 'snapshot'}  # CRD-TXT-6 rule 2
        query = [w for _, _, w in words(request['query'].encode('utf-8'))]
        n, items = len(query), []
        for ref, desc in self.records(members, {'artifact'}):  # CRD-TXT-2, CRD-ORD-1: ascending ref
            if not is_text_media(desc['payload']['media_type']):
                continue
            data = self.blob(desc['payload']['digest'], ref=ref)
            if data is None:
                items.append(refused(['payload_missing'], [ref]))  # CRD-TXT-5
                continue
            snapshots = [s for s, closure in closures.items() if ref in closure]
            found = words(data)
            folded = [w for _, _, w in found]
            for i in range(len(found) - n + 1):  # CRD-TXT-4: each word start, so matches can overlap
                if folded[i:i + n] == query:
                    selector = {'selector_profile': d.BYTES, 'start': found[i][0], 'end': found[i + n - 1][1]}
                    items.append(item(ref=ref, snapshots=snapshots, selector=selector))
        offset, limit = request.get('offset', 0), request.get('limit')
        rest = items[offset:]
        bounded = limit is not None and len(rest) > limit  # CRD-ORD-2
        return self.result('search_text', rest[:limit] if limit is not None else rest, context, bounded)

    # The retrieval index profile (document 24). The index is a projection of its snapshot: the store keeps no
    # derived data, so a search makes the bucket sets again from the snapshot (IDX-DEF-2, IDX-SRC-7).

    def op_semantic_search(self, request: dict) -> dict:
        strict(request, {'context': (True, is_str), 'index': (True, is_digest), 'query': (True, is_query), 'offset': (False, is_count),
                         'limit': (False, lambda v: is_count(v, 1)), 'filter': (False, is_filter)})  # IDX-SRC-1 steps 1 to 3
        context, members = self.members(request, 'semantic_search')  # steps 4 and 5
        index = request['index']
        code = self.locate(index, members)  # IDX-SRC-2
        if code:
            raise fail(code, refs=[index])
        if self.kind(index) != 'retrieval-index':
            raise fail('unsupported_target_kind', refs=[index])
        root = self.stale(index, context)
        if root:
            raise fail('index_stale', refs=[index, root])  # IDX-STL-1
        desc = self.descriptor(index)
        if desc['method'] != HASHED_TERMS:  # IDX-MTH-2: the store does not run an external method
            raise Failure('unsupported', [reason('unsupported_profile', [index], [desc['method']])])
        dimensions, snapshot = desc['parameters']['dimensions'], desc['snapshot']
        wanted = buckets(request['query'].encode('utf-8'), dimensions)
        scored, absent = [], []
        for unit in self.descriptor(snapshot).get('units', []):  # IDX-MTH-3: the items are the units of the snapshot
            found: set[int] | None = set()
            for ref in sorted(self.closure([unit])):
                part = self.descriptor(ref)
                if part['kind'] == 'artifact' and is_text_media(part['payload']['media_type']):
                    data = self.blob(part['payload']['digest'], ref=ref)
                    if data is None:
                        found = None  # IDX-SRC-4: no score from a part of the texts
                        break
                    found |= buckets(data, dimensions)
            if found is None:
                absent.append(unit)
            elif found & wanted:
                scored.append((-len(found & wanted), 'logical_id' not in self.descriptor(unit), self.descriptor(unit).get('logical_id', ''), unit))
        entries = [item(ref=unit, kind='knowledge-unit', logical_id=None if none else lid, snapshot=snapshot, index=index, score=-score)
                   for score, none, lid, unit in sorted(scored)]  # IDX-SRC-3, IDX-SRC-6
        if 'filter' in request:  # IDX-SRC-5: a match keeps its item, a failed check gives its outcome, and no match gives no item
            checked = self.filtered(request['filter'], [e['ref'] for e in entries], members)
            entries = [e if o is True else o for e, (_, o) in zip(entries, checked) if o is not False]
        entries += [refused(['payload_missing'], [unit]) for unit in sorted(absent)]
        items, bounded = self.page(request, entries)
        return self.result('semantic_search', items, context, bounded)

    def distances(self, start, members: set[str], forward: bool) -> dict[str, int]:
        """The smallest number of reference steps from the start objects, in the read context (QRY-BDL-3, CHG-IMP-4)."""
        out, todo = {r: 0 for r in start}, deque(start)
        while todo:
            ref = todo.popleft()
            for other in self.targets(ref) if forward else self.sources(ref):
                if other not in out and other in members:
                    out[other] = out[ref] + 1
                    todo.append(other)
        return out

    def located_items(self, refs: list[str], members: set[str]) -> list[dict]:
        """QRY-BDL-2 and CHG-IMP-2: one item for each reference that is not in the read context."""
        return [item(REASON_STATUS[code], [reason(code, [r])], ref=r) for r in refs if (code := self.locate(r, members))]

    def op_context_bundle(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'roots': (True, is_refs),
                       'budget': (False, lambda v: isinstance(v, dict) and set(v) == {'unit', 'max'}
                                  and isinstance(v['unit'], str) and is_count(v['max']))})
        budget = request.get('budget')
        if budget and budget['unit'] != 'bytes':
            raise fail('unsupported_budget')  # QRY-BDL-1
        context, members = self.members(request)
        located = self.located_items(request['roots'], members)
        if located:
            return self.result('context_bundle', located, context)
        distance = self.distances(request['roots'], members, forward=True)
        items, total = [], 0
        for ref in sorted(distance, key=lambda r: (distance[r], r)):
            size = sum(p['size'] for p in d.payloads(self.descriptor(ref)))
            if budget and total + size > budget['max']:
                return self.result('context_bundle', items, context, bounded=True)  # QRY-BDL-4
            total += size
            items.append(item(ref=ref, kind=self.kind(ref), size=size))
        return self.result('context_bundle', items, context)

    # Coverage and impact (documents 07 and 12)

    @staticmethod
    def coverage_values(values) -> bool:
        """The form of OBS-COV-1."""
        def omission(o):
            return (isinstance(o, dict) and set(o) <= {'reason', 'selector'} and isinstance(o.get('reason'), str)
                    and o['reason'] != '' and ('selector' not in o or d.is_selector(o['selector'])))
        return (isinstance(values, dict) and set(values) == {'unit', 'covered', 'total', 'omissions'}
                and isinstance(values['unit'], str) and values['unit'] != '' and is_count(values['covered'])
                and (values['total'] is None or is_count(values['total'], values['covered']))
                and isinstance(values['omissions'], list) and all(omission(o) for o in values['omissions']))

    def coverage_of(self, subject: str, members: set[str]) -> list[tuple[str, dict]]:
        return [(r, o) for r, o in self.records(members, {'observation'})
                if o['observation_type'] == COVERAGE and subject in o['subjects']]

    def op_coverage(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'subject': (True, is_digest),
                       'observation_type': (False, is_str)})
        if request.get('observation_type', COVERAGE) != COVERAGE:
            raise fail('unsupported_profile')  # OBS-COV-2
        context, members = self.members(request)
        subject = request['subject']
        code = self.locate(subject, members)
        if code:
            raise fail(code, refs=[subject])
        items = [item(ref=r, **o['values']) if self.coverage_values(o['values']) else refused(['values_form'], [r])
                 for r, o in self.coverage_of(subject, members)]  # OBS-COV-3
        return self.result('coverage', items, context)

    def op_impact(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'changed': (True, is_refs),
                       'dependents': (False, is_str), 'limit': (False, lambda v: is_count(v, 1))})
        if request.get('dependents', 'declared') != 'declared':
            raise fail('unsupported_dependents')  # CHG-IMP-1
        context, members = self.members(request)
        changed = request['changed']
        located = self.located_items(changed, members)
        if located:
            return self.result('impact', located, context)  # CHG-IMP-2
        distance = self.distances(changed, members, forward=False)  # CHG-IMP-3
        dependents = sorted((r for r in distance if r not in changed), key=lambda r: (distance[r], r))
        limit = request.get('limit')
        bounded = limit is not None and len(dependents) > limit
        items = [item(ref=r, kind=self.kind(r), distance=distance[r]) for r in dependents[:limit]]  # CHG-IMP-4
        return self.result('impact', items, context, bounded)

    # Publication (document 13)

    def op_create_snapshot(self, request: dict) -> dict:
        given = [f for f in ('descriptor_profile', 'kind') if f in request]
        if given:  # PUB-SNP-1: these fields are not in the request
            return self.result('create_snapshot', [refused(['unknown_field'] * len(given))])
        desc = {**request, 'descriptor_profile': BASE_PROFILE, 'kind': 'snapshot'}  # PUB-SNP-1
        out = self.commit_batch('create_snapshot', self.check_batch([desc], {}, incomplete=False), {})  # PUB-SNP-2
        return self.envelope('create_snapshot', out['status'], out['reasons'], out['items'])

    def heads(self, names: list[str]) -> dict[str, str | None]:
        """The head of each name, from one SQL statement: one atomic read, also with other connections (CTX-VIW-3)."""
        marks = ', '.join('?' * len(names))
        found = dict(self.db.execute(f'SELECT collection, snapshot FROM heads WHERE collection IN ({marks})', names))
        return {n: found.get(n) for n in names}

    def head(self, collection: str) -> str | None:
        row = self.db.execute('SELECT snapshot FROM heads WHERE collection = ?', (collection,)).fetchone()
        return row[0] if row else None

    def op_create_candidate(self, request: dict) -> dict:
        strict(request, {'snapshot': (True, is_digest),
                         'checks': (False, lambda v: is_refs(v, empty=True)),
                         'policy': (True, is_str),
                         'expected_head': (True, lambda v: v is None or is_digest(v))}, sets=('checks',))  # PUB-CND-1
        snapshot, checks = request['snapshot'], sorted(request.get('checks', []))
        identity = revision({'format': 'aavanam-candidate/v1', 'snapshot': snapshot, 'checks': checks,
                             'policy': request['policy'], 'expected_head': request['expected_head']})  # PUB-CND-3

        def outcome(*codes, refs=()):
            return self.result('create_candidate', [refused(list(codes), refs)])

        unknown = [r for r in [snapshot, *checks] if self.descriptor(r) is None]
        if unknown:  # PUB-CND-2 step 1
            return self.result('create_candidate', [item('not_found', [reason('unknown_ref', [r]) for r in unknown])])
        wrong = [r for r in [snapshot] if self.kind(r) != 'snapshot'] + [r for r in checks if self.kind(r) != 'check']
        if wrong:  # step 2
            return self.result('create_candidate', [item('unsupported', [reason('unsupported_target_kind', [r]) for r in wrong])])
        if request['policy'] not in self.policies:  # step 3
            return outcome('unsupported_profile')
        closure = self.closure([snapshot])
        outside = [r for r in checks if self.descriptor(r)['subject'] not in closure]
        if outside:  # step 4
            return self.result('create_candidate', [item('incompatible', [reason('check_outside_snapshot', [r]) for r in outside])])
        collection = self.descriptor(snapshot)['collection']
        ok = item(candidate=identity, collection=collection)
        self.begin()
        try:
            if self.db.execute('SELECT 1 FROM candidates WHERE id = ?', (identity,)).fetchone():
                self.db.execute('COMMIT')
                return self.result('create_candidate', [ok])  # step 5, PUB-CND-4
            if self.head(collection) != request['expected_head']:
                self.db.execute('ROLLBACK')
                return outcome('head_moved')  # step 6
            self.db.execute('INSERT INTO candidates VALUES (?, ?, ?, ?, ?)',
                            (identity, snapshot, collection, request['policy'], request['expected_head']))
            self.db.execute('COMMIT')
        except BaseException:
            self.rollback()
            raise
        return self.result('create_candidate', [ok])

    def published(self, candidate: str, collection: str) -> dict | None:
        """The item of step 3 of PUB-HED-3, if the store published the candidate before (PUB-HED-4, PUB-HED-5)."""
        row = self.db.execute('SELECT 1 FROM publications WHERE candidate = ?', (candidate,)).fetchone()
        if row is None:
            return None
        snapshot = self.db.execute('SELECT snapshot FROM candidates WHERE id = ?', (candidate,)).fetchone()[0]
        head = self.head(collection)
        return item(candidate=candidate, disposition='already_published' if head == snapshot else 'superseded', head=head)

    def covered(self, lock: str) -> set[str]:
        """The covered set of a lock (PUB-ACT-2): the closure of its items, without the lock descriptor."""
        desc = self.descriptor(lock)
        if desc['kind'] == 'execution-lock':  # LCK-EXE-4, LCK-EXE-5
            return self.closure([desc['binding_set'], *desc.get('context_inputs', [])])
        return self.closure(desc['inputs'])  # LCK-BLD-3

    def members_of(self, snapshot: str | None, fields=MEMBER_FIELDS) -> set[str]:
        """The members of a snapshot (PUB-ACT-2). No snapshot has no members."""
        desc = self.descriptor(snapshot) if snapshot else {}
        return {r for f in fields for r in desc.get(f, [])}

    def removed_members(self, base: str | None, snapshot: str) -> set[str]:
        """The base members that the snapshot does not keep and that no member of it replaces (PUB-ACT-2)."""
        new = self.members_of(snapshot)
        keys = {(self.kind(r), self.descriptor(r)['logical_id']) for r in new if 'logical_id' in self.descriptor(r)}
        return {r for r in self.members_of(base) - new
                if (self.kind(r), self.descriptor(r).get('logical_id')) not in keys or 'logical_id' not in self.descriptor(r)}

    def activation_checks(self, identity: str, snapshot: str, collection: str, expected, activation) -> list[dict]:
        """The reasons of the first activation check that fails, or no reasons (PUB-ACT-3, PUB-ACT-4).

        The checks read only immutable objects and the gate declarations. They do not evaluate a date, a person or a payload (PUB-ACT-5).
        """
        gated = self.gated(collection)
        if activation is None:  # check 1 (PUB-GAT-3). Without an activation, the store applies no other check.
            return [reason('activation_required')] if gated else []
        act = self.descriptor(activation)
        wrong = [act['candidate'] != identity, act.get('base') != expected, act['partition'] != collection]
        if any(wrong):  # check 2: one reason for each field that does not agree
            return [reason('activation_mismatch', [activation]) for w in wrong if w]
        covered = self.covered(act['lock'])
        snapshots = sorted(r for r in covered if self.kind(r) == 'snapshot')
        if snapshots:  # check 3
            return [reason('lock_has_snapshot', [r]) for r in snapshots]
        outside = sorted(covered - self.closure([snapshot]))
        if outside:  # check 4 (KND-DEP-3)
            return [reason('lock_outside_snapshot', [r]) for r in outside]
        changed = self.members_of(snapshot, MEMBER_FIELDS[:-1]) - self.members_of(act.get('base'))
        uncovered = sorted(changed - covered)
        if uncovered:  # check 5: not for native mappings
            return [reason('change_outside_lock', [r]) for r in uncovered]
        listed = set(act.get('removed', []))
        refused_refs = (self.removed_members(act.get('base'), snapshot) ^ listed) | (listed if gated else set())
        return [reason('removal_refused', [r]) for r in sorted(refused_refs)]  # check 6

    def gate_checks(self, collection: str, activation) -> list[dict]:
        """Check 1 and the empty rule of check 6 again, in the atomic step (PUB-ACT-3). A gate can start during the validation."""
        if not self.gated(collection):
            return []
        if activation is None:
            return [reason('activation_required')]
        return [reason('removal_refused', [r]) for r in sorted(self.descriptor(activation).get('removed', []))]

    def op_publish(self, request: dict) -> dict:
        strict(request, {'candidate': (True, is_digest), 'receipt': (True, is_digest),
                         'activation': (False, is_digest)})  # PUB-HED-2
        identity, receipt, activation = request['candidate'], request['receipt'], request.get('activation')
        refs = [receipt] + ([activation] if activation is not None else [])
        row = self.db.execute('SELECT snapshot, collection, policy, expected_head FROM candidates WHERE id = ?',
                              (identity,)).fetchone()
        missing = ([reason('unknown_candidate')] if row is None else []) + \
                  [reason('unknown_ref', [r]) for r in refs if self.descriptor(r) is None]
        if missing:  # PUB-HED-3 step 1
            return self.result('publish', [item('not_found', missing, candidate=identity)])
        wrong = [r for r, k in zip(refs, ('receipt', 'activation')) if self.kind(r) != k]
        if wrong:  # step 2
            return self.result('publish', [item('unsupported', [reason('unsupported_target_kind', [r]) for r in wrong],
                                                candidate=identity)])
        snapshot, collection, policy, expected = row
        before = self.published(identity, collection)
        if before:  # step 3: no validator call and no activation check
            return self.result('publish', [before])
        failed = self.activation_checks(identity, snapshot, collection, expected, activation)
        if failed:  # PUB-ACT-3, before step 4: no validator call
            return self.result('publish', [item('incompatible', failed, candidate=identity)])
        validate = self.policies.get(policy)
        held = [self.device.mutex, self.mutex] if self.device else [self.mutex]
        for mutex in reversed(held):  # the store keeps no update open while the validator runs (PUB-RCP-3)
            mutex.release()
        try:
            accepted = validate is not None and validate(identity, receipt, activation) is True  # PUB-RCP-2
        except TimeoutError:  # PUB-RCP-4: no decision, so no receipt_rejected and no change of the store
            raise fail('deadline_exceeded') from None
        finally:
            for mutex in held:  # the order of call()
                mutex.acquire()
        if not accepted:  # step 4
            return self.result('publish', [item('incompatible', [reason('receipt_rejected', [receipt])], candidate=identity)])
        self.db.execute('BEGIN IMMEDIATE')  # step 5, one atomic step
        try:
            kept = self.db.execute('SELECT count(*) FROM objects WHERE ref IN (%s)' % ', '.join('?' * len(refs)), refs).fetchone()[0]
            if kept != len(set(refs)) or not self.db.execute('SELECT 1 FROM candidates WHERE id = ?', (identity,)).fetchone():
                self.cache.clear()  # a `gc_collect` removed an object of the request while the validator ran (LIF-GC-5)
                raise Stale  # the call runs again, and step 1 gives the result
            again = self.published(identity, collection)
            if again:  # 5.1
                self.db.execute('COMMIT')
                return self.result('publish', [again])
            failed = self.gate_checks(collection, activation)
            if failed:  # 5.1: checks 2 to 5 and the exact rule of check 6 read immutable objects only
                self.db.execute('ROLLBACK')
                return self.result('publish', [item('incompatible', failed, candidate=identity)])
            head = self.head(collection)
            if head != expected:  # 5.2
                self.db.execute('ROLLBACK')
                return self.result('publish', [item('incompatible', [reason('head_moved')], candidate=identity, head=head)])
            self.db.execute('INSERT OR REPLACE INTO heads VALUES (?, ?)', (collection, snapshot))  # 5.3
            self.db.execute('INSERT INTO publications VALUES (?, ?, ?, ?, ?, ?)',
                            (identity, receipt, activation, collection, head, snapshot))  # PUB-LOG-2
            self.db.execute('COMMIT')
        except BaseException:
            self.rollback()
            raise
        return self.result('publish', [item(candidate=identity, disposition='published', head=snapshot)])

    def op_publications(self, request: dict) -> dict:
        """The recorded publications of one collection, in the order of the head updates (PUB-LOG-1 to PUB-LOG-4)."""
        strict(request, {'collection': (True, is_str)})  # PUB-LOG-1
        rows = self.db.execute('SELECT candidate, receipt, activation, previous_head, head FROM publications '
                               'WHERE collection = ? ORDER BY rowid', (request['collection'],))
        return self.result('publications', [item(candidate=c, receipt=r, activation=a, previous_head=p, head=h)
                                            for c, r, a, p, h in rows])

    # Lifecycle and discovery (documents 17 and 18)

    def known_kinds(self, request: dict) -> None:
        """Step 4 of LIF-CHG-1 and step 5 of DSC-LST-1: one unknown_kind for each name that is not a registered kind.
        A kind of a profile that the store does not declare is not registered (KND-REG-2, LYR-PRF-1)."""
        unknown = [k for k in request.get('kinds', []) if not registered(k, self.profiles)]
        if unknown:
            raise Failure('unsupported', [reason('unknown_kind') for _ in unknown])

    def content(self, snapshot: str | None) -> set[str]:
        """The closure of the snapshot without the snapshot (LIF-ITM-1). A null snapshot has no content."""
        return self.closure([snapshot]) - {snapshot} if snapshot else set()

    def item_keys(self, refs) -> dict[tuple, list[str]]:
        """The revisions of each item key, in ascending order (LIF-ITM-2). The key sorts in the item order (LIF-ITM-3)."""
        out: dict[tuple, list[str]] = {}
        for ref in sorted(refs):
            desc = self.descriptor(ref)
            lid = desc.get('logical_id')
            out.setdefault((desc['kind'], lid is None, ref if lid is None else lid), []).append(ref)
        return out

    @staticmethod
    def page(request: dict, entries: list) -> tuple[list, bool]:
        """The entries from `offset`, at most `limit`, and whether a declared bound stopped the operation (STS-ENV-3)."""
        offset, limit = request.get('offset', 0), request.get('limit')
        rest = entries[offset:]
        return (rest[:limit], len(rest) > limit) if limit is not None else (rest, False)

    def op_snapshot_changes(self, request: dict) -> dict:
        strict(request, {'before': (True, is_digest), 'after': (True, is_digest),
                         'kinds': (False, lambda v: isinstance(v, list) and all(is_str(k) for k in v)),
                         'offset': (False, is_count), 'limit': (False, lambda v: is_count(v, 1))},
               sets=('kinds',), many=('kinds',))  # LIF-CHG-1
        self.known_kinds(request)
        refused_items = []
        for ref in (request['before'], request['after']):  # LIF-CHG-2
            kind = self.kind(ref)
            if kind is None:
                refused_items.append(item('not_found', [reason('unknown_ref', [ref])], ref=ref))
            elif kind != 'snapshot':
                refused_items.append(item('unsupported', [reason('unsupported_target_kind', [ref])], ref=ref))
        if refused_items:
            return self.result('snapshot_changes', refused_items)
        before, after = self.item_keys(self.content(request['before'])), self.item_keys(self.content(request['after']))
        kinds = request.get('kinds')
        entries = []
        for key in sorted(set(before) | set(after)):  # LIF-CHG-3
            old, new = before.get(key, []), after.get(key, [])
            if old == new or (kinds is not None and key[0] not in kinds):
                continue
            change = 'added' if not old else 'removed' if not new else 'replaced'
            entries.append(item(kind=key[0], logical_id=None if key[1] else key[2], change=change, before=old, after=new))
        items, bounded = self.page(request, entries)  # LIF-CHG-4
        return self.result('snapshot_changes', items, None, bounded)

    def op_item_history(self, request: dict) -> dict:
        strict(request, {'collection': (True, is_str), 'logical_id': (True, lambda v: is_str(v) and v != '')})  # LIF-HST-1
        lid = request['logical_id']

        def revisions(snapshot):  # LIF-HST-3
            return [{'ref': r, 'kind': self.kind(r)} for r in sorted(self.content(snapshot))
                    if self.descriptor(r).get('logical_id') == lid]
        items = []
        rows = self.db.execute('SELECT candidate, previous_head, head FROM publications WHERE collection = ? ORDER BY rowid',
                               (request['collection'],)).fetchall()
        for candidate, previous, head in rows:  # LIF-HST-2
            absent = [s for s in dict.fromkeys((head, previous)) if s is not None and self.descriptor(s) is None]
            if absent:  # LIF-HST-6: gc_collect removed a snapshot of the record
                items.append(item('not_found', [reason('unknown_ref', [s]) for s in absent],
                                  candidate=candidate, previous_head=previous, head=head))
                continue
            old, new = [r['ref'] for r in revisions(previous)], revisions(head)
            refs = [r['ref'] for r in new]
            change = ('absent' if not old and not refs else 'added' if not old else 'removed' if not refs
                      else 'unchanged' if old == refs else 'replaced')  # LIF-HST-4
            items.append(item(candidate=candidate, previous_head=previous, head=head, revisions=new, change=change))
        return self.result('item_history', items)

    def op_list_items(self, request: dict) -> dict:
        strict(request, {'context': (True, is_str), 'kinds': (False, lambda v: isinstance(v, list) and all(is_str(k) for k in v)),
                         'logical_ids': (False, lambda v: isinstance(v, list) and all(is_str(k) and k != '' for k in v)),
                         'offset': (False, is_count), 'limit': (False, lambda v: is_count(v, 1)), 'filter': (False, is_filter)},
               sets=('kinds', 'logical_ids'), many=('kinds', 'logical_ids'))  # DSC-LST-1
        context, members = self.members(request)  # context_expired before unknown_kind
        self.known_kinds(request)
        kinds, lids = request.get('kinds'), request.get('logical_ids')
        entries = []
        for key, refs in sorted(self.item_keys(members).items()):  # DSC-LST-2, DSC-LST-3
            lid = None if key[1] else key[2]
            if (kinds is None or key[0] in kinds) and (lids is None or lid in lids):
                entries.extend(item(ref=r, kind=key[0], logical_id=lid) for r in refs)
        if 'filter' in request:  # DSC-FLT-5: a match keeps its item, a failed check gives its outcome, and no match gives no item
            checked = self.filtered(request['filter'], [e['ref'] for e in entries], members)
            entries = [e if o is True else o for e, (_, o) in zip(entries, checked) if o is not False]
        items, bounded = self.page(request, entries)
        return self.result('list_items', items, context, bounded)

    # Rendering (document 14)

    def render_root(self, root: str, members: set[str]) -> list[dict]:
        """The items of one root (RND-DOC-2, RND-SCP-1 to RND-SCP-4)."""
        code = self.locate(root, members)
        if code:
            return [item(REASON_STATUS[code], [reason(code, [root])], entry='subject', ref=root)]
        out = [item(entry='subject', ref=root, kind=self.kind(root))]
        checks = [(r, c) for r, c in self.records(members, {'check'}) if c['subject'] == root]
        out += [item(entry='check', subject=root, ref=r, check_profile=c['check_profile'], outcome=c['outcome'],
                     scope=c['scope'], origin=c['origin']) for r, c in checks]
        observations = self.coverage_of(root, members)
        gaps = [] if checks else [('no_check', None)]
        if not observations:
            gaps.append(('no_coverage', None))
        for r, o in observations:
            values = o['values']
            if not self.coverage_values(values):
                gaps.append(('values_form', r))
                continue
            out.append(item(entry='coverage', subject=root, ref=r, **values))
            if values['total'] is None:
                gaps.append(('unknown_total', r))
            if values['omissions']:
                gaps.append(('omissions', r))
        withdrawn = {w['target'] for _, w in self.records(members, {'retraction', 'supersession'})}
        gaps += [('withdrawn', r) for r, _ in checks if r in withdrawn]
        return out + [item(entry='gap', subject=root, gap=g, ref=r) for g, r in gaps]

    # The producer section (document 19). The store keeps the bytes and never parses them (PRD-SEC-4).
    # Calibration data uses these objects and logs with no other check (PRD-CAL-1, PRD-CAL-2).

    # Removal (document 06 topic RMV, document 17 topic GC, document 11 topic RMV)

    def op_remove(self, request: dict) -> dict:
        strict(request, {'ref': (True, is_digest), 'reason': (True, is_name(1024))})  # REL-RMV-2
        ref, kind = request['ref'], self.kind(request['ref'])
        if kind is None:  # REL-RMV-3 steps 1 and 2: marked() gives `removed` for a removed object
            raise Failure('not_found', [reason('unknown_ref', [ref])])
        if kind not in EVIDENCE_KINDS:
            raise Failure('unsupported', [reason('unsupported_target_kind', [ref])])
        own = {p['digest'] for p in d.payloads(self.descriptor(ref))}

        def change():  # REL-RMV-4: one transaction. The edges of the object stay (REL-RMV-8).
            self.db.execute('DELETE FROM objects WHERE ref = ?', (ref,))
            self.drop_blobs(own)
            self.db.execute('DELETE FROM deferred WHERE ref = ?', (ref,))
            sequence = self.db.execute('SELECT count(*) FROM removals').fetchone()[0] + 1
            record = {'sequence': sequence, 'ref': ref, 'kind': kind, 'time': self.clock().strftime('%Y-%m-%dT%H:%M:%SZ'),
                      'reason': request['reason']}  # REL-RMV-5: no other field, no content
            self.db.execute('INSERT INTO removals VALUES (?, ?, ?, ?, ?)', tuple(record.values()))
            self.end_contexts('id IN (SELECT context FROM context_members WHERE ref = ?)', (ref,))  # CTX-LIF-4
            self.db.execute('UPDATE removal_epoch SET epoch = epoch + 1')
            return record
        try:
            record = self.transaction(change, strict=True)
        finally:
            self.cache.pop(ref, None)
            self.gone = None
        return self.envelope('remove', 'ok', [], [item(**record)])

    def op_removals(self, request: dict) -> dict:
        strict(request, {})  # REL-RMV-6
        rows = self.db.execute('SELECT sequence, ref, kind, time, reason FROM removals ORDER BY sequence')
        return self.envelope('removals', 'ok', [], [item(sequence=s, ref=r, kind=k, time=t, reason=why) for s, r, k, t, why in rows])

    def drop_blobs(self, digests=None) -> list[str]:
        """Remove the bytes of each digest of the list that no object of the store names (REL-RMV-4, LIF-GC-5). With no
        list, the digests are all the bytes of the store. A device store keeps the bytes that an object of its local
        output store names (BUN-RMV-2). The caller has the transaction."""
        named = set()
        for store in (self, self.output) if self.output else (self,):
            for (text,) in store.db.execute('SELECT descriptor FROM objects'):
                if digests is None or any(k in text for k in digests):  # the text of a descriptor has each digest that it names
                    named |= {p['digest'] for p in d.payloads(json.loads(text))}
        have = {k for (k,) in self.db.execute('SELECT digest FROM blobs')}
        dropped = sorted((have if digests is None else have & set(digests)) - named)
        self.db.executemany('DELETE FROM blobs WHERE digest = ?', [(k,) for k in dropped])
        return dropped

    def open_members(self) -> set[str]:
        """Each member of each open read context of this store (CTX-LIF-6)."""
        return {r for (r,) in self.db.execute(
            'SELECT ref FROM context_members WHERE context IN (SELECT id FROM contexts) '
            'AND context NOT IN (SELECT context FROM context_ends WHERE expires <= ?)', (self.now(),))}

    def retained_roots(self, request: dict) -> set[str]:
        """The retained roots of gc_collect: LIF-GC-2 for a writer store, BUN-RMV-1 for a device store."""
        roots = set(request.get('roots', [])) | self.open_members()
        if self.role == 'device':
            if 'roots' not in request:  # each imported root that the store has
                roots |= {r for (text,) in self.db.execute('SELECT roots FROM imports') for r in json.loads(text)
                          if self.descriptor(r)}
            return roots
        roots |= {s for (s,) in self.db.execute('SELECT snapshot FROM heads')}
        roots |= {s for (s,) in self.db.execute(
            'SELECT snapshot FROM candidates WHERE id NOT IN (SELECT candidate FROM publications)')}
        newest = self.db.execute('SELECT receipt, activation FROM publications WHERE rowid IN '
                                 '(SELECT max(rowid) FROM publications GROUP BY collection)')
        return roots | {r for row in newest for r in row if r is not None}

    def op_gc_collect(self, request: dict) -> dict:
        strict(request, {'roots': (False, lambda v: is_refs(v, empty=True))}, sets=('roots',))  # LIF-GC-1
        own = lambda r: self.db.execute('SELECT 1 FROM objects WHERE ref = ?', (r,)).fetchone()
        absent = [r for r in request.get('roots', []) if not own(r)]  # LIF-GC-4, BUN-RMV-2
        if absent:
            raise Failure('not_found', [reason('unknown_ref', [r]) for r in absent])
        with self.output.mutex if self.output else contextlib.nullcontext():
            if self.output:
                self.output.fresh()
            retained = self.closure(self.retained_roots(request), True)
            if self.output:  # BUN-RMV-1: each object that the local output store keeps, and its open contexts
                kept = {r for (r,) in self.output.db.execute('SELECT ref FROM objects')}
                retained |= self.output.closure(kept | self.output.open_members(), True)
            gone = [(r, k) for r, k in self.db.execute('SELECT ref, kind FROM objects ORDER BY ref') if r not in retained]

            def change():  # LIF-GC-5: one transaction
                refs = [(r,) for r, _ in gone]
                self.db.executemany('DELETE FROM objects WHERE ref = ?', refs)
                self.db.executemany('DELETE FROM deferred WHERE ref = ?', refs)
                for (source,) in self.db.execute('SELECT DISTINCT source FROM edges').fetchall():
                    if source not in retained:  # also the kept refs of a removed object that no retained root reaches
                        self.db.execute('DELETE FROM edges WHERE source = ?', (source,))
                payloads = self.drop_blobs()
                snapshots = {r for r, k in gone if k == 'snapshot'}
                candidates = sorted(c for c, s in self.db.execute('SELECT id, snapshot FROM candidates') if s in snapshots)
                self.db.executemany('DELETE FROM candidates WHERE id = ?', [(c,) for c in candidates])
                self.end_contexts('id IN (SELECT context FROM context_ends WHERE expires <= ?)', (self.now(),))  # an ended context keeps no row
                if gone or payloads or candidates:
                    self.db.execute('UPDATE removal_epoch SET epoch = epoch + 1')
                return payloads, candidates
            try:
                payloads, candidates = self.transaction(change, self.output, strict=True)
            finally:
                for r, _ in gone:
                    self.cache.pop(r, None)
                self.gone = None
        return self.envelope('gc_collect', 'ok', [], [item(ref=r, kind=k) for r, k in gone], payloads=payloads, candidates=candidates)

    def producer_write(self, change) -> dict:
        """Run `change()` in one write transaction, so a second open of the store sees all of it or none (STS-MAL-2)."""
        self.begin()
        try:
            result = change()
            self.db.execute('COMMIT')
            return result
        except BaseException:
            self.rollback()
            raise

    def producer_object(self, producer: str, key: str) -> bytes | None:
        row = self.db.execute('SELECT data FROM producer_objects WHERE producer = ? AND key = ?', (producer, key)).fetchone()
        return None if row is None else bytes(row[0])

    def producer_absent(self, producer: str, key: str) -> Failure:
        """The not_found outcome of a key with no object (PRD-OBJ-3, PRD-RMV-2)."""
        removed = self.db.execute('SELECT 1 FROM producer_removals WHERE producer = ? AND key = ?', (producer, key)).fetchone()
        return fail('producer_removed' if removed else 'unknown_producer_key')

    def op_producer_put(self, request: dict) -> dict:
        strict(request, {**PRODUCER_KEY, 'data': (True, is_b64)})
        producer, key, data = request['producer'], request['key'], unb64(request['data'])

        def change():
            kept = self.producer_object(producer, key)
            if kept is None:  # PRD-OBJ-1, PRD-OBJ-4
                self.db.execute('INSERT INTO producer_objects VALUES (?, ?, ?)', (producer, key, data))
            elif kept != data:
                raise fail('producer_key_conflict')  # PRD-OBJ-2
        self.producer_write(change)
        return self.envelope('producer_put', 'ok', [], [item(key=key, size=len(data))])

    def op_producer_get(self, request: dict) -> dict:
        strict(request, PRODUCER_KEY)
        producer, key = request['producer'], request['key']
        data = self.producer_object(producer, key)
        if data is None:
            raise self.producer_absent(producer, key)
        return self.envelope('producer_get', 'ok', [], [item(key=key, data=b64(data))])  # PRD-OBJ-3

    def op_producer_forget(self, request: dict) -> dict:
        strict(request, {**PRODUCER_KEY, 'reason': (True, is_name(1024))})
        producer, key, why = request['producer'], request['key'], request['reason']

        def change():  # the removal and its record in one transaction (PRD-RMV-1)
            if self.producer_object(producer, key) is None:
                raise self.producer_absent(producer, key)  # PRD-RMV-2
            (count,) = self.db.execute('SELECT count(*) FROM producer_removals WHERE producer = ?', (producer,)).fetchone()
            time = self.clock().astimezone(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
            self.db.execute('DELETE FROM producer_objects WHERE producer = ? AND key = ?', (producer, key))
            self.db.execute('INSERT INTO producer_removals VALUES (?, ?, ?, ?, ?)', (producer, count + 1, key, time, why))
            return {'sequence': count + 1, 'key': key, 'time': time, 'reason': why}
        return self.envelope('producer_forget', 'ok', [], [item(**self.producer_write(change))])

    def op_producer_removals(self, request: dict) -> dict:
        strict(request, PRODUCER)
        rows = self.db.execute('SELECT sequence, key, time, reason FROM producer_removals WHERE producer = ? ORDER BY sequence',
                               (request['producer'],))
        return self.envelope('producer_removals', 'ok', [],
                             [item(sequence=s, key=k, time=t, reason=r) for s, k, t, r in rows])  # PRD-RMV-4

    @staticmethod
    def log_name(log: list[str]) -> str:
        return json.dumps(log, ensure_ascii=False, separators=(',', ':'))  # one text for one name (PRD-LOG-1)

    def op_producer_append(self, request: dict) -> dict:
        strict(request, {**PRODUCER_LOG, 'sequence': (True, lambda v: is_count(v, 1)), 'entry': (True, is_b64)})
        producer, log, sequence = request['producer'], self.log_name(request['log']), request['sequence']
        entry = unb64(request['entry'])

        def change():  # the check and the write in one transaction, so two writers cannot both add one number (PRD-LOG-2)
            (count,) = self.db.execute('SELECT count(*) FROM producer_entries WHERE producer = ? AND log = ?',
                                       (producer, log)).fetchone()
            if sequence != count + 1:
                raise fail('log_moved')
            self.db.execute('INSERT INTO producer_entries VALUES (?, ?, ?, ?)', (producer, log, sequence, entry))
        self.producer_write(change)
        return self.envelope('producer_append', 'ok', [], [item(sequence=sequence)])

    def op_producer_read(self, request: dict) -> dict:
        strict(request, PRODUCER_LOG)
        rows = self.db.execute('SELECT sequence, entry FROM producer_entries WHERE producer = ? AND log = ? ORDER BY sequence',
                               (request['producer'], self.log_name(request['log'])))
        return self.envelope('producer_read', 'ok', [], [item(sequence=s, entry=b64(bytes(e))) for s, e in rows])  # PRD-LOG-3

    def op_render(self, request: dict) -> dict:
        strict(request, {'context': (True, is_str), 'roots': (True, lambda v: is_refs(v, empty=True)),
                         'render_profile': (False, is_str)}, sets=('roots',), many=('roots',))  # RND-DOC-1
        profile = request.get('render_profile', RENDER_PROFILE)
        if profile != RENDER_PROFILE:
            raise fail('unsupported_profile')
        context, members = self.members(request)
        roots = request['roots']
        items = [i for root in roots for i in self.render_root(root, members)]
        status, reasons = summary(items, 'render')
        document = None
        if status in ('ok', 'partial'):  # RND-DOC-3
            entries = [{k: v for k, v in i.items() if k not in ('status', 'reasons')} for i in items if i['status'] == 'ok']
            document = {'format': 'aavanam-render/v1', 'render_profile': profile, 'context': context, 'roots': sorted(roots),
                        'entries': sorted(entries, key=canonical),
                        'omitted': sorted(i['ref'] for i in items if i['status'] != 'ok')}
        return self.envelope('render', status, reasons, items, context, document=document,
                             document_id=revision(document) if document else None)
