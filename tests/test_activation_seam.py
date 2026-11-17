"""The seam test of the activation gate (PUB-GAT-1 to PUB-GAT-3, PUB-ACT-1 to PUB-ACT-5, PUB-HED-3 to PUB-HED-5, issue #4).

A workflow producer activates an update and publishes it to a gated collection. This test checks the store side:

- the head moves only with a valid activation;
- a refused activation leaves the head unchanged and gives the reason;
- a repeat with the same request key gives the earlier outcome.

The activation records are a STAND-IN. They come from `tests/activation_record_stand_in.json`, not from a release
of the producer. No release writes the proposed activation record yet. The note in that file
says how to replace it with a pinned release. `descriptor_of` maps the record to the store descriptor.
The rule checks also read the record layout.

The record table at commit 4d9d66e maps to PUB-ACT-1 as follows. A derived value is a test value.

| Record field | Descriptor field |
|---|---|
| format | not used |
| scope | not used |
| function | not used |
| event_ref | not used |
| request_key | request_key |
| candidate | candidate, from candidate.ref after binding; candidate.kind is not used |
| base | base after binding; an empty string gives no base |
| collection | not used |
| partition | partition |
| from | effective_from |
| until | effective_until |
| source_ref | not used |
| snapshot_ref | not used |
| report_ref | report, a derived store receipt with this reference as its payload |
| decision_ref | approval, a derived store receipt with this reference as its payload |
| producer | producer |
| runner | runner |
| approver | approver |
| activator | not used |
| targets | not used |
| supersedes | not used |
| applicability | not used |

The test derives descriptor_profile and kind from the store profile. It derives lock from the changed members
of the candidate snapshot. No removed member needs a reference in these updates, so removed is absent.
The two collection and partition names agree by fixture design. This is no general mapping of the two head models.
The candidate binding does not prove that the native candidate identities are equal. The base binding uses a
store snapshot, although the record names a base candidate. A receipt payload is a reference, not receipt content.
These stand-ins prove no signed record check or production integration.

What the record does not prove
-----------------------------

- lock: the test derives a build lock from the changed store members. PUB-ACT-3 checks 3 to 5, including check 4,
  run on this derived lock. The seam test does not prove that the record declares the store lock.
- base: the test derives a store snapshot binding from a base candidate reference. PUB-ACT-3 check 2 runs on this
  derived base. The seam test does not prove that the record declares the store snapshot.
- approval: the test derives a store receipt with decision_ref as its payload text. The receipt checks at put
  (PUB-ACT-1, KND-REF-2, KND-REF-3) run on this derived receipt. The seam test does not prove that the record declares it.
- report: the test derives a store receipt with report_ref as its payload text. The receipt checks at put
  (PUB-ACT-1, KND-REF-2, KND-REF-3) run on this derived receipt. The seam test does not prove that the record declares it.

The store-side gate behavior is still proved for each check. The producer record does not yet give these four
descriptor values. These are gaps of this PR. The guard test below requires a mapping change if the record adds them.

Each expected outcome comes from the clauses of document 13, not from a run of the store.
"""
from __future__ import annotations

import copy
import hashlib
import json
import re
import unittest
from datetime import date

from support import BASE, ROOT, Store, candidate, payload, put_ref

STAND_IN = ROOT / 'tests/activation_record_stand_in.json'
COLLECTION = 'sample-notices'
POLICY = 'sample-policy'
# The record table and schema at commit 4d9d66e. No schema package is needed.
FIELDS = {'format', 'scope', 'function', 'event_ref', 'request_key', 'candidate', 'base', 'collection', 'partition',
          'from', 'until', 'source_ref', 'snapshot_ref', 'report_ref', 'decision_ref', 'producer', 'runner', 'approver',
          'activator', 'targets', 'supersedes', 'applicability'}
REQUIRED = FIELDS - {'collection', 'partition'}
REF = r'sha256:[0-9a-f]{64}'


def json_object(text: str) -> dict:
    """Rule 2: reject a repeated member name in any object, and reject non-JSON numbers."""
    def pairs(items):
        out = {}
        for key, value in items:
            if key in out:
                raise ValueError('json')
            out[key] = value
        return out

    def non_json(value):
        raise ValueError('json')

    try:
        out = json.loads(text, object_pairs_hook=pairs, parse_constant=non_json)
    except (ValueError, RecursionError) as error:
        raise ValueError('json') from error
    if not isinstance(out, dict):
        raise ValueError('json')
    return out


