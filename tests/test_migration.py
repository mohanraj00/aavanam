"""The migration rules that the runner cannot express (document 25): the store file of release 0.6.0, a read
context of that release after a later put, and a store file with a form that this implementation does not know."""
import hashlib
import json
import shutil
import sqlite3
import tempfile
import unittest
from pathlib import Path

from support import BASE, Store, payload, put_ref
from aavanam.store import UnsupportedStore

RELEASE = Path(__file__).resolve().parent.parent / 'fixtures' / 'releases' / '0.6.0'


def codes(result):
    return result['status'], [r['code'] for r in result['reasons']]


class ReleaseFile(unittest.TestCase):
    """The writer store file that the reference store of release 0.6.0 wrote (fixtures/README.md)."""

    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.path = str(Path(self.folder.name) / 'writer.sqlite')
        shutil.copyfile(RELEASE / 'reference' / 'writer.sqlite', self.path)
        self.contexts = json.loads((RELEASE / 'reference' / 'contexts.json').read_text())['writer']

    def open(self):
        store = Store(self.path)
        self.addCleanup(store.close)
        return store

    def tables(self):
        db = sqlite3.connect(self.path)
        try:
            return {name for (name,) in db.execute("SELECT name FROM sqlite_master WHERE type = 'table'")}
        finally:
            db.close()

    def members(self, store, context, kind):
        result = store.call('list_items', {'context': context, 'kinds': [kind]})
        self.assertEqual(codes(result), ('ok', []))
        return [item['ref'] for item in result['items']]

    def test_two_opens_of_a_release_file_add_the_new_table_one_time(self):
        """MIG-VER-1, MIG-STG-1, MIG-STG-2: the file has no table for the removed objects of a read context
        (REL-RMV-13). The open adds it, and a second open of the same file reads the same content."""
        self.assertNotIn('context_removed', self.tables())
        first, second = self.open(), self.open()
        self.assertIn('context_removed', self.tables())
        for store in (first, second):
            removals = store.call('removals', {})
            self.assertEqual(codes(removals), ('ok', []))
            self.assertEqual([(r['sequence'], r['kind'], r['reason']) for r in removals['items']],
                             [(1, 'excerpt', 'sample removal request 1')])
            self.assertEqual(len(self.members(store, self.contexts['c1'], 'artifact')), 1)

    def test_an_export_in_a_context_of_the_release_gives_the_earlier_result_after_a_put(self):
        """MIG-VER-4: a read context of release 0.6.0 has no record of its removed objects. After a `put` adds
        the removed excerpt again, an export in that context gives the result that release 0.6.0 gave: the
        bundle has the excerpt. A read context that this release opens gives `not_in_context` (REL-RMV-13)."""
        store = self.open()
        old = self.contexts['ce']
        snapshot = self.members(store, old, 'snapshot')
        reading = self.members(store, old, 'reading')
        self.assertEqual((len(snapshot), len(reading)), (1, 1))
        new = store.call('open_context', {'roots': snapshot})
        self.assertEqual(codes(new), ('ok', []))
        removed = new['items'][0]['removed']
        self.assertEqual(len(removed), 1)
        part, data = payload('check the seal.')
        again = put_ref(store, {'descriptor_profile': BASE, 'kind': 'excerpt', 'parent': reading[0],
                                'ranges': [{'start': 6, 'end': 21}], 'payload': part}, data)
        self.assertEqual([again], removed)
        earlier = store.call('export', {'context': old, 'roots': snapshot})
        self.assertEqual(codes(earlier), ('ok', []))
        self.assertIn(again, [item['ref'] for item in earlier['items']])
        later = store.call('export', {'context': new['context'], 'roots': snapshot})
        self.assertEqual(codes(later), ('not_found', ['not_in_context']))
        self.assertEqual(codes(store.call('get', {'context': old, 'refs': [again]})), ('not_found', ['not_in_context']))

    def test_a_file_with_a_form_version_does_not_open_and_does_not_change(self):
        """MIG-STG-3, MIG-MAJ-3: a store file with a version number has a form that this implementation does not
        know. The open fails with the reason code `unsupported_profile`, and the file does not change."""
        db = sqlite3.connect(self.path)
        db.execute('PRAGMA user_version = 2')
        db.close()
        before = hashlib.sha256(Path(self.path).read_bytes()).hexdigest()
        with self.assertRaises(UnsupportedStore) as raised:
            Store(self.path)
        self.assertEqual(raised.exception.reason, 'unsupported_profile')
        self.assertEqual(hashlib.sha256(Path(self.path).read_bytes()).hexdigest(), before)
        self.assertNotIn('context_removed', self.tables())
        self.assertFalse(list(Path(self.folder.name).glob('writer.sqlite-*')))

    def test_the_release_store_has_each_stored_form(self):
        """The release store has the definition and each stored form: the definition names the commit of
        release 0.6.0."""
        definition = json.loads((RELEASE / 'release.json').read_text())
        self.assertEqual(definition['commit'], '818ecee28bc4a262f81f6a730ef443b8a2f7fd78')
        for name in ('reference/writer.sqlite', 'reference/device.sqlite', 'reference/output.sqlite', 'reference/contexts.json',
                     'backend/postgresql.sql', 'backend/contexts.json', 'device/device.sqlite', 'device/output.sqlite',
                     'device/contexts.json', 'bundles/x1.json', 'bundles/x2.json'):
            self.assertTrue((RELEASE / name).is_file(), name)


if __name__ == '__main__':
    unittest.main()
