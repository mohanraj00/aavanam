"""Gated collections and the publication record (PUB-GAT-1 to PUB-GAT-3, PUB-ACT-3, PUB-LOG-2).

The vectors declare each gate before the first step of one open. These tests cover a later open, a gate
that a different open declares during the validation, and a store from before the publication record.
"""
from __future__ import annotations

import sqlite3
import tempfile
import unittest
from pathlib import Path

import threading

from support import BASE, Store, candidate, payload, put_ref, receipt, snapshot

COLLECTION = 'sample-collection'


def schema(store, name: str) -> str:
    obj, data = payload(name)
    return put_ref(store, {'descriptor_profile': BASE, 'kind': 'schema', 'logical_id': name,
                           'schema_profile': 'sample/schema/v1', 'payload': obj}, data)


def activate(store, cand: str, schemas: list, base=None, removed=None) -> str:
    lock = put_ref(store, {'descriptor_profile': BASE, 'kind': 'build-lock', 'inputs': schemas,
                           'consumer': 'sample-consumer', 'completeness': 'sample/declared/v1'})
    rcpt = receipt(store, 'approved')
    value = {'descriptor_profile': BASE, 'kind': 'activation', 'candidate': cand, 'lock': lock, 'approval': rcpt,
             'report': rcpt, 'partition': COLLECTION, 'effective_from': '2026-01-01', 'request_key': 'sample-request',
             'producer': 'sample-producer', 'runner': 'sample-runner', 'approver': 'sample-approver'}
    if base:
        value['base'] = base
    if removed is not None:
        value['removed'] = removed
    return put_ref(store, value)


def members(store, schemas: list) -> str:
    result = store.call('create_snapshot', {'collection': COLLECTION, 'schemas': schemas})
    assert result['status'] == 'ok', result
    return result['items'][0]['ref']


def publish(store, cand: str, rcpt: str, activation=None) -> dict:
    request = {'candidate': cand, 'receipt': rcpt}
    if activation:
        request['activation'] = activation
    return store.call('publish', request)


def code(result: dict) -> str:
    i = result['items'][0]
    return i.get('disposition') or i['reasons'][0]['code']


