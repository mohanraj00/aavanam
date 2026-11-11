"""The producer section rules that the runner cannot express: two opens, the store file, the clock and export."""
import base64
import re
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

from support import BASE, Store, payload, put_ref

P = 'example/run-writer'
SECRET = b'Synthetic tenant text 7d41c0 that the producer removes.'


def b64(data: bytes) -> str:
    return base64.b64encode(data).decode()


class ProducerTest(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.path = str(Path(self.folder.name) / 'store.db')

    def tearDown(self):
        self.folder.cleanup()

    def call(self, store, operation, **fields):
        return store.call(operation, {'producer': P, **fields})

    def test_a_later_open_reads_the_same_data(self):  # PRD-SEC-5
        first = Store(self.path)
        self.assertEqual(self.call(first, 'producer_put', key='k', data=b64(b'bytes'))['status'], 'ok')
        self.assertEqual(self.call(first, 'producer_append', log=['l'], sequence=1, entry=b64(b'e'))['status'], 'ok')
        first.close()
        again = Store(self.path)
        self.assertEqual(self.call(again, 'producer_get', key='k')['items'][0]['data'], b64(b'bytes'))
        self.assertEqual([i['sequence'] for i in self.call(again, 'producer_read', log=['l'])['items']], [1])
        again.close()

    def test_two_opens_cannot_add_the_same_sequence(self):  # PRD-LOG-2
        first, second = Store(self.path), Store(self.path)
        self.assertEqual(self.call(first, 'producer_append', log=['l'], sequence=1, entry=b64(b'a'))['status'], 'ok')
        moved = self.call(second, 'producer_append', log=['l'], sequence=1, entry=b64(b'b'))
        self.assertEqual((moved['status'], [r['code'] for r in moved['reasons']]), ('incompatible', ['log_moved']))
        self.assertEqual(self.call(second, 'producer_append', log=['l'], sequence=2, entry=b64(b'b'))['status'], 'ok')
        first.close()
        second.close()

    def test_a_removal_record_has_the_time_of_the_store_clock(self):  # PRD-RMV-1
        store = Store()
        store.clock = lambda: datetime(2026, 1, 2, 3, 4, 5, tzinfo=timezone.utc)
        self.call(store, 'producer_put', key='k', data=b64(b'bytes'))
        record = self.call(store, 'producer_forget', key='k', reason='asked')['items'][0]
        self.assertEqual(record['time'], '2026-01-02T03:04:05Z')
        self.assertEqual(self.call(store, 'producer_removals')['items'][0]['time'], '2026-01-02T03:04:05Z')
        store.clock = lambda: datetime.now(timezone.utc)
        self.call(store, 'producer_put', key='k2', data=b64(b'bytes'))
        later = self.call(store, 'producer_forget', key='k2', reason='asked')['items'][0]
        self.assertRegex(later['time'], r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$')
        self.assertEqual(later['sequence'], 2)

    def test_the_store_file_has_no_removed_bytes(self):  # PRD-RMV-3: the reference store also clears the free page
        store = Store(self.path)
        self.call(store, 'producer_put', key='k', data=b64(SECRET))
        store.close()
        self.assertIn(SECRET, Path(self.path).read_bytes())
        store = Store(self.path)
        self.assertEqual(self.call(store, 'producer_forget', key='k', reason='asked')['status'], 'ok')
        store.close()
        self.assertNotIn(SECRET, Path(self.path).read_bytes())

    def test_a_removal_does_not_remove_a_payload_with_the_same_bytes(self):  # PRD-RMV-5
        store = Store()
        obj, data = payload(SECRET.decode())
        source = put_ref(store, {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}, data)
        self.call(store, 'producer_put', key=obj['digest'], data=b64(SECRET))
        self.assertEqual(self.call(store, 'producer_forget', key=obj['digest'], reason='asked')['status'], 'ok')
        self.assertEqual(store.blob(obj['digest']), SECRET)
        self.assertIsNotNone(store.descriptor(source))

    def test_an_export_has_no_producer_data(self):  # PRD-ISO-3
        store = Store()
        obj, data = payload('{"fields":["total"]}')
        schema = put_ref(store, {'descriptor_profile': BASE, 'kind': 'schema', 'schema_profile': 'sample/schema/v1',
                                 'payload': obj}, data)
        made = store.call('create_snapshot', {'collection': 'sample', 'schemas': [schema]})
        self.assertEqual(made['status'], 'ok', made)
        unit = made['items'][0]['ref']
        self.call(store, 'producer_put', key=obj['digest'] + '-copy', data=b64(SECRET))
        self.call(store, 'producer_append', log=[obj['digest']], sequence=1, entry=b64(SECRET))
        context = store.call('open_context', {'roots': [unit]})
        self.assertEqual(context['status'], 'ok', context)
        bundle = store.call('export', {'context': context['context'], 'roots': [unit]})['bundle']
        text = repr(bundle)
        self.assertNotIn(b64(SECRET), text)
        self.assertEqual(sorted(bundle['payloads']), [obj['digest']])
        self.assertIsNone(re.search('producer_', text))


if __name__ == '__main__':
    unittest.main()