def check_record(record: dict, record_kind: str | None) -> None:
    """Apply consumer rules 1 to 8 at commit 4d9d66e. Rule 1 requires the record kind 'activation'.

    A missing kind (None) or another value gives unexpected_record_kind.
    json_object checks duplicate names before this function gets a record object.
    """
    def require(ok, rule):
        if not ok:
            raise ValueError(rule)

    def matches(value, pattern):
        return isinstance(value, str) and re.fullmatch(pattern, value) is not None

    require(record_kind == 'activation', 'unexpected_record_kind')  # rule 1
    require(isinstance(record, dict), 'json')  # rule 2
    require(record.get('format') == 'entelos.record/1', 'unsupported_record_format')  # rule 3
    # Rule 4: check names in text order, including candidate fields. Applicability names are data.
    for key, value in record.items():
        require(key in FIELDS, 'unknown_field:' + key)
        if key == 'candidate' and isinstance(value, dict):
            for field in value:
                require(field in {'kind', 'ref'}, 'unknown_field:candidate.' + field)
    # Rule 5: the closed schema, including optional names and all applicability levels.
    require(REQUIRED <= record.keys(), 'schema')
    for key in ('scope', 'function', 'producer', 'runner', 'approver', 'activator'):
        require(isinstance(record[key], str) and len(record[key]) >= 1, 'schema')
    require(isinstance(record['request_key'], str), 'schema')
    for key in ('event_ref', 'source_ref', 'snapshot_ref', 'report_ref', 'decision_ref'):
        require(matches(record[key], REF), 'schema')
    require(matches(record['base'], '(' + REF + ')?'), 'schema')
    cand = record['candidate']
    require(isinstance(cand, dict) and set(cand) == {'kind', 'ref'}, 'schema')
    require(cand['kind'] in ('content', 'cel-bundle') and matches(cand['ref'], REF), 'schema')
    for key in ('collection', 'partition'):
        if key in record:
            require(matches(record[key], r'[A-Za-z0-9._-]{1,128}'), 'schema')
    for key in ('from', 'until'):
        require(matches(record[key], r'[0-9]{4}-[0-9]{2}-[0-9]{2}'), 'schema')
    for key in ('targets', 'supersedes'):
        require(isinstance(record[key], list) and all(matches(ref, REF) for ref in record[key]), 'schema')
    require(isinstance(record['applicability'], dict), 'schema')
    for values in record['applicability'].values():
        require(isinstance(values, dict), 'schema')
        for levels in values.values():
            require(isinstance(levels, list) and 1 <= len(levels) <= 8, 'schema')
            require(all(isinstance(level, str) and 1 <= len(level) <= 256 for level in levels), 'schema')
    content = cand['kind'] == 'content'
    require(all((key in record) == content for key in ('collection', 'partition')), 'candidate_fields')  # rule 6
    if content:
        require(record['function'] == 'content:' + record['collection'] + '/' + record['partition'], 'subject')  # rule 7
    try:
        start, end = date.fromisoformat(record['from']), date.fromisoformat(record['until'])
    except ValueError as error:
        raise ValueError('interval') from error
    require(start < end, 'interval')  # rule 8


def records() -> dict[str, dict]:
    return json_object(STAND_IN.read_text(encoding='utf-8'))['records']


def bind_value(value: str, bound: dict[str, str]) -> str:
    return bound[value] if value.startswith('$') else value


def bind_record(record: dict, bound: dict[str, str]) -> dict:
    """Bind only the two fields whose values depend on the test store."""
    out = copy.deepcopy(record)
    out['candidate']['ref'] = bind_value(record['candidate']['ref'], bound)
    out['base'] = bind_value(record['base'], bound)
    return out


