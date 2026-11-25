"""The layer rules of document 23 that the vectors do not express: the example of issue 44 with 200 base units,
each operation with a reference of a different layer, a store file of an older version and a section declaration."""
import json
import sqlite3
import tempfile
import unittest
from pathlib import Path

from support import BASE, Store, payload, put, put_ref

COLLECTION = 'sample-parts'


def desc(kind, **fields):
    return {'descriptor_profile': BASE, 'kind': kind, **fields}


def unit(store, name, text):
    """A unit with one note and one table. Return the unit, the BindingSet, the table and the note."""
    obj, data = payload(text)
    note = put_ref(store, desc('artifact', artifact_type='sample/note/v1', payload=obj), data)
    table, rows = payload(json.dumps({'columns': ['part'], 'key': ['part'], 'rows': [[name]]}))
    schema, fields = payload('{"fields": ["part"]}')
    schema = put_ref(store, desc('schema', schema_profile='sample/schema/v1', payload=schema), fields)
    table = put_ref(store, desc('data-resource', data_profile='aavanam/table/v1', schema=schema, payload=table), rows)
    binding = put_ref(store, desc('binding-set', bindings={'note': note, 'parts': table}, parameters={'text': text}))
    return put_ref(store, desc('knowledge-unit', logical_id=name, binding_set=binding, narratives=[note])), binding, table, note


def refs_in(value):
    """Each reference in a request value."""
    if isinstance(value, str) and value.startswith('sha256:'):
        yield value
    for v in value.values() if isinstance(value, dict) else value if isinstance(value, list) else ():
        yield from refs_in(v)


def layer(store, scope, units=(), **fields):
    return put_ref(store, desc('layer', base=COLLECTION, scope=scope, units=sorted(units), **fields))


def opened(store, base, ref=None, scope=None):
    request = {'roots': [base], **({'layer': {'ref': ref, 'scope': scope}} if ref else {})}
    result = store.call('open_context', request)
    assert result['status'] == 'ok', result
    return result['context']


def units_of(store, context):
    result = store.call('list_items', {'context': context, 'kinds': ['knowledge-unit']})
    assert result['status'] == 'ok', result
    return [i['ref'] for i in result['items']]


class LayerTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):  # one store for the class: the tests only add objects
        cls.store = Store(debug=True)
        cls.base_units = [unit(cls.store, f'part-{n:03}', f'Base text of part {n}.')[0] for n in range(200)]
        result = cls.store.call('create_snapshot', {'collection': COLLECTION, 'units': cls.base_units})
        cls.base = result['items'][0]['ref']
        cls.changed = unit(cls.store, 'part-007', 'Text of part 7 for the first scope.')
        cls.added = [unit(cls.store, f'extra-{n}', f'Extra text {n} of the first scope.') for n in range(2)]
        cls.first = layer(cls.store, 'scope-1', [cls.changed[0], cls.added[0][0], cls.added[1][0]])
        cls.second = layer(cls.store, 'scope-2')

    @classmethod
    def tearDownClass(cls):
        cls.store.close()

    def test_the_example_of_the_issue_gives_202_and_200_units(self):  # LYR-PRE-1, LYR-PRE-2, LYR-ISO-1
        first = units_of(self.store, opened(self.store, self.base, self.first, 'scope-1'))
        self.assertEqual(len(first), 202)
        self.assertEqual(set(first), set(self.base_units) - {self.base_units[7]} | {self.changed[0], self.added[0][0], self.added[1][0]})
        for context in (opened(self.store, self.base, self.second, 'scope-2'), opened(self.store, self.base)):
            self.assertEqual(sorted(units_of(self.store, context)), sorted(self.base_units))

    def test_each_operation_refuses_a_reference_of_a_different_layer(self):  # LYR-ISO-1, LYR-ISO-2
        context = opened(self.store, self.base, self.second, 'scope-2')
        foreign_unit, binding, table, note = self.added[0]
        foreign = {foreign_unit, binding, table, note, self.changed[0], self.first}
        selector = {'selector_profile': 'aavanam/bytes/v1', 'start': 0, 'end': 1}
        lock = {'consumer': 'sample-consumer', 'completeness': 'sample/declared/v1'}
        calls = [('get', {'refs': [foreign_unit, self.first]}), ('read', {'ref': note}), ('query_data', {'resources': [table]}),
                 ('evidence', {'subject': note}), ('observations', {'subject': foreign_unit}), ('coverage', {'subject': foreign_unit}),
                 ('compare', {'before': note, 'after': note, 'selector': selector}), ('impact', {'changed': [table]}),
                 ('context_bundle', {'roots': [foreign_unit]}), ('render', {'roots': [foreign_unit]}),
                 ('execution_inputs', {'binding_set': binding, **lock}), ('build_inputs', {'inputs': [foreign_unit], **lock}),
                 ('export', {'roots': [self.base, self.first]})]
        before = self.store.fingerprint()
        for operation, request in calls:
            result = self.store.call(operation, {'context': context, **request})
            codes = {r['code'] for r in result['reasons']} | {r['code'] for i in result['items'] for r in i['reasons']}
            self.assertEqual(codes - {'items_not_ok'}, {'unknown_ref'}, operation)
            self.assertNotIn(result['status'], ('ok', 'partial'), operation)
            self.assertIsNone(result.get('lock'), operation)
            self.assertIsNone(result.get('bundle'), operation)
            named = {r for x in [result, *result['items']] for reason in x['reasons'] for r in reason['refs']} | \
                {i['ref'] for i in result['items'] if 'ref' in i}
            self.assertLessEqual(named, set(refs_in(request)), operation)  # a reason names only a reference of the request
        self.assertEqual(self.store.fingerprint(), before)
        # No listing or search of the second scope gives an object of the first layer.
        text = self.store.call('search_text', {'context': context, 'query': 'scope'})
        self.assertEqual((text['status'], text['items']), ('ok', []))
        listed = self.store.call('list_items', {'context': context})
        self.assertFalse(foreign & {i['ref'] for i in listed['items']})
        exported = self.store.call('export', {'context': context, 'roots': [self.base, self.second]})
        self.assertFalse(foreign & {i['ref'] for i in exported['items']})
        self.assertFalse(any('first scope' in t for t in exported['bundle']['descriptors']))

    def test_a_context_does_not_change_after_a_new_layer_of_the_scope(self):  # LYR-OPN-3, CTX-OPN-2
        context = opened(self.store, self.base, self.second, 'scope-2')
        later = layer(self.store, 'scope-2', [unit(self.store, 'extra-9', 'Extra text of the second scope.')[0]])
        self.assertEqual(len(units_of(self.store, context)), 200)
        self.assertEqual(len(units_of(self.store, opened(self.store, self.base, later, 'scope-2'))), 201)

    def test_a_lock_request_has_no_view_field(self):  # LYR-LCK-1
        context = opened(self.store, self.base, self.first, 'scope-1')
        request = {'context': context, 'inputs': [self.changed[0]], 'consumer': 'sample-consumer', 'completeness': 'sample/declared/v1'}
        self.assertEqual(self.store.call('build_inputs', {**request, 'view': [self.base]})['status'], 'failed')
        lock = self.store.descriptor(self.store.call('build_inputs', request)['lock'])
        self.assertEqual(lock['view'], sorted([self.base, self.first]))
        plain = self.store.descriptor(self.store.call('build_inputs', {**request, 'inputs': [self.base_units[0]], 'context': opened(self.store, self.base)})['lock'])
        self.assertNotIn('view', plain)


