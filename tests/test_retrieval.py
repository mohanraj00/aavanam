"""The retrieval index rules of document 24 that the vectors do not express: the example of issue 45, a store with
no profile, a layered context, a store file that opens again, and the order of items with no logical ID."""
import base64
import hashlib
import sqlite3
import tempfile
import unittest
from pathlib import Path

from support import BASE, Store, payload, put, put_ref

COLLECTION = 'sample-notes'
INDEX = 'aavanam/semantic-index/v1'
METHOD = 'aavanam/hashed-terms/v1'


def desc(kind, **fields):
    return {'descriptor_profile': BASE, 'kind': kind, **fields}


def unit(store, text, name=None, data=None):
    """A unit with one note. `data` gives the bytes of a note that is not UTF-8 text."""
    if data is None:
        obj, bytes_of = payload(text)
    else:
        key = 'sha256:' + hashlib.sha256(data).hexdigest()
        obj, bytes_of = {'digest': key, 'media_type': 'text/plain', 'size': len(data)}, {key: base64.b64encode(data).decode()}
    note = put_ref(store, desc('artifact', artifact_type='sample/note/v1', payload=obj), bytes_of)
    binding = put_ref(store, desc('binding-set', parameters={'text': text}))
    return put_ref(store, desc('knowledge-unit', binding_set=binding, narratives=[note], **({'logical_id': name} if name else {})))


def snapshot(store, units, collection=COLLECTION):
    result = store.call('create_snapshot', {'collection': collection, 'units': sorted(units)})
    assert result['status'] == 'ok', result
    return result['items'][0]['ref']


def index(store, snap, dimensions=65536):
    return put_ref(store, desc('retrieval-index', snapshot=snap, method=METHOD, parameters={'dimensions': dimensions}))


def opened(store, *roots, **fields):
    result = store.call('open_context', {'roots': list(roots), **fields})
    assert result['status'] == 'ok', result
    return result['context']


def codes(result):
    return result['status'], [r['code'] for r in result['reasons']]


