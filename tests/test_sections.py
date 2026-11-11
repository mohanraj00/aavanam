"""The storage section rules that the runner cannot express: declarations and two opens of one store."""
import json
import tempfile
import unittest
from pathlib import Path

from support import BASE, Store, payload, put_ref

SERVING = 'sample-serving'


def snapshot(store, collection):
    return store.call('create_snapshot', {'collection': collection})


class SectionTest(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.path = str(Path(self.folder.name) / 'store.db')

    def tearDown(self):
        self.folder.cleanup()

    def test_a_declaration_has_a_collection_name_and_a_known_section(self):  # PUB-SEC-1
        for collection, section in ((None, 'serving'), (SERVING, 'producer'), (SERVING, None)):
            with self.assertRaises(TypeError):
                Store().section(collection, section)

    def test_a_declaration_comes_before_the_first_call(self):  # PUB-SEC-1
        store = Store()
        store.section(SERVING, 'serving')
        store.call('publications', {'collection': SERVING})
        with self.assertRaises(ValueError):
            store.section('sample-other', 'evidence')

    def test_a_repeat_has_no_effect_and_a_different_section_is_refused(self):  # PUB-SEC-2
        store = Store()
        store.section(SERVING, 'serving')
        store.section(SERVING, 'serving')
        with self.assertRaises(ValueError):
            store.section(SERVING, 'evidence')
        self.assertEqual(store.section_of(SERVING), 'serving')

    def test_a_collection_with_a_snapshot_is_refused(self):  # PUB-SEC-2
        store = Store(self.path)
        self.assertEqual(snapshot(store, SERVING)['status'], 'ok')
        store.close()
        store = Store(self.path)
        with self.assertRaises(ValueError):
            store.section(SERVING, 'serving')
        self.assertIsNone(store.section_of(SERVING))
        store.section('sample-other', 'serving')  # a different collection has no snapshot
        store.close()

    def test_a_declaration_during_a_snapshot_write(self):  # PUB-SEC-2, PUB-SEC-3
        store = Store(self.path)
        schema_obj, schema_data = payload('schema')
        schema = put_ref(store, {'descriptor_profile': BASE, 'kind': 'schema', 'schema_profile': 'sample/schema/v1', 'payload': schema_obj}, schema_data)
        table_obj, table_data = payload('table')
        table = put_ref(store, {'descriptor_profile': BASE, 'kind': 'data-resource', 'data_profile': 'sample/table/v1', 'schema': schema, 'payload': table_obj}, table_data)
        source_obj, source_data = payload('source')
        source = put_ref(store, {'descriptor_profile': BASE, 'kind': 'source', 'payload': source_obj}, source_data)
        reading_obj, reading_data = payload('reading')
        reading = put_ref(store, {'descriptor_profile': BASE, 'kind': 'reading', 'source': source, 'producer': 'sample/reader/v1', 'coverage_profile': 'sample/coverage/v1', 'payload': reading_obj}, reading_data)
        evidence = put_ref(store, {'descriptor_profile': BASE, 'kind': 'evidence', 'subject': table, 'supports': reading, 'origin': 'sample/fixture/v1'})
        members = put_ref(store, {'descriptor_profile': BASE, 'kind': 'relation-set', 'members': [evidence]})
        write, target = store.write, [SERVING]

        def declare_then_write(objects, blobs, *rest):  # a different open declares after the check of call()
            other = Store(self.path)
            other.section(target[0], 'serving')
            other.close()
            write(objects, blobs, *rest)
        store.write = declare_then_write
        before = store.fingerprint()
        result = store.call('create_snapshot', {'collection': SERVING, 'relation_sets': [members]})
        self.assertEqual(result['status'], 'failed')
        self.assertEqual({r['code'] for r in result['items'][0]['reasons']}, {'section_violation'})
        self.assertEqual(len(result['items'][0]['reasons']), 3)  # the evidence, the reading and the source
        self.assertEqual(store.fingerprint(), before)  # STS-ENV-4
        target[0] = 'sample-other'
        snapshots = [json.dumps({'descriptor_profile': BASE, 'kind': 'snapshot', 'collection': target[0], 'logical_id': name, 'relation_sets': [members]}) for name in 'ab']
        result = store.call('put', {'descriptors': snapshots, 'payloads': {}})
        self.assertEqual([i['status'] for i in result['items']], ['failed', 'failed'])
        for each in result['items']:  # each snapshot has its own reasons (PUB-SEC-3), also after the check of call()
            self.assertEqual({r['code'] for r in each['reasons']}, {'section_violation'})
        store.close()

    def test_only_a_writer_store_has_sections(self):  # PUB-SEC-1
        device = Store(role='device')
        for store in (device, device.output):
            with self.assertRaises(ValueError):
                store.section(SERVING, 'serving')
            self.assertIsNone(store.section_of(SERVING))
        device.close()

    def test_a_later_open_keeps_the_section(self):  # PUB-SEC-2
        store = Store(self.path)
        store.section(SERVING, 'serving')
        store.close()
        store = Store(self.path)
        self.assertEqual(store.section_of(SERVING), 'serving')
        store.close()


if __name__ == '__main__':
    unittest.main()