def descriptor_of(record: dict, bound: dict[str, str], receipts: dict[str, str], lock: str) -> dict:
    """Map one record to the `activation` descriptor (PUB-ACT-1). See the module table."""
    record = bind_record(record, bound)
    # Derived profile and kind: the store profile and descriptor kind, not the record format.
    # Derived lock: a build lock of the changed store members, supplied by the test.
    # Derived receipts: store receipts whose payloads contain decision_ref and report_ref, not fetched content.
    out = {'descriptor_profile': BASE, 'kind': 'activation', 'candidate': record['candidate']['ref'],
           'lock': lock, 'approval': receipts[record['decision_ref']], 'report': receipts[record['report_ref']],
           'partition': record['partition'], 'effective_from': record['from'],
           'effective_until': record['until'], 'request_key': record['request_key'],
           'producer': record['producer'], 'runner': record['runner'], 'approver': record['approver']}
    # Derived base: the bound expected store head. An empty record base gives no descriptor base.
    if record['base']:
        out['base'] = record['base']
    # Derived removed: absent because these updates replace members and remove none (PUB-ACT-2).
    return out


class Seam:
    """One gated collection with two updates, and the calls of a producer that activates a record."""

    def __init__(self):
        self.store = Store()
        self.store.gate(COLLECTION)  # PUB-GAT-1: before the first call
        self.validations = []
        self.store.accept(POLICY, lambda cand, rcpt, act: self.validations.append((cand, rcpt, act)) or True)
        self.acceptance = self.receipt({'producer': 'example/reviewer', 'media_type': 'text/plain', 'text': 'accepted'})
        first, second = self.schema('Sample notice, first text.'), self.schema('Sample notice, second text.')
        snap1, snap2 = self.snapshot(first), self.snapshot(second)
        # The second update replaces the member of the first update, with the first snapshot as its base.
        self.bound = {'$snapshot-1': snap1, '$snapshot-2': snap2, '$candidate-1': candidate(self.store, snap1, POLICY),
                      '$lock-1': self.lock(first), '$lock-2': self.lock(second)}
        self.locks = {'$candidate-1': self.bound['$lock-1'], '$candidate-2': self.bound['$lock-2']}

    def first_is_head(self) -> None:
        """Bind the candidate of the second update. Its expected head is the first snapshot, the head now (PUB-CND-2)."""
        self.bound['$candidate-2'] = candidate(self.store, self.bound['$snapshot-2'], POLICY, self.bound['$snapshot-1'])

    def receipt(self, value: dict) -> str:
        obj, data = payload(value['text'])
        return put_ref(self.store, {'descriptor_profile': BASE, 'kind': 'receipt', 'producer': value['producer'],
                                    'payload': {**obj, 'media_type': value['media_type']}}, data)

    def schema(self, text: str) -> str:
        obj, data = payload(text)
        return put_ref(self.store, {'descriptor_profile': BASE, 'kind': 'schema', 'logical_id': 'sample-notice',
                                    'schema_profile': 'sample/schema/v1', 'payload': obj}, data)

    def snapshot(self, member: str) -> str:
        result = self.store.call('create_snapshot', {'collection': COLLECTION, 'schemas': [member]})
        assert result['status'] == 'ok', result
        return result['items'][0]['ref']

    def lock(self, member: str) -> str:
        return put_ref(self.store, {'descriptor_profile': BASE, 'kind': 'build-lock', 'inputs': [member],
                                    'consumer': 'sample-consumer', 'completeness': 'sample/declared/v1'})

    def descriptor(self, record: dict) -> dict:
        # Derived receipts keep the native references as opaque payloads. They assert no approval outcome.
        receipts = {record[key]: self.receipt({'producer': 'sample-reference-adapter', 'media_type': 'text/plain',
                                              'text': record[key]}) for key in ('decision_ref', 'report_ref')}
        return descriptor_of(record, self.bound, receipts, self.locks[record['candidate']['ref']])

    def activate(self, record: dict) -> tuple[dict, str]:
        """Write reference receipts and the activation, then publish."""
        activation = put_ref(self.store, self.descriptor(record))
        result = self.store.call('publish', {'candidate': self.bound[record['candidate']['ref']], 'receipt': self.acceptance,
                                             'activation': activation})
        return result, activation

    def head(self) -> str | None:
        """The head of the collection in the catalog, or None (CTX-VIW-2)."""
        entry = self.store.call('open_context', {'heads': [COLLECTION]})['items'][0]
        if entry['status'] == 'ok':
            return entry['ref']
        assert [r['code'] for r in entry['reasons']] == ['no_head'], entry
        return None

    def publications(self) -> list[dict]:
        return self.store.call('publications', {'collection': COLLECTION})['items']


