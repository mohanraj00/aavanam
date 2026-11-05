"""Concurrent publish calls (PUB-RCP-3, PUB-HED-5). The vectors run one call at a time, so these tests cover the races.

Each validator waits at a barrier until both calls are in validation. Thus both calls pass step 4 of
PUB-HED-3 before either applies step 5, and the store does not keep the update open during validation.
"""
from __future__ import annotations

import tempfile
import threading
import unittest
from pathlib import Path

from support import Store, candidate, receipt, snapshot

TIMEOUT = 30


def race(calls) -> list[dict]:
    """Run each call in its own thread and return the results in the order of the calls."""
    results = [None] * len(calls)

    def run(n, call):
        results[n] = call()

    threads = [threading.Thread(target=run, args=(n, c)) for n, c in enumerate(calls)]
    for t in threads:
        t.start()
    for t in threads:
        t.join(TIMEOUT)
    return results


def at_barrier(count: int):
    barrier = threading.Barrier(count, timeout=TIMEOUT)

    def validate(candidate_id, receipt_ref, activation_ref):
        barrier.wait()
        return True
    return validate


def outcome(result: dict) -> str:
    i = result['items'][0]
    return i.get('disposition') or i['reasons'][0]['code']


class PublishRaceTest(unittest.TestCase):
    def two_candidates(self, store):
        rcpt = receipt(store)
        return rcpt, candidate(store, snapshot(store, 'a')), candidate(store, snapshot(store, 'b'))

    def test_two_candidates_one_head(self):
        store = Store()
        store.accept('sample-policy', at_barrier(2))
        rcpt, first, second = self.two_candidates(store)
        results = race([lambda c=c: store.call('publish', {'candidate': c, 'receipt': rcpt}) for c in (first, second)])
        self.assertEqual(sorted(map(outcome, results)), ['head_moved', 'published'])
        winner = next(r for r in results if outcome(r) == 'published')['items'][0]['head']
        loser = next(r for r in results if outcome(r) == 'head_moved')
        self.assertEqual(loser['status'], 'incompatible')
        self.assertEqual(loser['items'][0]['head'], winner)  # PUB-HED-4
        self.assertEqual(store.head('sample-collection'), winner)

    def test_same_candidate_twice(self):
        store = Store()
        store.accept('sample-policy', at_barrier(2))
        rcpt, first, _ = self.two_candidates(store)
        results = race([lambda: store.call('publish', {'candidate': first, 'receipt': rcpt})] * 2)
        self.assertEqual(sorted(map(outcome, results)), ['already_published', 'published'])  # step 5.1
        self.assertEqual(store.db.execute('SELECT count(*) FROM publications').fetchone()[0], 1)  # PUB-HED-5

    def test_two_stores_on_one_file(self):
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'store.sqlite')
            stores = [Store(path), Store(path)]
            validate = at_barrier(2)
            for s in stores:
                s.accept('sample-policy', validate)
            rcpt, first, second = self.two_candidates(stores[0])
            results = race([lambda s=s, c=c: s.call('publish', {'candidate': c, 'receipt': rcpt})
                            for s, c in zip(stores, (first, second))])
            self.assertEqual(sorted(map(outcome, results)), ['head_moved', 'published'])
            heads = {s.head('sample-collection') for s in stores}
            self.assertEqual(len(heads), 1)
            for s in stores:
                s.close()

    def test_two_stores_create_one_candidate(self):
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'store.sqlite')
            stores = [Store(path), Store(path)]
            for s in stores:
                s.accept('sample-policy', lambda c, r, a: True)
            snap = snapshot(stores[0], 'a')
            request = {'snapshot': snap, 'policy': 'sample-policy', 'expected_head': None}
            results = race([lambda s=s: s.call('create_candidate', request) for s in stores])
            self.assertEqual([r['status'] for r in results], ['ok', 'ok'])  # PUB-CND-2 step 5
            self.assertEqual(len({r['items'][0]['candidate'] for r in results}), 1)
            self.assertEqual(stores[0].db.execute('SELECT count(*) FROM candidates').fetchone()[0], 1)  # PUB-CND-4
            for s in stores:
                s.close()


if __name__ == '__main__':
    unittest.main()