class ProfileTest(unittest.TestCase):
    def test_a_store_that_does_not_declare_the_profile_is_a_base_store(self):  # LYR-PRF-1, KND-REG-2
        store = Store(debug=True, profiles=())
        base = store.call('create_snapshot', {'collection': COLLECTION})['items'][0]['ref']
        binding = put_ref(store, desc('binding-set', parameters={'item': 'one'}))

        def refusal(result):
            return result['status'], [r['code'] for r in result['reasons']]

        before = store.fingerprint()
        self.assertEqual(refusal(put(store, desc('layer', base=COLLECTION, scope='scope-1'))), ('unsupported', ['unknown_kind']))
        lock = desc('build-lock', inputs=[binding], consumer='sample-consumer', completeness='sample/declared/v1')
        self.assertEqual(refusal(put(store, {**lock, 'view': [base]})), ('failed', ['unknown_field']))
        request = {'roots': [base], 'layer': {'ref': base, 'scope': 'scope-1'}}
        self.assertEqual(refusal(store.call('open_context', request)), ('failed', ['unknown_field']))
        self.assertEqual(store.fingerprint(), before)
        self.assertEqual(put(store, lock)['status'], 'ok')
        self.assertEqual(Store(debug=True).profiles, ('aavanam/layer/v1',))
        with self.assertRaises(ValueError):
            Store(profiles=('sample/other/v1',))

    def test_a_device_store_and_its_output_store_declare_the_same_profiles(self):  # LYR-PRF-1, BUN-IMP-5
        for profiles in ((), ('aavanam/layer/v1',)):
            device = Store(role='device', profiles=profiles)
            self.assertEqual(device.output.profiles, profiles)
            device.close()


class LayerFileTest(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.path = str(Path(self.folder.name) / 'store.db')

    def tearDown(self):
        self.folder.cleanup()

    def test_a_context_of_an_older_store_file_has_no_layer(self):  # LYR-OPN-3
        db = sqlite3.connect(self.path)
        db.execute('CREATE TABLE contexts (id TEXT PRIMARY KEY, roots TEXT NOT NULL, capabilities TEXT NOT NULL)')
        db.execute("INSERT INTO contexts VALUES ('ctx:old', '[]', '[]')")
        db.commit()
        db.close()
        store = Store(self.path)
        other = put_ref(store, desc('binding-set', parameters={'item': 'other'}))
        result = store.call('get', {'context': 'ctx:old', 'refs': [other]})
        self.assertEqual([r['code'] for r in result['items'][0]['reasons']], ['not_in_context'])  # CTX-OPN-4: no layer
        base = store.call('create_snapshot', {'collection': COLLECTION})['items'][0]['ref']
        context = opened(store, base, layer(store, 'scope-1'), 'scope-1')
        result = store.call('get', {'context': context, 'refs': [base, other]})
        self.assertEqual([[r['code'] for r in i['reasons']] for i in result['items']], [[], ['unknown_ref']])  # LYR-ISO-2
        store.close()
        # The table `contexts` keeps its three columns, so an earlier version of the store reads the file.
        db = sqlite3.connect(self.path)
        self.assertEqual([c for (_, c, *_) in db.execute('PRAGMA table_info(contexts)')], ['id', 'roots', 'capabilities'])
        db.close()

    def test_a_section_declaration_for_the_base_of_a_layer_is_refused(self):  # LYR-DEF-5, PUB-SEC-2
        store = Store(self.path)
        layer(store, 'scope-1')
        store.close()
        store = Store(self.path)
        with self.assertRaises(ValueError):
            store.section(COLLECTION, 'serving')
        store.section('sample-other', 'serving')
        store.close()


if __name__ == '__main__':
    unittest.main()