class GateTest(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.path = str(Path(self.folder.name) / 'store.sqlite')

    def tearDown(self):
        self.folder.cleanup()

    def test_gate_before_the_first_call_only(self):
        store = Store()
        store.gate(COLLECTION)
        store.call('publications', {'collection': COLLECTION})
        with self.assertRaises(ValueError):  # PUB-GAT-1
            store.gate('sample-other')
        with self.assertRaises(TypeError):
            Store().gate(None)

    def test_a_later_open_keeps_the_gate(self):
        store = Store(self.path)
        store.gate(COLLECTION)
        store.close()
        store = Store(self.path)  # no declaration in this open (PUB-GAT-2)
        store.accept('sample-policy', lambda c, r, a: True)
        rcpt = receipt(store)
        cand = candidate(store, snapshot(store, 'a'))
        result = publish(store, cand, rcpt)
        self.assertEqual((result['status'], code(result)), ('incompatible', 'activation_required'))
        self.assertIsNone(store.head(COLLECTION))
        store.close()

    def test_a_gate_during_the_validation(self):
        store = Store(self.path)
        calls = []

        def validate(c, r, a):
            other = Store(self.path)  # a different open declares the gate while this validator runs
            other.gate(COLLECTION)
            other.close()
            calls.append(a)
            return True
        store.accept('sample-policy', validate)
        rcpt = receipt(store)
        cand = candidate(store, snapshot(store, 'a'))
        result = publish(store, cand, rcpt)
        self.assertEqual(calls, [None])  # the collection was not gated at step 3 (PUB-RCP-2)
        self.assertEqual((result['status'], code(result)), ('incompatible', 'activation_required'))  # action 5.1
        self.assertIsNone(store.head(COLLECTION))  # STS-ENV-4
        self.assertEqual(store.call('publications', {'collection': COLLECTION})['items'], [])
        store.close()

    def test_a_competing_publish_with_activations(self):
        store = Store(self.path)
        barrier = threading.Barrier(2, timeout=30)
        store.accept('sample-policy', lambda c, r, a: barrier.wait() is not None)
        rcpt = receipt(store)
        a, b = schema(store, 'a'), schema(store, 'b')
        first, second = candidate(store, members(store, [a])), candidate(store, members(store, [b]))
        calls = [(first, activate(store, first, [a])), (second, activate(store, second, [b]))]
        results = [None, None]

        def run(n):
            other = Store(self.path)  # each call has its own open
            other.accept('sample-policy', lambda c, r, x: barrier.wait() is not None)
            results[n] = publish(other, calls[n][0], rcpt, calls[n][1])
            other.close()
        threads = [threading.Thread(target=run, args=(n,)) for n in (0, 1)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(30)
        self.assertEqual(sorted(code(r) for r in results), ['head_moved', 'published'])  # PUB-RCP-3
        self.assertEqual(len(store.call('publications', {'collection': COLLECTION})['items']), 1)  # PUB-LOG-2
        store.close()

    def test_a_gate_during_the_validation_of_a_removal(self):
        store = Store(self.path)
        store.accept('sample-policy', lambda c, r, x: True)
        rcpt = receipt(store)
        a, b = schema(store, 'a'), schema(store, 'b')
        n1 = members(store, [a, b])
        k1 = candidate(store, n1)
        self.assertEqual(code(publish(store, k1, rcpt, activate(store, k1, [a, b]))), 'published')
        k2 = candidate(store, members(store, [a]), expected_head=n1)
        listed = activate(store, k2, [a], base=n1, removed=[b])  # correct in a collection that is not gated
        calls = []

        def validate(c, r, x):
            other = Store(self.path)  # a different open declares the gate while this validator runs
            other.gate(COLLECTION)
            other.close()
            calls.append(x)
            return True
        store.accept('sample-policy', validate)
        result = publish(store, k2, rcpt, listed)
        self.assertEqual(calls, [listed])  # the checks of step 3 passed (PUB-ACT-4)
        self.assertEqual((result['status'], code(result)), ('incompatible', 'removal_refused'))  # action 5.1
        self.assertEqual(result['items'][0]['reasons'][0]['refs'], [b])
        self.assertEqual(store.head(COLLECTION), n1)  # STS-ENV-4
        store.close()

    def test_a_store_before_the_publication_record(self):
        store = Store(self.path)
        store.accept('sample-policy', lambda c, r, a: True)
        rcpt = receipt(store)
        snap = snapshot(store, 'a')
        cand = candidate(store, snap)
        self.assertEqual(code(publish(store, cand, rcpt)), 'published')
        store.close()
        db = sqlite3.connect(self.path)  # the table form before PUB-LOG-2
        db.executescript('CREATE TABLE old (candidate TEXT PRIMARY KEY, receipt TEXT NOT NULL);'
                         'INSERT INTO old SELECT candidate, receipt FROM publications;'
                         'DROP TABLE publications; ALTER TABLE old RENAME TO publications;')
        db.close()
        db = sqlite3.connect(self.path)  # a failed migration leaves the old form (one transaction)
        db.execute('CREATE TRIGGER stop BEFORE UPDATE ON publications BEGIN SELECT RAISE(ABORT, "stop"); END')
        db.commit()
        db.close()
        with self.assertRaises(sqlite3.DatabaseError):
            Store(self.path)
        db = sqlite3.connect(self.path)
        self.assertNotIn('head', [c for (_, c, *_) in db.execute('PRAGMA table_info(publications)')])
        db.execute('DROP TRIGGER stop')
        db.commit()
        db.close()
        store = Store(self.path)
        items = store.call('publications', {'collection': COLLECTION})['items']
        self.assertEqual(items, [{'status': 'ok', 'reasons': [], 'candidate': cand, 'receipt': rcpt,
                                  'activation': None, 'previous_head': None, 'head': snap}])
        store.close()


if __name__ == '__main__':
    unittest.main()
