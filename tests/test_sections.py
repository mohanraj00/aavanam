"""The storage section rules that the runner cannot express: declarations and two opens of one store."""
import tempfile
import unittest
from pathlib import Path

from support import Store

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

    def test_a_later_open_keeps_the_section(self):  # PUB-SEC-2
        store = Store(self.path)
        store.section(SERVING, 'serving')
        store.close()
        store = Store(self.path)
        self.assertEqual(store.section_of(SERVING), 'serving')
        store.close()


if __name__ == '__main__':
    unittest.main()
