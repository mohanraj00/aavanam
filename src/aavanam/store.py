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
import json
import os
import sqlite3
import threading
import uuid
from collections import deque
from datetime import datetime, timezone

from . import descriptors as d
from .jsontext import FormError, canonical, detached, digest, is_digest, is_ijson, number_value, parse, revision, valid_string
from .kinds import BASE_PROFILE
from .statuses import ORDER, REASON_STATUS, RESERVED

BUNDLE_FORMAT = 'aavanam-bundle/v1'
COVERAGE = 'aavanam/coverage/v1'
RENDER_PROFILE = 'aavanam/render/v1'
# The operations whose row in the operation table has `partial` (STS-ENV-3)
PARTIAL = {'query_data', 'search', 'context_bundle', 'evidence', 'coverage', 'observations', 'render', 'compare', 'impact'}
LOCK_KINDS = {'execution_inputs': 'execution-lock', 'build_inputs': 'build-lock'}
# The capability table (CTX-PRJ-1): each capability and the operations that need it
CAPABILITIES = {'aavanam/data/v1': ('query_data',), 'aavanam/lexical/v1': ('search',),
                'aavanam/locks/v1': ('execution_inputs', 'build_inputs'), 'aavanam/export/v1': ('export',)}
NEEDS = {op: cap for cap, ops in CAPABILITIES.items() for op in ops}
SKEW_POLICIES = ('reject', 'permit')  # CTX-SKW-3
ROLES = ('writer', 'device', 'output')  # BUN-IMP-5
# The fields of the publication record (PUB-LOG-2), after `candidate` and `receipt`. The rowid order is the head update order.
LOG_COLUMNS = ('activation', 'collection', 'previous_head', 'head')
# The member fields of a snapshot (PUB-ACT-2). Check 5 of PUB-ACT-3 does not apply to the last field.
MEMBER_FIELDS = ('units', 'schemas', 'relation_sets', 'overlay_sets', 'observations', 'native_mappings')
# The operations that write to a store, refused on a device store (BUN-IMP-6)
WRITES = ('put', 'create_snapshot', 'create_candidate', 'publish')

SCHEMA = """
CREATE TABLE IF NOT EXISTS objects (ref TEXT PRIMARY KEY, kind TEXT NOT NULL, descriptor TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS edges (source TEXT NOT NULL, target TEXT NOT NULL, PRIMARY KEY (source, target));
CREATE TABLE IF NOT EXISTS blobs (digest TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS contexts (id TEXT PRIMARY KEY, roots TEXT NOT NULL, capabilities TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS context_members (context TEXT NOT NULL, ref TEXT NOT NULL, PRIMARY KEY (context, ref));
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
"""


class Failure(Exception):
    """An outcome of the operation as a whole (STS-ENV-3 rule 1)."""

    def __init__(self, status: str, reasons: list[dict]):
        super().__init__(status)
        self.status = status
        self.reasons = reasons


SECTIONS = ('serving', 'evidence')  # PUB-SEC-1
NOT_IN_SERVING = ('source', 'reading', 'excerpt', 'evidence')  # PUB-SEC-3


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


