"""The seam test of the activation gate (PUB-GAT-1 to PUB-GAT-3, PUB-ACT-1 to PUB-ACT-5, PUB-HED-3 to PUB-HED-5, issue #4).

A workflow producer activates an update and publishes it to a gated collection. This test checks the store side:

- the head moves only with a valid activation;
- a refused activation leaves the head unchanged and gives the reason;
- a repeat with the same request key gives the earlier outcome.

The activation records are a STAND-IN. They come from `tests/activation_record_stand_in.json`, not from a release
of the producer. The producer project has no release with the signed activation record yet. The note in that file
says how to replace it with a pinned release. Only `descriptor_of` reads the layout of a record.

Each expected outcome comes from the clauses of document 13, not from a run of the store.
"""
from __future__ import annotations

import json
import unittest

from support import BASE, ROOT, Store, candidate, payload, put_ref

STAND_IN = ROOT / 'tests/activation_record_stand_in.json'
COLLECTION = 'sample-notices'
POLICY = 'sample-policy'
# The field list of the activation record of the producer (the issue in the `source` of the stand-in).
FIELDS = {'candidate', 'base', 'lock', 'approval', 'report', 'approver', 'producer', 'runner', 'partition', 'effective',
          'request_key'}


def records() -> dict[str, dict]:
    return json.loads(STAND_IN.read_text(encoding='utf-8'))['records']


def descriptor_of(record: dict, bound: dict[str, str], receipts: dict[str, str]) -> dict:
    """The `activation` descriptor of one record (PUB-ACT-1). This function is the mapping from the record layout.

    The store keeps the interval, the request key and the names as strings. It does not evaluate them (PUB-ACT-5).
    """
    out = {'descriptor_profile': BASE, 'kind': 'activation', 'candidate': bound[record['candidate']],
           'lock': bound[record['lock']], 'approval': receipts['approval'], 'report': receipts['report'],
           'partition': record['partition'], 'effective_from': record['effective']['from'],
           'effective_until': record['effective']['until'], 'request_key': record['request_key'],
           'producer': record['producer'], 'runner': record['runner'], 'approver': record['approver']}
    if record['base'] is not None:
        out['base'] = bound[record['base']]
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

    def activate(self, record: dict) -> tuple[dict, str]:
        """The calls of the producer for one record: write the receipts and the activation, then publish."""
        receipts = {name: self.receipt(record[name]) for name in ('approval', 'report')}
        activation = put_ref(self.store, descriptor_of(record, self.bound, receipts))
        result = self.store.call('publish', {'candidate': self.bound[record['candidate']], 'receipt': self.acceptance,
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

    def test_the_stand_in_has_the_field_list_of_the_record(self):
        text = json.loads(STAND_IN.read_text(encoding='utf-8'))
        self.assertIs(text['stand_in'], True)  # change this test when a pinned release replaces the stand-in
        for name, record in self.records.items():
            with self.subTest(record=name):
                self.assertEqual(set(record), FIELDS)
                self.assertLess(record['effective']['from'], record['effective']['until'])  # a half-open interval

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
        # The faults of a record, with the reasons of PUB-ACT-3. Each fault changes one field of a valid record.
        bound['$candidate-other'] = candidate(seam.store, bound['$snapshot-2'], POLICY)
        with_no_head = [
            ({**first, 'partition': 'sample-other'}, ['activation_mismatch']),  # check 2: the partition
            ({**first, 'candidate': '$candidate-other'}, ['activation_mismatch']),  # check 2: a different candidate
            ({**first, 'base': '$snapshot-2'}, ['activation_mismatch']),  # check 2: a base, and no expected head
            ({**first, 'lock': '$lock-2'}, ['lock_outside_snapshot']),  # check 4: the lock covers a different update
        ]
        for record, reasons in with_no_head:
            with self.subTest(head=None, reasons=reasons, changed=[k for k in record if record[k] != first[k]]):
                receipts = {name: seam.receipt(record[name]) for name in ('approval', 'report')}
                activation = put_ref(seam.store, descriptor_of(record, bound, receipts))
                before = seam.store.fingerprint()
                result = seam.store.call('publish', {'candidate': bound['$candidate-1'], 'receipt': seam.acceptance,
                                                     'activation': activation})
                self.refused(result, 'incompatible', reasons, None, before)
        self.assertEqual(seam.validations, [])  # PUB-GAT-3: no validator call if a check fails
        self.assertEqual(seam.activate(first)[0]['status'], 'ok')
        seam.first_is_head()
        with_head = [
            ({**second, 'base': None}, ['activation_mismatch']),  # check 2: no base, and an expected head
            ({**second, 'lock': '$lock-1'}, ['lock_outside_snapshot']),  # check 4: the lock covers the first update
            ({**second, 'partition': 'sample-other', 'base': None}, ['activation_mismatch', 'activation_mismatch']),
        ]
        for record, reasons in with_head:
            with self.subTest(head='first', reasons=reasons):
                receipts = {name: seam.receipt(record[name]) for name in ('approval', 'report')}
                activation = put_ref(seam.store, descriptor_of(record, bound, receipts))
                before = seam.store.fingerprint()
                result = seam.store.call('publish', {'candidate': bound['$candidate-2'], 'receipt': seam.acceptance,
                                                     'activation': activation})
                self.refused(result, 'incompatible', reasons, bound['$snapshot-1'], before)
        self.assertEqual(len(seam.publications()), 1)
        # A validator that does not accept the record is also a refusal (PUB-HED-3 step 4).
        seam.store.accept(POLICY, lambda cand, rcpt, act: False)
        receipts = {name: seam.receipt(second[name]) for name in ('approval', 'report')}
        activation = put_ref(seam.store, descriptor_of(second, bound, receipts))
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
        record = {**self.records['first'], 'partition': 'sample-other'}
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
