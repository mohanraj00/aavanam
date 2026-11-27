"""The removal rules that the runner cannot express: two opens of one store file, the store file, the store clock,
a layered context, and the locks and layers that `gc_collect` keeps only as declared roots."""
import sqlite3
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

from support import BASE, Store, candidate, payload, put_ref, receipt, snapshot
from aavanam.kinds import LAYER_PROFILE

SECRET = 'Synthetic source text 5b90e2 that the maintainer removes.'


def desc(kind, **fields):
    return {'descriptor_profile': BASE, 'kind': kind, **fields}


def codes(result):
    return result['status'], [r['code'] for r in result['reasons']]


def evidence(store, collection='sample-evidence'):
    """A source, a reading, an excerpt and a snapshot of one relation set. Return the references by name."""
    obj, data = payload(SECRET)
    schema, fields = payload('{"fields": ["part"]}')
    out = {'schema': put_ref(store, desc('schema', schema_profile='sample/schema/v1', payload=schema), fields)}
    table, rows = payload('{"columns": ["part"], "key": ["part"], "rows": [["seal"]]}')
    out['table'] = put_ref(store, desc('data-resource', data_profile='aavanam/table/v1', schema=out['schema'], payload=table), rows)
    out['source'] = put_ref(store, desc('source', payload=obj), data)
    out['reading'] = put_ref(store, desc('reading', source=out['source'], producer='sample/reader/v1',
                                         coverage_profile='sample/coverage/v1', payload=obj), data)
    part, bytes_ = payload(SECRET[:16])
    out['excerpt'] = put_ref(store, desc('excerpt', parent=out['reading'], ranges=[{'start': 0, 'end': 16}], payload=part), bytes_)
    out['evidence'] = put_ref(store, desc('evidence', subject=out['table'], supports=out['excerpt'], origin='sample/editor/v1'))
    out['set'] = put_ref(store, desc('relation-set', members=[out['evidence']]))
    made = store.call('create_snapshot', {'collection': collection, 'relation_sets': [out['set']]})
    assert made['status'] == 'ok', made
    out['snapshot'] = made['items'][0]['ref']
    return out


def opened(store, root, **fields):
    result = store.call('open_context', {'roots': [root], **fields})
    assert result['status'] == 'ok', result
    return result['context']