def b64(data: bytes) -> str:
    return base64.b64encode(data).decode('ascii')


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
                 device: Store | None = None):
        if role not in ROLES or (role == 'output') != (device is not None):
            raise ValueError('a local output store has one device store, and only a local output store has one')
        if role == 'output' and (device.role != 'device' or device.output is not None):
            raise ValueError('only a device store creates its local output store (BUN-IMP-5)')
        if role == 'device' and path != ':memory:' and os.path.realpath(path) == os.path.realpath(output_path):
            raise ValueError('a device store and its local output store have separate databases (BUN-IMP-7)')
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
        self.mutex = threading.RLock()  # one connection, one call at a time
        self.called = False  # a gate is declared before the first call (PUB-GAT-1)
        self.cache: dict[str, dict] = {}  # descriptors do not change (IDN-CAN-1)
        self.policies: dict[str, object] = {}  # the acceptance policies of the application (PUB-RCP-1)
        self.output = Store(output_path, debug, 'output', device=self) if role == 'device' else None
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

        `activation` is the activation reference of the request, or None (PUB-RCP-2).
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
                    for (text,) in self.db.execute("SELECT descriptor FROM objects WHERE kind = 'snapshot'"):
                        if json.loads(text, parse_float=number_value)['collection'] == collection:
                            raise ValueError('the collection already has a snapshot (PUB-SEC-2)')
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

    def blob(self, key: str) -> bytes | None:
        row = self.db.execute('SELECT data FROM blobs WHERE digest = ?', (key,)).fetchone()
        if row is None:
            return self.device.blob(key) if self.device else None  # BUN-IMP-8
        return bytes(row[0])

    def targets(self, ref: str) -> set[str]:
        """The `refs` of an object (KND-DEP-2), from the edges table of this store and of its device store."""
        out = {t for (t,) in self.db.execute('SELECT target FROM edges WHERE source = ?', (ref,))}
        return out | self.device.targets(ref) if self.device else out

    def sources(self, ref: str) -> set[str]:
        """The objects whose `refs` have the object, in this store and in its device store (BUN-IMP-8)."""
        out = {s for (s,) in self.db.execute('SELECT source FROM edges WHERE target = ?', (ref,))}
        return out | self.device.sources(ref) if self.device else out

    def closure(self, roots) -> set[str]:
        """The roots and each object that their references reach (KND-DEP-3). The edges table is `refs` (KND-DEP-2)."""
        seen, todo = set(roots), deque(roots)
        while todo:
            for target in self.targets(todo.popleft()):
                if target not in seen:
                    seen.add(target)
                    todo.append(target)
        return seen

    def fingerprint(self) -> str:
        h = hashlib.sha256()
        for table, key in (('objects', 'ref'), ('blobs', 'digest'), ('candidates', 'id'),
                           ('heads', "collection || ' ' || snapshot"), ('publications', "candidate || ' ' || receipt || ' ' || coalesce(activation, '')"),
                           ('producer_objects', "producer || ' ' || key || ' ' || hex(data)"),
                           ('producer_entries', "producer || ' ' || log || ' ' || sequence || ' ' || hex(entry)"),
                           ('producer_removals', "producer || ' ' || sequence || ' ' || key || ' ' || time || ' ' || reason")):
            for (value,) in self.db.execute(f'SELECT {key} FROM {table} ORDER BY {key}'):
                h.update(f'{table}:{value}\n'.encode())
        return h.hexdigest()

    def write(self, objects: dict[str, dict], blobs: dict[str, bytes]) -> None:
        """Write the objects and bytes in one transaction: all or none (STS-MAL-2)."""
        self.db.execute('BEGIN IMMEDIATE')
        try:
            # A second connection can import after the check of call(), so check again in the transaction (BUN-IMP-6)
            if self.role == 'device' and self.db.execute('SELECT 1 FROM objects LIMIT 1').fetchone():
                raise fail('read_only_store')
            if self.role == 'output':  # the objects and bytes of the device store are already here (BUN-IMP-8)
                objects = {r: o for r, o in objects.items() if self.device.descriptor(r) is None}
                blobs = {k: b for k, b in blobs.items() if self.device.blob(k) is None}
            # A second connection can declare a section after the check of call(), so check again here (PUB-SEC-2)
            found = {ref: self.section_violations(desc, lambda r: objects.get(r) or self.descriptor(r)) for ref, desc in objects.items()}
            if any(found.values()):
                raise SectionViolation(found)
            for ref, desc in objects.items():
                self.db.execute('INSERT OR IGNORE INTO objects VALUES (?, ?, ?)', (ref, desc['kind'], canonical(desc)))
                self.db.executemany('INSERT OR IGNORE INTO edges VALUES (?, ?)', {(ref, t) for _, t in d.references(desc)})
            self.db.executemany('INSERT OR IGNORE INTO blobs VALUES (?, ?)', blobs.items())
            self.db.execute('COMMIT')
        except BaseException:
            self.rollback()
            raise

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
        if operation in RESERVED:  # STS-MAL-4, RSV-RSP-1
            return self.envelope(operation, 'unsupported', [reason('reserved_profile', (), [RESERVED[operation]])])
        method = getattr(self, 'op_' + operation, None)
        if method is None:
            return self.envelope(operation, 'unsupported', [reason('unsupported_operation')])
        self.called = True
        context = request.get('context') if isinstance(request, dict) and isinstance(request.get('context'), str) else None
        # A local output store reads its device store, so it waits for a device call, for example an import (CTX-OPN-2).
        # The order is always the device store, then the output store.
        with self.device.mutex if self.device else contextlib.nullcontext(), self.mutex:
            try:
                if self.broken:
                    raise sqlite3.DatabaseError('the connection is not usable')
                if self.role == 'device' and (operation in WRITES or operation == 'import' and self.has_objects()):
                    raise fail('read_only_store')  # BUN-IMP-6: after STS-MAL-4, before the request
                if operation.startswith('producer_') and self.role != 'writer':
                    raise fail('producer_section_absent')  # PRD-SEC-1: after STS-MAL-4, before the request
                if not isinstance(request, dict):
                    raise fail('field_type')
                return detached(method(request))  # a caller cannot change the cached descriptors (CTX-OPN-2)
            except Failure as f:
                return self.envelope(operation, f.status, f.reasons, context=context, **self.empty(operation))
            except Exception:  # noqa: BLE001  STS-MAL-1
                if self.debug:
                    raise
                self.rollback()
                return self.envelope(operation, 'failed', [reason('storage_error')], context=context, **self.empty(operation))

    def has_objects(self) -> bool:
        return self.db.execute('SELECT 1 FROM objects LIMIT 1').fetchone() is not None

    @staticmethod
    def empty(operation: str) -> dict:
        """The added fields of a result that did not complete (STS-ENV-3 rule 1, RND-DOC-3)."""
        return {'items': [], 'document': None, 'document_id': None} if operation == 'render' else {'items': []}

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
        if operation in NEEDS and NEEDS[operation] not in capabilities:
            raise Failure('unsupported', [reason('capability_unavailable', (), [NEEDS[operation]])])
        return context, members

    def context_of(self, request: dict) -> tuple[str, set[str], list[str]]:
        """The identity, the content and the capabilities of the read context of the request (CTX-OPN-3, CTX-PRJ-2)."""
        context = request.get('context')
        if not isinstance(context, str):
            raise fail('field_type')
        row = self.db.execute('SELECT capabilities FROM contexts WHERE id = ?', (context,)).fetchone()
        if row is None:
            raise fail('context_expired')
        rows = self.db.execute('SELECT ref FROM context_members WHERE context = ?', (context,))
        return context, {r for (r,) in rows}, json.loads(row[0])

    def locate(self, ref: str, members: set[str]) -> str | None:
        """The reason code of a reference that is not in the read context (QRY-GET-2, CTX-OPN-4)."""
        if ref in members:
            return None
        return 'not_in_context' if self.descriptor(ref) else 'unknown_ref'

    @staticmethod
    def open_form(request: dict) -> None:
        """The request form of open_context: CTX-OPN-5, in order, with one reason for each affected value."""
        unknown = [f for f in request if f not in ('roots', 'heads', 'skew_policy', 'capabilities')]
        if unknown:
            raise fail(*['unknown_field'] * len(unknown))
        types = [f for f in ('roots', 'heads', 'capabilities') if f in request and not isinstance(request[f], list)]
        if 'skew_policy' in request and request['skew_policy'] not in SKEW_POLICIES:
            types.append('skew_policy')
        tests = {'roots': is_digest, 'heads': is_str, 'capabilities': is_str}
        types += [v for f, test in tests.items() if isinstance(request.get(f), list) for v in request[f] if not test(v)]
        if types:
            raise fail(*['field_type'] * len(types))
        counts = [] if request.get('roots') or request.get('heads') else ['roots']
        repeats = sum(len(v) - len(set(v)) for v in (request.get('heads', []), request.get('capabilities', [])))
        if counts or repeats:
            raise fail(*['field_count'] * (len(counts) + repeats))

    def op_open_context(self, request: dict) -> dict:
        self.open_form(request)
        capabilities = request.get('capabilities', list(CAPABILITIES))  # CTX-PRJ-2
        unknown = [c for c in capabilities if c not in CAPABILITIES]
        if unknown:  # CTX-PRJ-3
            raise Failure('unsupported', [reason('unsupported_profile', (), [c]) for c in unknown])
        roots, heads = request.get('roots', []), request.get('heads', [])
        found = self.heads(heads)  # CTX-VIW-3
        items = []
        for root in roots:  # CTX-OPN-1
            kind = self.kind(root)
            if kind is None:
                items.append(refused(['unknown_ref'], [root]))
            elif kind not in ('knowledge-unit', 'snapshot'):
                items.append(refused(['unsupported_target_kind'], [root]))
            else:
                items.append(item(ref=root))
        items += [item(ref=found[n]) if found[n] else refused(['no_head']) for n in heads]  # CTX-VIW-2
        resolved = [i['ref'] for i in items if i['status'] == 'ok']
        self.composed(items)
        if all(i['status'] == 'ok' for i in items) and request.get('skew_policy', 'reject') == 'reject':
            self.skewed(items)
        status, reasons = summary(items, 'open_context')
        if status != 'ok':
            return self.envelope('open_context', status, reasons, items)
        context = 'ctx:' + uuid.uuid4().hex
        closure = self.closure(resolved)
        self.db.execute('BEGIN IMMEDIATE')
        try:
            self.db.execute('INSERT INTO contexts VALUES (?, ?, ?)', (context, json.dumps(resolved), json.dumps(sorted(capabilities))))
            self.db.executemany('INSERT INTO context_members VALUES (?, ?)', ((context, r) for r in closure))
            self.db.execute('COMMIT')
        except BaseException:
            self.rollback()
            raise
        return self.envelope('open_context', 'ok', [], items, context)  # CTX-OPN-2

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

    def check_batch(self, texts: list[str], data: dict[str, bytes], incomplete: bool) -> list:
        """Check each descriptor text in the steps of STS-MAL-3. Return a normal descriptor or a refused item for each.

        A target or payload can be in the batch or in the store. If `incomplete`, a target or payload that is in
        neither gives `bundle_incomplete` (BUN-IMP-2).
        """
        out, batch = [], {}
        for text in texts:
            try:
                desc = d.check(parse(text) if isinstance(text, str) else text)
                batch[revision(desc)] = desc
                out.append(desc)
            except (FormError, d.Refusal) as e:
                out.append(refused(e.codes))

        def lookup(ref):
            return batch.get(ref) or self.descriptor(ref)

        def data_of(key):
            return data[key] if key in data else self.blob(key)

        for n, desc in enumerate(out):
            if 'status' in desc:
                continue
            for step in (self.step_references, self.step_payloads, self.step_content, self.step_section):
                try:
                    step(desc, lookup, data_of, incomplete)
                except d.Refusal as e:
                    out[n] = refused(e.codes, e.refs)
                    break
        return out

    @staticmethod
    def step_references(desc, lookup, data_of, incomplete):
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
    def step_payloads(desc, lookup, data_of, incomplete):
        """STS-MAL-3 step 5 (KND-OPQ-1)."""
        objects = d.payloads(desc)
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
    def step_content(desc, lookup, data_of, incomplete):
        """STS-MAL-3 step 6 (BUN-EXC-3, BUN-EXC-4, IDN-NAT-3)."""
        if desc['kind'] == 'excerpt':
            parent = lookup(desc['parent'])['payload']
            whole = data_of(parent['digest'])
            if whole is None:
                raise d.Refusal(['bundle_incomplete' if incomplete else 'payload_missing'])
            parts = [whole[r['start']:r['end']] for r in desc['ranges'] if r['end'] <= len(whole)]
            if len(parts) != len(desc['ranges']) or b''.join(parts) != data_of(desc['payload']['digest']):
                raise d.Refusal(['excerpt_mismatch'])
        if desc['kind'] == 'snapshot':
            subjects: dict[tuple, set] = {}
            for m in desc.get('native_mappings', []):
                mapping = lookup(m)
                subjects.setdefault((mapping['native_scheme'], mapping['native_id']), set()).add(mapping['subject'])
            conflicts = [k for k, v in subjects.items() if len(v) > 1]
            if conflicts:
                raise d.Refusal(['native_id_conflict'] * len(conflicts))

    def step_section(self, desc, lookup, data_of, incomplete):
        """STS-MAL-3 step 6: the closure of a snapshot in a `serving` collection (PUB-SEC-3)."""
        found = self.section_violations(desc, lookup)
        if found:
            raise d.Refusal(['section_violation'] * len(found), found)

    def section_violations(self, desc, lookup) -> list[str]:
        """The descriptors in the closure of a snapshot that its `serving` collection does not permit (PUB-SEC-3)."""
        if desc['kind'] != 'snapshot' or self.section_of(desc['collection']) != 'serving':
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

    def commit_batch(self, operation: str, checked: list, data: dict[str, bytes], whole: list[dict] = ()) -> dict:
        """Write the batch if each descriptor passed, or return the refusal (STS-ENV-5, STS-MAL-2)."""
        bad = [c for c in checked if 'status' in c] + list(whole)
        if bad:
            status, reasons = first_status(bad)
            return {'status': status, 'reasons': reasons,
                    'items': [c if 'status' in c else refused(['batch_aborted']) for c in checked]}
        objects = {revision(c): c for c in checked}
        blobs = {p['digest']: data[p['digest']] for c in checked for p in d.payloads(c) if p['digest'] in data}
        try:
            self.write(objects, blobs)
        except SectionViolation as e:  # STS-ENV-4: the store did not change
            return self.commit_batch(operation, [refused(['section_violation'] * len(e.found[revision(c)]), e.found[revision(c)]) if e.found.get(revision(c)) else c for c in checked], data, whole)
        except Failure:
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
        data = self.blob(desc['payload']['digest'])
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

    @staticmethod
    def required(request: dict, capabilities: list[str]) -> None:
        """LCK-CAP-2: one reason for each name of `requires` that is not in the table or not in the context."""
        reasons = [reason('unsupported_profile' if c not in CAPABILITIES else 'capability_unavailable', (), [c])
                   for c in request.get('requires', []) if c not in capabilities]
        if reasons:
            raise Failure('unsupported', reasons)

    def lock(self, operation: str, request: dict, context: str, covered: list[str]) -> dict:
        """Write the lock of the request and return the closure that it covers (LCK-EXE-4, LCK-BLD-3).
        A device store writes the lock to its local output store (BUN-IMP-7)."""
        fields = {k: v for k, v in request.items() if k != 'context'}
        desc = d.check({'descriptor_profile': BASE_PROFILE, 'kind': LOCK_KINDS[operation], **fields})
        ref = revision(desc)
        items = [item(ref=r) for r in sorted(self.closure(covered))]  # read before the write (STS-ENV-4)
        target = self.output or self
        with target.mutex:  # the local output store can have calls of its own
            target.write({ref: desc}, {})
        return self.envelope(operation, 'ok', [], items, context, lock=ref)

    def lock_context(self, request: dict, operation: str) -> tuple[str, set[str], list[str]]:
        """The read context of a lock request, with the capability of the operation (CTX-PRJ-4)."""
        context, members = self.members(request, operation)
        return context, members, self.context_of(request)[2]

    def op_execution_inputs(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'binding_set': (True, is_digest), 'context_inputs': (False, lambda v: is_refs(v, empty=True)),
                       'requires': (False, is_names), 'consumer': (True, is_text), 'completeness': (True, is_label)})
        counted(request, 'context_inputs')  # LCK-EXE-1
        counted(request, 'requires')  # LCK-CAP-1
        context, members, capabilities = self.lock_context(request, 'execution_inputs')
        inputs = request.get('context_inputs', [])
        binding_set, permitted = request['binding_set'], d.targets('execution-lock', 'context_inputs')
        self.checked_refs([(binding_set, ('binding-set',))] + [(r, permitted) for r in inputs], members)
        self.required(request, capabilities)
        self.bindings_problems(self.descriptor(binding_set))
        return self.lock('execution_inputs', request, context, [binding_set, *inputs])

    def op_build_inputs(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'inputs': (True, lambda v: is_refs(v, empty=True)),
                       'requires': (False, is_names), 'consumer': (True, is_text), 'completeness': (True, is_label)})
        counted(request, 'inputs', many=True)  # LCK-BLD-1
        counted(request, 'requires')  # LCK-CAP-1
        context, members, capabilities = self.lock_context(request, 'build_inputs')
        inputs = request['inputs']
        self.checked_refs([(r, d.targets('build-lock', 'inputs')) for r in inputs], members)
        self.required(request, capabilities)
        return self.lock('build_inputs', request, context, inputs)

    # Bundles (document 11)

    def op_export(self, request: dict) -> dict:
        form(request, {'context': (True, is_str), 'roots': (True, is_refs)})
        context, members = self.members(request, 'export')
        roots = request['roots']
        located = [(r, code) for r in roots if (code := self.locate(r, members))]
        if located:  # BUN-EXP-1
            return self.envelope('export', 'not_found', [reason(code, [r]) for r, code in located], [], context)
        objects = sorted(self.closure(roots))
        digests = sorted({p['digest'] for r in objects for p in d.payloads(self.descriptor(r))})
        bundle = {'format': BUNDLE_FORMAT, 'roots': roots, 'descriptors': [canonical(self.descriptor(r)) for r in objects],
                  'payloads': {k: b64(self.blob(k)) for k in digests}}  # BUN-EXP-2
        return self.envelope('export', 'ok', [], [item(ref=r) for r in objects], context, bundle=bundle)

    def op_import(self, request: dict) -> dict:
        bundle = request.get('bundle')
        if not (set(request) == {'bundle'} and isinstance(bundle, dict)
                and set(bundle) == {'format', 'roots', 'descriptors', 'payloads'} and bundle['format'] == BUNDLE_FORMAT
                and is_refs(bundle['roots']) and isinstance(bundle['descriptors'], list)
                and all(isinstance(t, str) for t in bundle['descriptors']) and isinstance(bundle['payloads'], dict)
                and all(is_digest(k) for k in bundle['payloads'])):
            raise fail('field_type')  # BUN-IMP-1
        data = {k: unb64(v) for k, v in bundle['payloads'].items()}
        checked = self.check_batch(bundle['descriptors'], data, incomplete=True)
        present = self.revisions(bundle['descriptors'])  # a root with a failed check is present (BUN-IMP-2)
        absent = [refused(['bundle_incomplete'], [r]) for r in bundle['roots'] if r not in present]
        out = self.commit_batch('import', checked, data, absent)
        if out['status'] != 'ok':  # BUN-IMP-4
            return self.envelope('import', out['status'], out['reasons'], [])
        return self.envelope('import', 'ok', [], out['items'])

    # Lookup and discovery (document 09)

    def table(self, desc: dict) -> dict | None:
        """The table of a data resource under aavanam/table/v1, or None if the payload does not agree with DAT-TBL-1."""
        try:
            value = parse(self.blob(desc['payload']['digest']).decode('utf-8'))
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
        if self.blob(desc['payload']['digest']) is None:
            return refused(['payload_missing'], [ref])
        table = self.table(desc)
        if table is None:
            return refused(['payload_form'], [ref])
        where = request.get('where', {})
        unknown = [c for c in where if c not in table['columns']]
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
            data = self.blob(desc['payload']['digest'])
            count = data.count(query) if data is not None else 0
            if count:
                matches.append((-count, ref, desc['kind']))
        matches.sort()
        offset, limit = request.get('offset', 0), request.get('limit')
        rest = matches[offset:]
        bounded = limit is not None and len(rest) > limit
        items = [item(ref=r, kind=k, count=-c) for c, r, k in (rest[:limit] if limit is not None else rest)]  # QRY-LEX-3
        return self.result('search', items, context, bounded)

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
        self.db.execute('BEGIN IMMEDIATE')
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
        finally:
            for mutex in held:  # the order of call()
                mutex.acquire()
        if not accepted:  # step 4
            return self.result('publish', [item('incompatible', [reason('receipt_rejected', [receipt])], candidate=identity)])
        self.db.execute('BEGIN IMMEDIATE')  # step 5, one atomic step
        try:
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

    # The producer section (document 17). The store keeps the bytes and never parses them (PRD-SEC-4).

    def producer_write(self, change) -> dict:
        """Run `change()` in one write transaction, so a second open of the store sees all of it or none (STS-MAL-2)."""
        self.db.execute('BEGIN IMMEDIATE')
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