class ExampleTest(unittest.TestCase):
    def test_the_example_of_the_issue_gives_five_items_of_one_snapshot(self):  # IDX-SRC-3, IDX-SRC-6
        store = Store(debug=True)
        units = [unit(store, f'Torque note {n}: tighten bolt {n} of the frame.', f'note-{n:02}') for n in range(30)]
        snap = snapshot(store, units)
        idx = index(store, snap)
        context = opened(store, snap, idx)
        before = store.fingerprint()
        result = store.call('semantic_search', {'context': context, 'index': idx, 'query': 'tighten bolt 7', 'limit': 5})
        self.assertEqual(codes(result), ('partial', ['bounds_reached']))  # 30 items match, and the limit is a declared bound
        self.assertEqual([i['logical_id'] for i in result['items']], ['note-07', 'note-00', 'note-01', 'note-02', 'note-03'])
        self.assertEqual([i['score'] for i in result['items']], [3, 2, 2, 2, 2])
        for i in result['items']:
            self.assertEqual((i['snapshot'], i['index'], i['kind']), (snap, idx, 'knowledge-unit'))
            self.assertEqual(store.descriptor(i['ref'])['logical_id'], i['logical_id'])  # the revision of the item
        full = store.call('semantic_search', {'context': context, 'index': idx, 'query': 'tighten bolt 7'})
        self.assertEqual((codes(full), len(full['items'])), (('ok', []), 30))
        self.assertEqual(full['items'][:5], result['items'])
        self.assertEqual(store.fingerprint(), before)  # IDX-SRC-7: a search does not change the store
        store.close()

    def test_items_with_no_logical_id_come_after_the_others_in_one_score(self):  # IDX-SRC-6
        store = Store(debug=True)
        named = unit(store, 'Clean the lens.', 'lens')
        others = [unit(store, 'Clean the mirror.'), unit(store, 'Clean the prism.')]
        snap = snapshot(store, [named, *others])
        idx = index(store, snap)
        result = store.call('semantic_search', {'context': opened(store, idx), 'index': idx, 'query': 'clean'})
        self.assertEqual([(i['ref'], i['logical_id']) for i in result['items']], [(named, 'lens'), *((r, None) for r in sorted(others))])
        store.close()

    def test_a_word_comparison_is_a_byte_comparison(self):  # IDX-MTH-4, CRD-TXT-3: only A to Z have a folded form
        store = Store(debug=True)
        word = unit(store, 'bytes', 'cafe', data='Open the CAFé door.'.encode('utf-8'))
        raw = unit(store, 'raw', 'raw', data=b'\xff\xfe valve \xff')  # not UTF-8: the store reads bytes and does not decode them
        snap = snapshot(store, [word, raw])
        idx = index(store, snap)
        context = opened(store, idx)

        def found(query):
            return [i['logical_id'] for i in store.call('semantic_search', {'context': context, 'index': idx, 'query': query})['items']]

        self.assertEqual(found('café'), ['cafe'])
        self.assertEqual(found('CAFÉ'), [])  # the bytes of É are not the bytes of é
        self.assertEqual(found('VALVE'), ['raw'])
        store.close()

    def test_two_stale_indexes_give_two_reasons(self):  # IDX-LCK-2, IDX-STL-1
        store = Store(debug=True)
        first = unit(store, 'Check the belt.', 'belt')
        old, new = snapshot(store, [first]), snapshot(store, [first, unit(store, 'Check the chain.', 'chain')])
        stale = sorted([index(store, old), index(store, old, 8)])
        context = opened(store, new, *stale)
        request = {'context': context, 'binding_set': store.descriptor(first)['binding_set'], 'indexes': stale,
                   'consumer': 'sample-consumer', 'completeness': 'sample/declared/v1'}
        before = store.fingerprint()
        result = store.call('execution_inputs', request)
        self.assertEqual(codes(result), ('incompatible', ['index_stale', 'index_stale']))
        self.assertEqual([r['refs'] for r in result['reasons']], [[i, new] for i in stale])
        self.assertIsNone(result.get('lock'))
        self.assertEqual(store.fingerprint(), before)
        # A context of a different collection does not make the index stale.
        other = snapshot(store, [first], 'sample-other')
        ok = store.call('semantic_search', {'context': opened(store, other, stale[0]), 'index': stale[0], 'query': 'belt'})
        self.assertEqual((codes(ok), [i['logical_id'] for i in ok['items']]), (('ok', []), ['belt']))
        store.close()