def codes(result: dict) -> list[str]:
    return sorted(r['code'] for r in result['items'][0]['reasons'])


class ActivationSeamTest(unittest.TestCase):
    def setUp(self):
        self.records = records()
        self.seam = Seam()

    def refused(self, result: dict, status: str, reasons: list[str], head, before: str):
        """A refusal gives the reason, and it changes no head and no other part of the store (PUB-HED-6, STS-ENV-4)."""
        self.assertEqual((result['status'], codes(result)), (status, reasons))
        self.assertEqual(self.seam.head(), head)
        self.assertEqual(self.seam.store.fingerprint(), before)

    def test_each_stand_in_obeys_the_consumer_rules(self):
        paths = [STAND_IN, ROOT / 'examples/library_hours/activation_record_stand_in.json']
        for path in paths:
            text = json_object(path.read_text(encoding='utf-8'))
            self.assertIs(text['stand_in'], True)  # change with the first pinned record writer
            for name, record in text['records'].items():
                with self.subTest(file=str(path), record=name):
                    self.assertEqual(set(record), FIELDS)
                    # Derived schema-test bindings are synthetic hashes of the names. They prove no identity mapping.
                    names = [record['candidate']['ref'], record['base']]
                    bound = {name: 'sha256:' + hashlib.sha256(name.encode()).hexdigest()
                             for name in names if name.startswith('$')}
                    check_record(bind_record(record, bound), text.get('record_kind'))

    def test_the_record_gives_no_dependency_that_the_test_derives(self):
        message = ('If a record layout adds such a field, map the descriptor value from that field '
                   'and remove the derived value.')
        derived_fields = {'lock', 'approval', 'report', 'inputs'}
        self.assertFalse(FIELDS & derived_fields, message)
        seam = self.seam
        self.assertEqual(seam.activate(self.records['first'])[0]['status'], 'ok')
        seam.first_is_head()
        # The library record uses another name for the first candidate of this test store.
        seam.bound['$candidate'] = seam.bound['$candidate-1']
        seam.locks['$candidate'] = seam.bound['$lock-1']
        expected_locks = {'$candidate-1': seam.bound['$lock-1'], '$candidate-2': seam.bound['$lock-2'],
                          '$candidate': seam.bound['$lock-1']}
        for path in (STAND_IN, ROOT / 'examples/library_hours/activation_record_stand_in.json'):
            for name, record in json_object(path.read_text(encoding='utf-8'))['records'].items():
                with self.subTest(file=str(path), record=name):
                    # No field gives a store lock, receipt or snapshot. base names a producer candidate.
                    self.assertFalse(record.keys() & derived_fields, message)
                    descriptor = seam.descriptor(record)
                    self.assertEqual(descriptor['lock'], expected_locks[record['candidate']['ref']], message)
                    self.assertEqual(descriptor.get('base'),
                                     seam.bound[record['base']] if record['base'] else None, message)
                    for field, reference in (('approval', 'decision_ref'), ('report', 'report_ref')):
                        receipt = seam.store.descriptor(descriptor[field])
                        self.assertEqual(receipt['kind'], 'receipt', message)
                        self.assertEqual(receipt['payload']['media_type'], 'text/plain', message)
                        data = seam.store.blob(receipt['payload']['digest'], ref=descriptor[field])
                        self.assertEqual(data.decode('utf-8'), record[reference], message)

    def test_the_stand_in_references_are_distinct(self):
        seen = set()
        for path in (STAND_IN, ROOT / 'examples/library_hours/activation_record_stand_in.json'):
            for name, record in json_object(path.read_text(encoding='utf-8'))['records'].items():
                for field in ('event_ref', 'source_ref', 'snapshot_ref', 'report_ref', 'decision_ref'):
                    with self.subTest(file=str(path), record=name, field=field):
                        self.assertNotIn(record[field], seen, 'Each stand-in reference must be distinct in both files.')
                        seen.add(record[field])

    def test_the_rule_check_refuses_changed_records(self):
        record = bind_record(self.records['first'], self.seam.bound)
        check_record(record, 'activation')  # positive pair for each fault below
        faults = [
            ({'format': 'other/1'}, 'unsupported_record_format'),
            ({'owner': 'sample'}, 'unknown_field:owner'),
            ({'candidate': {**record['candidate'], 'digest': 'sample'}}, 'unknown_field:candidate.digest'),
            ({'approver': 7}, 'schema'),
            ({'event_ref': 'sha256:A'}, 'schema'),
            ({'applicability': {'sample': {'region': []}}}, 'schema'),
            ({'candidate': {'kind': 'cel-bundle', 'ref': record['candidate']['ref']}}, 'candidate_fields'),
            ({'function': 'content:other/other'}, 'subject'),
            ({'until': record['from']}, 'interval'),
            ({'until': '2026-02-30'}, 'interval'),
        ]
        for change, reason in faults:
            with self.subTest(change=change), self.assertRaisesRegex(ValueError, '^' + reason + '$'):
                check_record({**copy.deepcopy(record), **change}, 'activation')
        with self.assertRaisesRegex(ValueError, '^unexpected_record_kind$'):
            check_record(record, 'event')
        with self.assertRaisesRegex(ValueError, '^unexpected_record_kind$'):
            check_record(record, None)
        for text in ('[]', '{"candidate":{"ref":"one","ref":"two"}}', '{"value":NaN}'):
            with self.subTest(text=text), self.assertRaisesRegex(ValueError, '^json$'):
                json_object(text)

    def test_the_head_moves_only_with_a_valid_activation(self):
        seam, bound = self.seam, self.seam.bound
        self.assertIsNone(seam.head())
        # PUB-GAT-3: no activation in a gated collection. The store does not call the validator.
        before = seam.store.fingerprint()
        result = seam.store.call('publish', {'candidate': bound['$candidate-1'], 'receipt': seam.acceptance})
        self.refused(result, 'incompatible', ['activation_required'], None, before)
        self.assertEqual(seam.validations, [])
        # PUB-HED-3 step 5.3: a valid activation moves the head, and the catalog records it (PUB-LOG-2).
        result, activation = seam.activate(self.records['first'])
        self.assertEqual(result['status'], 'ok', result)
        self.assertEqual((result['items'][0]['disposition'], result['items'][0]['head']), ('published', bound['$snapshot-1']))
        self.assertEqual(seam.head(), bound['$snapshot-1'])
        self.assertEqual(seam.validations, [(bound['$candidate-1'], seam.acceptance, activation)])  # PUB-RCP-2
        # The second update moves the head again, from its base.
        seam.first_is_head()
        result, second = seam.activate(self.records['second'])
        self.assertEqual((result['status'], result['items'][0]['disposition']), ('ok', 'published'))
        self.assertEqual(seam.head(), bound['$snapshot-2'])
        log = seam.publications()
        self.assertEqual([(p['activation'], p['previous_head'], p['head']) for p in log],
                         [(activation, None, bound['$snapshot-1']), (second, bound['$snapshot-1'], bound['$snapshot-2'])])

    def test_a_refused_activation_leaves_the_head_and_gives_the_reason(self):
        seam, bound = self.seam, self.seam.bound
        first, second = self.records['first'], self.records['second']
        # The faults of a descriptor, with the reasons of PUB-ACT-3. Each fault changes a valid descriptor.
        # The lock faults change the derived lock of the test, not a record field.
        bound['$candidate-other'] = candidate(seam.store, bound['$snapshot-2'], POLICY)
        with_no_head = [
            ({'partition': 'sample-other'}, ['activation_mismatch']),  # check 2: the partition
            ({'candidate': bound['$candidate-other']}, ['activation_mismatch']),  # check 2: a different candidate
            ({'base': bound['$snapshot-2']}, ['activation_mismatch']),  # check 2: a base, and no expected head
            ({'lock': bound['$lock-2']}, ['lock_outside_snapshot']),  # check 4: the lock covers a different update
        ]
        for change, reasons in with_no_head:
            with self.subTest(head=None, reasons=reasons, changed=list(change)):
                activation = put_ref(seam.store, {**seam.descriptor(first), **change})
                before = seam.store.fingerprint()
                result = seam.store.call('publish', {'candidate': bound['$candidate-1'], 'receipt': seam.acceptance,
                                                     'activation': activation})
                self.refused(result, 'incompatible', reasons, None, before)
        self.assertEqual(seam.validations, [])  # PUB-GAT-3: no validator call if a check fails
        self.assertEqual(seam.activate(first)[0]['status'], 'ok')
        seam.first_is_head()
        with_head = [
            ({'base': None}, ['activation_mismatch']),  # check 2: no base, and an expected head
            ({'lock': bound['$lock-1']}, ['lock_outside_snapshot']),  # check 4: the lock covers the first update
            ({'partition': 'sample-other', 'base': None}, ['activation_mismatch', 'activation_mismatch']),
        ]
        for change, reasons in with_head:
            with self.subTest(head='first', reasons=reasons):
                descriptor = {**seam.descriptor(second), **change}
                if descriptor['base'] is None:
                    del descriptor['base']
                activation = put_ref(seam.store, descriptor)
                before = seam.store.fingerprint()
                result = seam.store.call('publish', {'candidate': bound['$candidate-2'], 'receipt': seam.acceptance,
                                                     'activation': activation})
                self.refused(result, 'incompatible', reasons, bound['$snapshot-1'], before)
        self.assertEqual(len(seam.publications()), 1)
        # A validator that does not accept the record is also a refusal (PUB-HED-3 step 4).
        seam.store.accept(POLICY, lambda cand, rcpt, act: False)
        activation = put_ref(seam.store, seam.descriptor(second))
        before = seam.store.fingerprint()
        result = seam.store.call('publish', {'candidate': bound['$candidate-2'], 'receipt': seam.acceptance, 'activation': activation})
        self.refused(result, 'incompatible', ['receipt_rejected'], bound['$snapshot-1'], before)

    def test_a_repeat_with_the_same_request_key_gives_the_earlier_outcome(self):
        seam, bound = self.seam, self.seam.bound
        first, second = self.records['first'], self.records['second']
        result, activation = seam.activate(first)
        self.assertEqual(result['items'][0]['disposition'], 'published')
        before = seam.store.fingerprint()
        # The same record gives the same activation object, and the earlier outcome (PUB-HED-5). The store does not change.
        again, same = seam.activate(first)
        self.assertEqual(same, activation)
        self.assertEqual((again['status'], again['items'][0]['disposition'], again['items'][0]['head']),
                         ('ok', 'already_published', bound['$snapshot-1']))
        self.assertEqual(seam.store.fingerprint(), before)
        self.assertEqual(len(seam.validations), 1)  # no second validator call
        self.assertEqual(len(seam.publications()), 1)  # no second publication (PUB-LOG-2)
        # After the second update, a repeat of the first record does not move the head back.
        seam.first_is_head()
        self.assertEqual(seam.activate(second)[0]['items'][0]['disposition'], 'published')
        late, _ = seam.activate(first)
        self.assertEqual((late['status'], late['items'][0]['disposition'], late['items'][0]['head']),
                         ('ok', 'superseded', bound['$snapshot-2']))
        self.assertEqual(seam.head(), bound['$snapshot-2'])
        self.assertEqual(len(seam.publications()), 2)

    def test_a_repeat_of_a_refused_record_gives_the_same_refusal(self):
        seam, bound = self.seam, self.seam.bound
        record = {**self.records['first'], 'partition': 'sample-other',
                  'function': 'content:' + COLLECTION + '/sample-other'}
        outcomes = []
        for _ in range(2):
            result, activation = seam.activate(record)
            outcomes.append((result['status'], codes(result), activation, seam.head(), seam.store.fingerprint()))
        self.assertEqual(outcomes[0], outcomes[1])
        self.assertEqual(outcomes[0][:2], ('incompatible', ['activation_mismatch']))
        self.assertIsNone(outcomes[0][3])
        self.assertEqual(seam.publications(), [])
        # The valid record with the same request key is then the first publication.
        result, _ = seam.activate(self.records['first'])
        self.assertEqual((result['items'][0]['disposition'], seam.head()), ('published', bound['$snapshot-1']))


if __name__ == '__main__':
    unittest.main()