class RemovalTest(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.path = str(Path(self.folder.name) / 'store.db')

    def tearDown(self):
        self.folder.cleanup()

    def remove(self, store, ref, reason='sample request'):
        return store.call('remove', {'ref': ref, 'reason': reason})

    def test_a_record_has_the_time_of_the_store_clock_and_no_content(self):  # REL-RMV-5
        store = Store()
        refs = evidence(store)
        store.clock = lambda: datetime(2026, 1, 2, 3, 4, 5, tzinfo=timezone.utc)
        record = self.remove(store, refs['source'])['items'][0]
        self.assertEqual(record, {'status': 'ok', 'reasons': [], 'sequence': 1, 'ref': refs['source'], 'kind': 'source',
                                  'time': '2026-01-02T03:04:05Z', 'reason': 'sample request'})
        self.assertEqual(store.call('removals', {})['items'], [record])
        self.assertNotIn(SECRET, repr(store.db.execute('SELECT * FROM removals').fetchall()))

    def test_a_later_open_does_not_give_the_removed_object(self):  # REL-RMV-7
        first = Store(self.path)
        refs = evidence(first)
        self.assertEqual(self.remove(first, refs['excerpt'])['status'], 'ok')
        first.close()
        again = Store(self.path)
        result = again.call('get', {'context': opened(again, refs['snapshot']), 'refs': [refs['excerpt']]})
        self.assertEqual(codes(result), ('not_found', ['removed']))
        self.assertEqual([i['ref'] for i in again.call('removals', {})['items']], [refs['excerpt']])
        again.close()

    def test_a_second_open_does_not_give_a_descriptor_from_its_cache(self):  # REL-RMV-7, CTX-LIF-4
        first, second = Store(self.path), Store(self.path)
        refs = evidence(first)
        context = opened(second, refs['snapshot'])
        self.assertEqual(second.call('get', {'context': context, 'refs': [refs['excerpt']]})['status'], 'ok')
        self.assertEqual(self.remove(first, refs['excerpt'])['status'], 'ok')
        self.assertEqual(codes(second.call('get', {'context': context, 'refs': [refs['excerpt']]})), ('failed', ['context_expired']))
        result = second.call('open_context', {'roots': [refs['snapshot']]})
        self.assertEqual(result['items'][0]['removed'], [refs['excerpt']])
        again = second.call('get', {'context': result['context'], 'refs': [refs['excerpt']]})
        self.assertEqual(codes(again), ('not_found', ['removed']))
        first.close()
        second.close()

    def test_the_store_file_has_no_removed_bytes(self):  # REL-RMV-7: the reference store also clears the free page
        store = Store(self.path)
        refs = evidence(store)
        store.close()
        self.assertIn(SECRET.encode(), Path(self.path).read_bytes())
        store = Store(self.path)
        for name in ('excerpt', 'reading', 'source'):  # REL-RMV-4: each object that holds the content
            self.assertEqual(self.remove(store, refs[name])['status'], 'ok')
        store.close()
        self.assertNotIn(SECRET.encode(), Path(self.path).read_bytes())
        self.assertNotIn(SECRET[:16].encode(), Path(self.path).read_bytes())

    def test_a_layered_context_keeps_unknown_ref(self):  # REL-RMV-7, LYR-ISO-2
        store = Store(profiles=(LAYER_PROFILE,))
        refs = evidence(store)
        layer = put_ref(store, desc('layer', base='sample-evidence', scope='sample-scope'))
        self.assertEqual(self.remove(store, refs['excerpt'])['status'], 'ok')
        result = store.call('open_context', {'roots': [refs['snapshot']], 'layer': {'ref': layer, 'scope': 'sample-scope'}})
        self.assertEqual(result['status'], 'ok', result)
        self.assertEqual(result['items'][0]['removed'], [refs['excerpt']])
        got = store.call('get', {'context': result['context'], 'refs': [refs['excerpt'], refs['evidence']]})
        self.assertEqual([codes(i) for i in got['items']], [('not_found', ['unknown_ref']), ('ok', [])])

    def test_a_removal_does_not_change_the_producer_section(self):  # PRD-RMV-5, REL-RMV-12
        store = Store()
        refs = evidence(store)
        store.call('producer_put', {'producer': 'example/run-writer', 'key': 'k', 'data': 'Ynl0ZXM='})
        self.assertEqual(self.remove(store, refs['source'])['status'], 'ok')
        self.assertEqual(store.call('gc_collect', {})['status'], 'ok')
        self.assertEqual(store.call('producer_get', {'producer': 'example/run-writer', 'key': 'k'})['items'][0]['data'], 'Ynl0ZXM=')
        self.assertEqual(store.call('producer_removals', {'producer': 'example/run-writer'})['items'], [])


class ContextLifeTest(unittest.TestCase):
    def test_a_life_ends_at_the_store_clock(self):  # CTX-LIF-2
        store = Store()
        refs = evidence(store)
        start = datetime(2026, 1, 2, 3, 4, 5, 250000, tzinfo=timezone.utc)
        store.clock = lambda: start
        context = opened(store, refs['snapshot'], life=1)
        store.clock = lambda: start + timedelta(milliseconds=999)
        self.assertEqual(store.call('get', {'context': context, 'refs': [refs['set']]})['status'], 'ok')
        store.clock = lambda: start + timedelta(seconds=1)
        self.assertEqual(codes(store.call('get', {'context': context, 'refs': [refs['set']]})), ('failed', ['context_expired']))
        self.assertEqual(codes(store.call('close_context', {'context': context})), ('failed', ['context_expired']))

    def test_a_context_of_an_older_store_file_has_no_life(self):  # CTX-LIF-1
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'store.db')
            db = sqlite3.connect(path)
            db.execute('CREATE TABLE contexts (id TEXT PRIMARY KEY, roots TEXT NOT NULL, capabilities TEXT NOT NULL)')
            db.execute("INSERT INTO contexts VALUES ('ctx:old', '[]', '[]')")
            db.commit()
            db.close()
            store = Store(path)
            store.clock = lambda: datetime.now(timezone.utc) + timedelta(days=3650)
            self.assertEqual(store.call('close_context', {'context': 'ctx:old'})['status'], 'ok')
            store.close()
            # The table `contexts` keeps its three columns, so an earlier version of the store reads the file.
            db = sqlite3.connect(path)
            self.assertEqual([c for (_, c, *_) in db.execute('PRAGMA table_info(contexts)')], ['id', 'roots', 'capabilities'])
            db.close()