class ProfileTest(unittest.TestCase):
    def test_a_store_that_does_not_declare_the_profile_keeps_the_reserved_response(self):  # IDX-PRF-1, RSV-LST-4, RSV-RSP-1
        for profiles in ((), ('aavanam/layer/v1',)):
            store = Store(debug=True, profiles=profiles)
            first = unit(store, 'Check the belt.', 'belt')
            snap = snapshot(store, [first])
            context = opened(store, snap)
            before = store.fingerprint()
            result = store.call('semantic_search', {'context': context, 'index': snap, 'query': 'belt'})
            self.assertEqual(codes(result), ('unsupported', ['reserved_profile']))
            self.assertEqual(result['reasons'][0]['capabilities'], [INDEX])
            self.assertEqual(codes(put(store, desc('retrieval-index', snapshot=snap, method=METHOD, parameters={'dimensions': 8}))),
                             ('unsupported', ['unknown_kind']))
            lock = desc('build-lock', inputs=[first], consumer='sample-consumer', completeness='sample/declared/v1')
            self.assertEqual(codes(put(store, {**lock, 'indexes': [snap]})), ('failed', ['unknown_field']))
            request = {'context': context, 'inputs': [first], 'consumer': 'sample-consumer', 'completeness': 'sample/declared/v1'}
            self.assertEqual(store.call('build_inputs', {**request, 'indexes': []})['status'], 'failed')
            self.assertEqual(codes(store.call('open_context', {'roots': [snap], 'capabilities': [INDEX]})), ('unsupported', ['unsupported_profile']))
            listed = store.call('list_items', {'context': context, 'kinds': ['retrieval-index']})  # DSC-LST-1: not a registered kind
            self.assertEqual((codes(listed), listed['items']), (('unsupported', ['unknown_kind']), []))
            self.assertEqual(store.fingerprint(), before)
            self.assertNotIn(INDEX, store.capabilities)
            store.close()

    def test_a_store_with_the_profile_lists_the_indexes_of_a_context(self):  # IDX-PRF-2, DSC-LST-1
        store = Store(debug=True, profiles=(INDEX,))
        snap = snapshot(store, [unit(store, 'Check the belt.', 'belt')])
        idx = index(store, snap)
        listed = store.call('list_items', {'context': opened(store, idx), 'kinds': ['retrieval-index']})
        self.assertEqual((codes(listed), [(i['ref'], i['kind']) for i in listed['items']]), (('ok', []), [(idx, 'retrieval-index')]))
        self.assertEqual(codes(put(store, desc('layer', base=COLLECTION, scope='scope-1'))), ('unsupported', ['unknown_kind']))
        store.close()

    def test_a_layered_context_has_no_index(self):  # IDX-DEF-4, LYR-ISO-2, LYR-OPN-1
        store = Store(debug=True)
        base_unit = unit(store, 'Check the belt.', 'belt')
        base = snapshot(store, [base_unit])
        idx = index(store, base)
        layer = put_ref(store, desc('layer', base=COLLECTION, scope='scope-1', units=[unit(store, 'Check the belt each day.', 'belt')]))
        request = {'layer': {'ref': layer, 'scope': 'scope-1'}}
        self.assertEqual(codes(store.call('open_context', {'roots': [base, idx], **request})), ('failed', ['field_count']))
        context = opened(store, base, **request)
        result = store.call('semantic_search', {'context': context, 'index': idx, 'query': 'belt'})
        self.assertEqual((codes(result), result['items']), (('not_found', ['unknown_ref']), []))
        lock = store.call('build_inputs', {'context': context, 'inputs': [base], 'indexes': [idx], 'consumer': 'sample-consumer',
                                           'completeness': 'sample/declared/v1'})
        self.assertEqual(codes(lock), ('not_found', ['unknown_ref']))
        store.close()


class FileTest(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.folder.cleanup()

    def filled(self, name):
        store = Store(str(Path(self.folder.name) / name))
        units = [unit(store, f'Seal note {n}: press seal {n} into the groove.', f'seal-{n}') for n in range(6)]
        snap = snapshot(store, units)
        return store, snap, index(store, snap)

    def test_a_rebuild_gives_the_same_items(self):  # IDX-DEF-2, IDX-SRC-7
        store, snap, idx = self.filled('one.db')
        request = {'index': idx, 'query': 'press seal 4 groove'}
        first = store.call('semantic_search', {'context': opened(store, snap, idx), **request})['items']
        self.assertEqual([i['score'] for i in first], [4, 3, 3, 3, 3, 3])
        store.close()
        # The file has no table of derived data: the index descriptor is the full record of the index.
        db = sqlite3.connect(str(Path(self.folder.name) / 'one.db'))
        tables = {name for (name,) in db.execute("SELECT name FROM sqlite_master WHERE type = 'table'")}
        db.close()
        self.assertFalse({t for t in tables if 'index' in t or 'term' in t or 'bucket' in t})
        again = Store(str(Path(self.folder.name) / 'one.db'))  # the same file, a new open
        self.assertEqual(again.call('semantic_search', {'context': opened(again, snap, idx), **request})['items'], first)
        again.close()
        other, snap_2, idx_2 = self.filled('two.db')  # a different file with the same snapshot
        self.assertEqual((snap_2, idx_2), (snap, idx))
        self.assertEqual(other.call('semantic_search', {'context': opened(other, idx), **request})['items'], first)
        other.close()


if __name__ == '__main__':
    unittest.main()
