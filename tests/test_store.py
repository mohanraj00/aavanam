"""Store behavior that the vectors do not cover: atomic writes, storage errors and a file store."""
from __future__ import annotations

import os
import sqlite3
import tempfile
import unittest
from pathlib import Path

from support import BASE, Store, canonical, parse, payload, put, put_ref, revision


def coverage_store():
    """A store with a source, a coverage observation of it in a snapshot, and a read context of the snapshot."""
    store = Store()
    obj, data = payload('sample page')
    source = put_ref(store, {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}, data)
    values = {'unit': 'pages', 'covered': 1, 'total': 2, 'omissions': [{'reason': 'unreadable page'}]}
    observation = put_ref(store, {'descriptor_profile': BASE, 'kind': 'observation', 'subjects': [source],
                                  'observation_type': 'aavanam/coverage/v1', 'scope': {}, 'values': values})
    snap = store.call('create_snapshot', {'collection': 'sample-collection', 'observations': [observation]})['items'][0]['ref']
    context = store.call('open_context', {'roots': [snap]})['context']
    return store, context, source


class StoreTest(unittest.TestCase):
    def test_storage_error_changes_nothing(self):
        store = Store()
        store.db.execute("CREATE TRIGGER no_bytes BEFORE INSERT ON blobs BEGIN SELECT RAISE(ABORT, 'full'); END")
        before = store.fingerprint()
        obj, data = payload('sample bytes')
        result = put(store, {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}, data)
        self.assertEqual((result['status'], [r['code'] for r in result['reasons']]), ('failed', ['storage_error']))  # STS-MAL-1
        self.assertEqual([(i['status'], [r['code'] for r in i['reasons']]) for i in result['items']],
                         [('failed', ['batch_aborted'])])  # STS-ENV-5
        self.assertNotIn('full', str(result))  # no storage error message crosses the API
        self.assertEqual(store.fingerprint(), before)  # STS-MAL-2: the descriptor and its bytes, or none of them
        self.assertEqual(store.db.execute('SELECT count(*) FROM objects').fetchone()[0], 0)

    def test_failed_rollback_does_not_cross_the_api(self):
        store = Store()
        store.db.execute("CREATE TRIGGER no_bytes BEFORE INSERT ON blobs BEGIN SELECT RAISE(ABORT, 'full'); END")
        deny = sqlite3.SQLITE_DENY
        store.db.set_authorizer(lambda action, arg, *_: deny if action == sqlite3.SQLITE_TRANSACTION and arg == 'ROLLBACK'
                                else sqlite3.SQLITE_OK)
        obj, data = payload('sample bytes')
        result = put(store, {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}, data)
        self.assertEqual((result['status'], [r['code'] for r in result['reasons']]), ('failed', ['storage_error']))  # STS-MAL-1
        store.db.set_authorizer(None)
        later = store.call('get', {'context': 'ctx:none', 'refs': [obj['digest']]})
        self.assertEqual([r['code'] for r in later['reasons']], ['storage_error'])  # STS-MAL-2: no partial write is visible

    def test_operation_that_is_not_a_string(self):
        for operation in ([], {}, None):
            result = Store().call(operation, {})
            self.assertEqual((result['status'], [r['code'] for r in result['reasons']]), ('unsupported', ['unsupported_operation']))

    def test_snapshot_value_that_is_not_ijson(self):
        store = Store()
        before = store.fingerprint()
        result = store.call('create_snapshot', {'collection': 'sample-collection', 'ext': {'x': 2 ** 53 + 1}})
        self.assertEqual((result['status'], [r['code'] for r in result['reasons']]), ('failed', ['field_type']))  # KND-SCH-4
        self.assertEqual(store.fingerprint(), before)

    def test_import_root_with_a_failed_check(self):
        store = Store()
        obj, data = payload('sample page')
        source = {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}
        members = {'descriptor_profile': BASE, 'kind': 'relation-set', 'members': [revision(source)]}
        bundle = {'format': 'aavanam-bundle/v1', 'roots': [revision(members)],
                  'descriptors': [canonical(source), canonical(members)], 'payloads': data}
        result = store.call('import', {'bundle': bundle})
        self.assertEqual((result['status'], [r['code'] for r in result['reasons']], result['items']),
                         ('unsupported', ['unsupported_target_kind'], []))  # BUN-IMP-2: the root is present

    def test_deep_value_is_readable(self):
        store = Store()
        obj, data = payload('sample page')
        source = put_ref(store, {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}, data)
        deep = '[' * 600 + ']' * 600
        text = '{"descriptor_profile": "%s", "kind": "check", "subject": "%s", "check_profile": "sample/check/v1", ' \
               '"outcome": "pass", "scope": %s, "origin": "example/reviewer"}' % (BASE, source, deep)
        result = store.call('put', {'descriptors': [text], 'payloads': {}})
        self.assertEqual(result['status'], 'ok', result)
        members = put_ref(store, {'descriptor_profile': BASE, 'kind': 'relation-set', 'members': [result['items'][0]['ref']]})
        snap = store.call('create_snapshot', {'collection': 'sample-collection', 'relation_sets': [members]})['items'][0]['ref']
        context = store.call('open_context', {'roots': [snap]})['context']
        result = store.call('render', {'context': context, 'roots': [source]})
        self.assertEqual(result['status'], 'ok', result)
        self.assertEqual(canonical(next(i['scope'] for i in result['items'] if i['entry'] == 'check')), deep)

    def test_failed_lock_writes_nothing(self):
        store, context, source = coverage_store()
        before = store.fingerprint()
        store.closure = lambda roots: (_ for _ in ()).throw(sqlite3.OperationalError('denied'))
        result = store.call('build_inputs', {'context': context, 'inputs': [source], 'consumer': 'example/consumer',
                                             'completeness': 'aavanam/closure-only/v1'})
        self.assertEqual((result['status'], [r['code'] for r in result['reasons']]), ('failed', ['storage_error']))
        self.assertEqual(store.fingerprint(), before)  # STS-ENV-4: no lock

    def test_result_cannot_change_the_store(self):
        store, context, source = coverage_store()
        first = store.call('coverage', {'context': context, 'subject': source})
        self.assertEqual(first['status'], 'ok', first)
        first['items'][0]['omissions'].clear()
        second = store.call('coverage', {'context': context, 'subject': source})
        self.assertEqual(second['items'][0]['omissions'], [{'reason': 'unreadable page'}])  # CTX-OPN-2, OBS-COV-4

    def test_where_value_that_is_not_ijson(self):
        store, context, source = coverage_store()
        result = store.call('query_data', {'context': context, 'resources': [source], 'where': {'part': float('nan')}})
        self.assertEqual((result['status'], [r['code'] for r in result['reasons']], result['items']),
                         ('failed', ['field_type'], []))  # QRY-DAT-1

    def test_lock_input_counts(self):
        store, context, source = coverage_store()
        before = store.fingerprint()
        for inputs in ([source, source], []):
            result = store.call('build_inputs', {'context': context, 'inputs': inputs, 'consumer': 'example/consumer',
                                                 'completeness': 'aavanam/closure-only/v1'})
            self.assertEqual([r['code'] for r in result['reasons']], ['field_count'])  # LCK-BLD-1, KND-SCH-5
        result = store.call('execution_inputs', {'context': context, 'binding_set': source, 'context_inputs': [source, source],
                                                 'consumer': 'example/consumer', 'completeness': 'aavanam/closure-only/v1'})
        self.assertEqual([r['code'] for r in result['reasons']], ['field_count'])  # LCK-EXE-1
        result = store.call('build_inputs', {'context': context, 'inputs': [source], 'consumer': '\ufdd0',
                                             'completeness': 'aavanam/closure-only/v1'})
        self.assertEqual([r['code'] for r in result['reasons']], ['field_type'])  # KND-SCH-4
        self.assertEqual(store.fingerprint(), before)

    def test_store_roles(self):
        for role, device in (('output', None), ('writer', Store()), ('reader', None)):
            with self.assertRaises(ValueError):
                Store(role=role, device=device)  # BUN-IMP-5
        device = Store(role='device')
        self.assertEqual((device.output.role, device.output.device), ('output', device))
        device.close()
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'store.sqlite')
            with self.assertRaises(ValueError):
                Store(path, role='device', output_path=path)  # BUN-IMP-7: separate databases
            link = str(Path(folder) / 'link.sqlite')
            Store(path).close()
            os.link(path, link)
            with self.assertRaises(ValueError):
                Store(path, role='device', output_path=link)  # one file with two paths
            device = Store(path, role='device')
            with self.assertRaises(ValueError):
                Store(path, role='output', device=device)  # only the device store creates its output store
            device.close()

    def test_second_connection_cannot_import_into_a_device_store(self):
        writer, context, _ = coverage_store()
        snapshot = next(r for (r,) in writer.db.execute("SELECT ref FROM objects WHERE kind = 'snapshot'"))
        bundle = writer.call('export', {'context': context, 'roots': [snapshot]})['bundle']
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'device.sqlite')
            first, second = Store(path, role='device'), Store(path, role='device')
            second.has_objects = lambda: False  # the second connection checked before the first import
            self.assertEqual(first.call('import', {'bundle': bundle})['status'], 'ok')
            before = first.fingerprint()
            result = second.call('import', {'bundle': bundle})
            self.assertEqual((result['status'], [r['code'] for r in result['reasons']]), ('unsupported', ['read_only_store']))
            self.assertEqual(first.fingerprint(), before)  # BUN-IMP-6
            first.close()
            second.close()

    def test_output_store_follows_device_references(self):
        writer, context, source = coverage_store()
        snapshot = next(r for (r,) in writer.db.execute("SELECT ref FROM objects WHERE kind = 'snapshot'"))
        bundle = writer.call('export', {'context': context, 'roots': [snapshot]})['bundle']
        device = Store(role='device')
        self.assertEqual(device.call('import', {'bundle': bundle})['status'], 'ok')
        output = device.output
        local = output.call('open_context', {'roots': [snapshot]})['context']
        impact = output.call('impact', {'context': local, 'changed': [source]})
        self.assertEqual(impact['status'], 'ok', impact)
        self.assertEqual(len(impact['items']), 2)  # the observation and the snapshot (BUN-IMP-8, CHG-IMP-4)
        device.close()

    def test_m1_database_gets_context_capabilities(self):
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'store.sqlite')
            db = sqlite3.connect(path)
            db.execute('CREATE TABLE contexts (id TEXT PRIMARY KEY, roots TEXT NOT NULL)')
            db.execute("INSERT INTO contexts VALUES ('ctx:old', '[]')")
            db.commit()
            db.close()
            store = Store(path)
            table = ['aavanam/data/v1', 'aavanam/export/v1', 'aavanam/lexical/v1', 'aavanam/locks/v1', 'aavanam/text/v1']
            self.assertEqual(store.context_of({'context': 'ctx:old'})[2], table)  # each capability (CTX-PRJ-1, CTX-PRJ-2)
            store.close()

    def test_exported_number_round_trip(self):
        store = Store()
        snap = store.call('create_snapshot', {'collection': 'sample-collection', 'ext': {'n': 1e20}})['items'][0]['ref']
        context = store.call('open_context', {'roots': [snap]})['context']
        bundle = store.call('export', {'context': context, 'roots': [snap]})['bundle']
        self.assertEqual(Store().call('import', {'bundle': bundle})['status'], 'ok')  # BUN-IMP-3

    def test_number_that_its_canonical_form_does_not_keep(self):
        store = Store()
        before = store.fingerprint()
        for number in ('9007199254740993', '9007199254740993e0', '9007199254740993.0'):
            text = '{"descriptor_profile": "%s", "kind": "binding-set", "parameters": {"n": %s}}' % (BASE, number)
            result = store.call('put', {'descriptors': [text], 'payloads': {}})
            self.assertEqual([r['code'] for r in result['reasons']], ['non_ijson_value'], number)  # STS-MAL-3 step 1
        result = store.call('create_snapshot', {'collection': 'sample-collection', 'ext': {'n': 2 ** 68}})
        self.assertEqual([r['code'] for r in result['reasons']], ['field_type'])  # KND-SCH-4
        self.assertEqual(store.fingerprint(), before)

    def test_number_with_an_extreme_exponent(self):
        for number, code in (('0e9999999999999999999', 'ok'), ('1e-9999999999999999999', 'failed'), ('1e9999999999999999999', 'failed'),
                             ('1' * 4301, 'failed'), ('1' * 4301 + 'e0', 'failed')):
            text = '{"descriptor_profile": "%s", "kind": "binding-set", "parameters": %s}' % (BASE, number)
            result = Store().call('put', {'descriptors': [text], 'payloads': {}})
            self.assertEqual([r['code'] for r in result['reasons']], [] if code == 'ok' else ['non_ijson_value'], number[:24])  # STS-MAL-3

    def test_integer_in_exponent_form_round_trip(self):
        store = Store()
        obj, data = payload('sample page')
        source = put_ref(store, {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}, data)
        reading = put_ref(store, {'descriptor_profile': BASE, 'kind': 'reading', 'source': source, 'producer': 'example/reader',
                                  'coverage_profile': 'sample/coverage/v1', 'payload': obj}, data)
        selector = {'selector_profile': 'aavanam/bytes/v1', 'start': 10 ** 21, 'end': 10 ** 21}
        derived = put_ref(store, {'descriptor_profile': BASE, 'kind': 'derivation', 'operands': [reading],
                                  'operand_selectors': [selector], 'method': 'sample/copy/v1', 'result': {}})
        self.assertEqual(store.descriptor(derived)['operand_selectors'], [selector])
        members = put_ref(store, {'descriptor_profile': BASE, 'kind': 'relation-set', 'members': [derived]})
        snap = store.call('create_snapshot', {'collection': 'sample-collection', 'relation_sets': [members]})['items'][0]['ref']
        context = store.call('open_context', {'roots': [snap]})['context']
        bundle = store.call('export', {'context': context, 'roots': [snap]})['bundle']
        self.assertEqual(Store().call('import', {'bundle': bundle})['status'], 'ok')  # BUN-IMP-3

    def test_integral_number_in_exponent_form(self):
        for number in ('1e23', '100000000000000000000000'):
            text = '{"descriptor_profile": "%s", "kind": "binding-set", "parameters": {"n": %s}}' % (BASE, number)
            result = Store().call('put', {'descriptors': [text], 'payloads': {}})
            self.assertEqual(result['status'], 'ok', number)  # STS-MAL-3: the notation has no effect
        self.assertEqual(parse('1e23'), 10 ** 23)  # DAT-TBL-2: no conversion of the value

    def test_import_root_of_an_unknown_kind(self):
        root = {'descriptor_profile': 'ext:example.com/test/v1', 'kind': 'ext:example.com/test/v1#sample'}
        bundle = {'format': 'aavanam-bundle/v1', 'roots': [revision(root)], 'descriptors': [canonical(root)], 'payloads': {}}
        result = Store().call('import', {'bundle': bundle})
        self.assertEqual((result['status'], [r['code'] for r in result['reasons']]), ('unsupported', ['unknown_kind']))  # BUN-IMP-2

    def test_import_root_that_is_not_the_normal_form(self):
        root = {'descriptor_profile': BASE, 'kind': 'snapshot', 'collection': 'sample-collection', 'units': []}
        bundle = {'format': 'aavanam-bundle/v1', 'roots': [revision(root)], 'descriptors': [canonical(root)], 'payloads': {}}
        result = Store().call('import', {'bundle': bundle})
        self.assertEqual((result['status'], [r['code'] for r in result['reasons']]), ('failed', ['bundle_incomplete']))  # BUN-IMP-2

    def test_request_that_is_not_an_object(self):
        result = Store().call('get', None)
        self.assertEqual((result['status'], [r['code'] for r in result['reasons']]), ('failed', ['field_type']))

    def test_file_store_keeps_its_content(self):
        with tempfile.TemporaryDirectory() as folder:
            path = str(Path(folder) / 'store.sqlite')
            first = Store(path)
            obj, data = payload('kept bytes')
            ref = put_ref(first, {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}, data)
            fingerprint = first.fingerprint()
            first.close()
            second = Store(path)
            self.assertEqual(second.fingerprint(), fingerprint)
            self.assertEqual(second.descriptor(ref)['payload'], obj)
            second.close()


if __name__ == '__main__':
    unittest.main()