class CollectTest(unittest.TestCase):
    def test_a_lock_and_a_layer_stay_only_as_declared_roots(self):  # LIF-GC-3
        store = Store(profiles=(LAYER_PROFILE,))
        refs = evidence(store)
        context = opened(store, refs['snapshot'])
        lock = store.call('build_inputs', {'context': context, 'inputs': [refs['set']], 'consumer': 'sample/emitter/build/v1',
                                           'completeness': 'sample/declared/v1'})['lock']
        layer = put_ref(store, desc('layer', base='sample-evidence', scope='sample-scope'))
        kept = store.call('gc_collect', {'roots': [lock, layer]})
        self.assertEqual((kept['status'], kept['items'], kept['payloads'], kept['candidates']), ('ok', [], [], []))
        self.assertEqual(store.call('close_context', {'context': context})['status'], 'ok')
        kept = store.call('gc_collect', {'roots': [lock]})  # the lock keeps the closure of its inputs
        self.assertEqual([(i['ref'], i['kind']) for i in kept['items']], sorted([(layer, 'layer'), (refs['snapshot'], 'snapshot')]))
        gone = store.call('gc_collect', {})
        self.assertEqual(len(gone['items']), 8, gone)  # the lock and the 7 objects of the closure of the set
        self.assertEqual(len(gone['payloads']), 4)  # the source and the reading have the same bytes
        self.assertEqual(store.db.execute('SELECT count(*) FROM objects').fetchone()[0], 0)
        self.assertEqual(store.db.execute('SELECT count(*) FROM edges').fetchone()[0], 0)
        self.assertEqual(store.db.execute('SELECT count(*) FROM blobs').fetchone()[0], 0)

    def test_a_second_open_agrees_with_a_collect_of_the_first_open(self):  # CTX-LIF-6, LIF-GC-7
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'store.db')
            first, second = Store(path), Store(path)
            refs = evidence(first)
            context = opened(second, refs['snapshot'])
            self.assertEqual(first.call('gc_collect', {})['items'], [])  # the context of the second open keeps its members
            self.assertEqual(second.call('close_context', {'context': context})['status'], 'ok')
            self.assertEqual(len(first.call('gc_collect', {})['items']), 8)
            again = second.call('open_context', {'roots': [refs['snapshot']]})  # not from the cache of the second open
            self.assertEqual(codes(again), ('not_found', ['unknown_ref']))
            first.close()
            second.close()

    def test_collect_keeps_a_context_that_a_second_open_opens_before_the_transaction(self):  # CTX-LIF-6, LIF-GC-5
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'store.db')
            first, second = Store(path), Store(path)
            refs = evidence(first)
            roots, contexts = first.retained_roots, []

            def roots_then_open(request):  # the second open commits a context after the first open read the roots
                out = roots(request)
                if not contexts:
                    contexts.append(opened(second, refs['snapshot']))
                return out
            first.retained_roots = roots_then_open
            result = first.call('gc_collect', {})
            self.assertEqual((result['status'], result['items'], result['payloads']), ('ok', [], []))
            got = second.call('get', {'context': contexts[0], 'refs': [refs['excerpt']]})
            self.assertEqual((got['status'], got['items'][0]['status']), ('ok', 'ok'))
            first.close()
            second.close()

    def test_a_context_does_not_open_on_content_that_a_second_open_collects(self):  # LIF-GC-5, STS-MAL-2
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'store.db')
            first, second = Store(path), Store(path)
            refs = evidence(first)
            gone_in, collected = second.gone_in, []

            def gone_then_collect(roots):  # the first open collects after the second open checked the roots
                out = gone_in(roots)
                if not collected:
                    collected.append(first.call('gc_collect', {}))
                return out
            second.gone_in = gone_then_collect
            result = second.call('open_context', {'roots': [refs['snapshot']]})
            self.assertEqual(len(collected[0]['items']), 8, collected)
            self.assertEqual(codes(result), ('not_found', ['unknown_ref']))
            self.assertEqual(second.db.execute('SELECT count(*) FROM contexts').fetchone()[0], 0)
            first.close()
            second.close()

    def test_an_output_store_does_not_read_a_collected_object_from_the_cache_of_its_device_store(self):  # BUN-RMV-2, BUN-IMP-8
        writer = Store()
        refs = evidence(writer)
        bundle = writer.call('export', {'context': opened(writer, refs['snapshot']), 'roots': [refs['snapshot']]})['bundle']
        with tempfile.TemporaryDirectory() as folder:
            paths = str(Path(folder) / 'device.db'), str(Path(folder) / 'output.db')
            first, second = (Store(paths[0], role='device', output_path=paths[1]) for _ in range(2))
            self.assertEqual(first.call('import', {'bundle': bundle})['status'], 'ok')
            context = opened(second.output, refs['snapshot'])  # the second open keeps the descriptors in its cache
            self.assertEqual(second.output.call('close_context', {'context': context})['status'], 'ok')
            self.assertEqual(len(first.call('gc_collect', {'roots': []})['items']), 8)
            for store in (second.output, second, first.output):
                self.assertEqual(codes(store.call('open_context', {'roots': [refs['snapshot']]})), ('not_found', ['unknown_ref']))
            first.close()
            second.close()

    def test_a_publish_does_not_record_a_receipt_that_a_collect_removed_during_the_validation(self):  # LIF-GC-5, PUB-HED-3
        store, calls = Store(), []

        def collect_then_accept(*_):  # the store keeps no update open while the validator runs (PUB-RCP-3)
            calls.append(store.call('gc_collect', {}))
            return True
        store.accept('sample-policy', collect_then_accept)
        rcpt, identity = receipt(store), candidate(store, snapshot(store, 'a'))
        result = store.call('publish', {'candidate': identity, 'receipt': rcpt})
        self.assertIn(rcpt, [i['ref'] for i in calls[0]['items']])  # no retained root reaches the receipt (LIF-GC-2)
        self.assertEqual(len(calls), 1)  # the second run of the call stops at step 1
        self.assertEqual((result['status'], [r['code'] for r in result['items'][0]['reasons']]), ('not_found', ['unknown_ref']))
        self.assertEqual(store.call('publications', {'collection': 'sample-collection'})['items'], [])

    def test_collect_keeps_the_records_of_the_store(self):  # LIF-GC-5
        store = Store()
        refs = evidence(store)
        self.assertEqual(store.call('remove', {'ref': refs['excerpt'], 'reason': 'sample request'})['status'], 'ok')
        result = store.call('gc_collect', {})
        self.assertEqual(len(result['items']), 7, result)
        self.assertEqual(len(store.call('removals', {})['items']), 1)
        got = store.call('open_context', {'roots': [refs['excerpt']]})
        self.assertEqual(codes(got), ('not_found', ['removed']))  # REL-RMV-7: the record stays, so the reason stays


if __name__ == '__main__':
    unittest.main()
