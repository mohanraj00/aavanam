"""Self-tests of the conformance runner. Run: python3 -B -m unittest discover -s conformance"""
from __future__ import annotations

import base64
import copy
import hashlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from contextlib import redirect_stdout

import runner
from adapter import envelope
from null_adapter import NullAdapter

REF_A = 'sha256:' + 'a' * 64


def rev(value: dict) -> str:
    return runner.revision(value, runner.check_spec.schemas())


class Scripted:
    """Return the queued envelopes in order, and keep each request and the store of each call."""

    def __init__(self, *results, change_store=False, write_locks=True, write_created=True, lock_store=None):
        self.results = list(results)
        self.requests = []
        self.stores = []  # the store of each call that is not a lock probe
        self.change_store = change_store
        self.write_locks = write_locks
        self.write_created = write_created
        self.lock_store = lock_store  # the store that keeps a written lock; None follows BUN-IMP-7
        self.validate = None
        self.probe = None  # the lock that the runner reads next with a put (IDN-CAN-3)
        self.reset()

    def reset(self):
        self.state = {s: 0 for s in runner.check_spec.STORES}
        self.created = {s: set() for s in runner.check_spec.STORES}
        self.kept = {s: set() for s in runner.check_spec.STORES}  # the refs of ok put items and of written locks

    def call(self, operation, request, store='writer'):
        if self.probe and operation == 'put':
            lock, self.probe = self.probe, None
            self.state[store] += lock not in self.kept[store]
            self.kept[store].add(lock)
            return {'status': 'ok', 'reasons': [], 'operation': 'put', 'operation_profile': runner.BASE_PROFILE,
                    'context': None, 'items': [{'status': 'ok', 'reasons': [], 'ref': lock}]}
        self.requests.append(request)
        self.stores.append(store)
        if self.change_store:
            self.state[store] += 1
        result = self.results.pop(0)
        if isinstance(result, Exception):
            raise result
        if isinstance(result, dict) and isinstance(result.get('lock'), str):
            self.probe = result['lock']
            if self.write_locks:
                target = self.lock_store or ('output' if store == 'device' else store)
                self.state[target] += 1
                self.kept[target].add(result['lock'])
        if operation == 'put' and isinstance(result, dict) and result.get('status') == 'ok':
            self.kept[store].update(i['ref'] for i in result.get('items') or [] if isinstance(i, dict) and isinstance(i.get('ref'), str))
        item = (result.get('items') or [{}])[0] if isinstance(result, dict) else {}
        if self.validate and operation == 'publish' and isinstance(item, dict) and (
                item.get('status') == 'incompatible' or item.get('disposition') == 'published') and (
                not runner.ACTIVATION_REASONS & {r.get('code') for r in item.get('reasons') or [] if isinstance(r, dict)}):
            self.validate(request['candidate'], request['receipt'], request.get('activation'))  # step 4 of PUB-HED-3, after PUB-ACT-3
            self.state[store] += item.get('disposition') == 'published'
        if self.write_created and operation in runner.CREATE_OPERATIONS and isinstance(result, dict) and result.get('status') == 'ok':
            value = item.get('ref', item.get('candidate'))
            self.state[store] += value not in self.created[store]
            self.created[store].add(value)
        return result

    def fingerprint(self, store='writer'):
        return str(self.state[store])

    def accept(self, policy, validate):
        self.validate = validate

    def gate(self, collection):
        self.gates = getattr(self, 'gates', []) + [collection]

    def section(self, collection, section):
        pass


def put_step(entries, status='ok', reasons=(), items=None, **extra):
    expect = {'status': status, 'reasons': list(reasons), **extra}
    if items is not None:
        expect['items'] = items
    return {'operation': 'put', 'request': {'descriptors': entries}, 'expect': expect}


OK_ITEM = {'status': 'ok', 'reasons': []}
REASON_X = envelope('get', 'failed', ['storage_error'])['reasons'][0]
PAYLOADS = {'p': {'media_type': 'text/plain', 'text': 'abc'}}


class CompareTest(unittest.TestCase):
    def test_match(self):
        actual = envelope('put', 'failed', ['field_type'], [{'status': 'failed', 'reasons': envelope('put', 'failed', ['field_type'])['reasons']}])
        expect = {'status': 'failed', 'reasons': ['field_type'], 'items': [{'status': 'failed', 'reasons': ['field_type']}]}
        self.assertEqual(runner.compare(expect, actual, 'put'), [])

    def test_differences(self):
        actual = envelope('get', 'ok', [], [{'status': 'ok', 'reasons': []}])
        expect = {'status': 'failed', 'reasons': ['field_type'], 'items': [{'status': 'failed', 'reasons': ['field_type']}]}
        problems = runner.compare(expect, actual, 'put')
        self.assertEqual(len(problems), 5, problems)

    def test_envelope_fields_and_reason_shape(self):
        actual = {'status': 'failed', 'reasons': [{'code': 'field_type'}]}
        problems = runner.compare({'status': 'failed', 'reasons': ['field_type']}, actual, 'put')
        self.assertTrue(any('STS-ENV-1' in p for p in problems))
        self.assertTrue(any('STS-RSN-2' in p for p in problems))

    def test_item_count(self):
        problems = runner.compare({'status': 'ok', 'reasons': [], 'items': [OK_ITEM]}, envelope('put', 'ok', []), 'put')
        self.assertEqual(problems, ['result has no items, not 1'])

    def test_returned_items_need_status_and_reasons(self):
        actual = envelope('sample_operation', 'unsupported', ['unsupported_operation'], [{}])
        problems = runner.compare({'status': 'unsupported', 'reasons': ['unsupported_operation']}, actual, 'sample_operation')
        self.assertEqual(problems, ['item 0 does not have status and reasons (STS-ENV-2)'])

    def test_returned_item_reasons_have_the_reason_fields(self):
        actual = envelope('get', 'ok', [], [{'status': 'ok', 'reasons': None}, {'status': 'failed', 'reasons': [{'code': 'x'}]}])
        problems = runner.compare({'status': 'ok', 'reasons': []}, actual, 'get')
        self.assertEqual(len(problems), 3, problems)

    def test_returned_items_have_a_valid_status(self):
        expect = {'status': 'unsupported', 'reasons': ['reserved_profile']}
        for item in ({'status': 'partial', 'reasons': []}, {'status': 'ok', 'reasons': [REASON_X]},
                     {'status': 'failed', 'reasons': []}, {'status': 'done', 'reasons': [REASON_X]},
                     {'status': [], 'reasons': [REASON_X]}, {'status': {}, 'reasons': [REASON_X]}):
            actual = envelope('semantic_search', 'unsupported', ['reserved_profile'], [item])
            problems = runner.compare(expect, actual, 'semantic_search')
            self.assertTrue(any('STS-SET-1, STS-RSN-1' in p for p in problems), item)
        actual = envelope('semantic_search', 'unsupported', ['reserved_profile'], [{'status': 'failed', 'reasons': [REASON_X]}])
        self.assertEqual(runner.compare(expect, actual, 'semantic_search'), [])
        bounds = envelope('get', 'partial', ['bounds_reached'])['reasons'][0]
        actual = envelope('semantic_search', 'unsupported', ['reserved_profile'], [{'status': 'failed', 'reasons': [bounds]}])
        self.assertTrue(any('STS-RSN-3' in p for p in runner.compare(expect, actual, 'semantic_search')))
        ext = dict(REASON_X, code='ext:sample/io/v1#disk_error')
        actual = envelope('semantic_search', 'unsupported', ['reserved_profile'], [{'status': 'failed', 'reasons': [ext]}])
        self.assertEqual(runner.compare(expect, actual, 'semantic_search'), [])

    def test_partial_reduction(self):
        ok, bad = {'status': 'ok', 'reasons': []}, {'status': 'not_found', 'reasons': ['not_found']}
        cases = [(['items_not_ok'], [ok, bad], True), (['bounds_reached'], [ok], True),
                 (['bounds_reached', 'items_not_ok'], [ok, bad], True), (['items_not_ok'], [ok, ok], False),
                 (['bounds_reached'], [ok, bad], False), (['items_not_ok'], [bad], False), (['bounds_reached'], [], False)]
        for codes, items, valid in cases:
            errors = []
            runner.check_spec.check_reduction('v', 'evidence', 'partial', codes, items, errors)
            self.assertEqual(errors == [], valid, (codes, items, errors))

    def test_mixed_items_need_partial(self):
        ok, bad = {'status': 'ok', 'reasons': []}, {'status': 'not_found', 'reasons': ['not_in_context']}
        errors = []
        runner.check_spec.check_reduction('v', 'query_data', 'not_found', ['not_in_context'], [ok, bad], errors, True)
        self.assertIn('STS-ENV-3 rule 5', errors[0])
        errors = []
        runner.check_spec.check_reduction('v', 'put', 'not_found', ['not_in_context'], [bad], errors, False)
        self.assertEqual(errors, [])
        errors = []
        whole = {'status': 'failed', 'reasons': ['storage_error']}
        form = {'status': 'failed', 'reasons': ['payload_form']}
        runner.check_spec.check_reduction('v', 'query_data', 'failed', ['storage_error'], [ok, form, whole], errors, True)
        self.assertEqual(errors, [])  # STS-ENV-3 rule 1
        profile = {'status': 'unsupported', 'reasons': ['unsupported_profile']}
        for item in (form, profile):
            errors = []
            runner.check_spec.check_reduction('v', 'query_data', item['status'], item['reasons'], [ok, item], errors, True)
            self.assertIn('STS-ENV-3 rule 5', errors[0])
        errors = []
        runner.check_spec.check_reduction('v', 'query_data', 'failed', ['payload_form'], [ok, bad], errors, True)
        self.assertIn('STS-ENV-3 rule 5', errors[0])
        disk = 'ext:sample/io/v1#disk_error'
        errors = []
        runner.check_spec.check_reduction('v', 'query_data', 'failed', [disk], [ok, bad], errors, True)
        self.assertEqual(errors, [])  # STS-ENV-3 rule 1, STS-RSN-2
        runner.check_spec.check_reduction('v', 'query_data', 'failed', [disk], [ok, {'status': 'failed', 'reasons': [disk]}],
                                          errors, True)
        self.assertEqual(errors, [])

    def test_refused_query_item_has_no_value(self):
        refused = {'status': 'unsupported', 'reasons': [], 'rows': [{'x': 42}], 'metadata': {}, 'next': None}
        self.assertEqual(len(runner.value_problems('query_data', {'items': [refused]})), 3)  # QRY-DAT-3
        self.assertEqual(runner.value_problems('query_data', {'items': [dict(refused, status='ok')]}), [])
        self.assertEqual(runner.value_problems('search', {'items': [refused]}), [])

    def test_item_fields_and_order(self):
        e = [{'status': 'ok', 'reasons': [], 'fields': {'ref': REF_A}}, {'status': 'ok', 'reasons': [], 'fields': {'ref': 'b'}}]
        actual = envelope('evidence', 'ok', [], [{'status': 'ok', 'reasons': [], 'ref': 'b'}, {'status': 'ok', 'reasons': [], 'ref': REF_A}])
        self.assertEqual(len(runner.compare({'status': 'ok', 'reasons': [], 'items': e}, actual, 'evidence')), 2)
        self.assertEqual(runner.compare({'status': 'ok', 'reasons': [], 'items': e, 'unordered': True}, actual, 'evidence'), [])
        actual['items'][0]['ref'] = REF_A
        problems = runner.compare({'status': 'ok', 'reasons': [], 'items': e, 'unordered': True}, actual, 'evidence')
        self.assertEqual(problems, ['the items do not match the expected items in any order'])

    def test_item_fields_are_present_and_typed(self):
        for want, got in ((None, {}), (1, {'v': True}), (True, {'v': 1})):
            expect = {'status': 'ok', 'reasons': [], 'items': [{'status': 'ok', 'reasons': [], 'fields': {'v': want}}]}
            actual = envelope('get', 'ok', [], [{'status': 'ok', 'reasons': [], **got}])
            self.assertEqual(len(runner.compare(expect, actual, 'get')), 1, (want, got))

    def test_equal_numbers(self):
        expect = {'status': 'ok', 'reasons': [], 'items': [{'status': 'ok', 'reasons': [], 'fields': {'v': [1, {'w': 2}]}}]}
        actual = envelope('get', 'ok', [], [{'status': 'ok', 'reasons': [], 'v': [1.0, {'w': 2.0}]}])
        self.assertEqual(runner.compare(expect, actual, 'get'), [])

    def test_reason_details_refs(self):
        ref_b = 'sha256:' + 'b' * 64
        expect = {'status': 'incompatible', 'reasons': ['binding_skew'], 'items': [
            {'status': 'incompatible', 'reasons': ['binding_skew'], 'reason_details': [{'code': 'binding_skew', 'refs': [REF_A, ref_b]}]}]}
        for refs, n in (([ref_b, REF_A], 0), ([REF_A], 1), ([], 1), ([1], 1)):
            reason = {'code': 'binding_skew', 'refs': refs, 'capabilities': []}
            actual = envelope('open_context', 'incompatible', ['binding_skew'], [{'status': 'incompatible', 'reasons': [reason]}])
            self.assertEqual(len(runner.compare(expect, actual, 'open_context')), n, refs)

    def test_reason_details_capabilities(self):
        cap = 'aavanam/data/v1'
        expect = {'status': 'unsupported', 'reasons': ['capability_unavailable'], 'items': [
            {'status': 'unsupported', 'reasons': ['capability_unavailable'], 'reason_details': [{'code': 'capability_unavailable', 'capabilities': [cap]}]}]}
        for caps, n in (([cap], 0), ([], 1), (['aavanam/lexical/v1'], 1), ([1], 1)):
            reason = {'code': 'capability_unavailable', 'refs': [REF_A], 'capabilities': caps}
            actual = envelope('query_data', 'unsupported', ['capability_unavailable'], [{'status': 'unsupported', 'reasons': [reason]}])
            self.assertEqual(len(runner.compare(expect, actual, 'query_data')), n, caps)

    def test_reason_details_bad_code(self):
        cap = 'aavanam/data/v1'
        details = [{'code': 'capability_unavailable', 'capabilities': [cap]}, {'code': 'unsupported_profile', 'capabilities': [cap]}]
        for code in (None, 1):
            reasons = [{'code': 'capability_unavailable', 'refs': [], 'capabilities': [cap]}, {'code': code, 'refs': [], 'capabilities': [cap]}]
            env = {'status': 'unsupported', 'reasons': ['capability_unavailable', 'unsupported_profile'], 'reason_details': details}
            actual = envelope('query_data', 'unsupported', [], [])
            actual['reasons'] = reasons
            self.assertTrue(runner.compare(env, actual, 'query_data'), code)
            item = dict(env, status='unsupported')
            del item['reason_details']
            item['items'] = [{'status': 'unsupported', 'reasons': env['reasons'], 'reason_details': details}]
            actual = envelope('query_data', 'unsupported', env['reasons'], [{'status': 'unsupported', 'reasons': reasons}])
            self.assertTrue(runner.compare(item, actual, 'query_data'), code)

    def test_reason_details_form(self):
        good = [{'code': 'binding_skew', 'refs': [{'$ref': 'x1'}, REF_A]}, {'code': 'unsupported_profile', 'capabilities': ['aavanam/x/v1']},
                {'code': 'binding_skew', 'refs': [], 'capabilities': []}]
        self.assertTrue(runner.check_spec.is_reason_details(good))
        for bad in ([{'code': 'binding_skew', 'refs': [1, REF_A]}], [{'code': 'binding_skew', 'refs': ['x1']}],
                    [{'code': 'binding_skew'}], {'code': 'binding_skew', 'refs': []}, [{'code': 'x', 'capabilities': [1]}],
                    [{'code': 'x', 'refs': [], 'other': []}]):
            self.assertFalse(runner.check_spec.is_reason_details(bad), bad)

    def test_not_an_object(self):
        self.assertEqual(len(runner.compare({'status': 'ok', 'reasons': []}, None, 'put')), 1)


class ExpanderTest(unittest.TestCase):
    def test_payload_faults(self):
        good = runner.Expander('t', {'payloads': PAYLOADS})
        request, _ = good.request('put', {'descriptors': [{'value': {'payload': {'$payload': 'p'}}}]})
        digest = runner.digest(b'abc')
        self.assertEqual(request['payloads'], {digest: base64.b64encode(b'abc').decode()})
        self.assertIn(f'"size":3', request['descriptors'][0])
        bad, _ = good.request('put', {'descriptors': [{'value': {'payload': {'$payload': 'p', 'fault': 'digest'}}}]})
        self.assertNotIn(digest, bad['payloads'])
        self.assertEqual(len(bad['payloads']), 1)
        size, _ = good.request('put', {'descriptors': [{'value': {'payload': {'$payload': 'p', 'fault': 'size'}}}]})
        self.assertIn('"size":4', size['descriptors'][0])
        absent = runner.Expander('t', {'payloads': {'p': {**PAYLOADS['p'], 'stored': False}}})
        request, _ = absent.request('put', {'descriptors': [{'value': {'payload': {'$payload': 'p'}}}]})
        self.assertEqual(request['payloads'], {})

    def test_raw_and_handles(self):
        e = runner.Expander('t', {})
        request, names = e.request('put', {'descriptors': [{'raw': '{'}, {'as': 'x', 'value': {'a': {'$absent': 'y'}}}]})
        self.assertEqual(request['descriptors'][0], '{')
        self.assertEqual(names, [None, 'x'])
        self.assertRegex(request['descriptors'][1], r'"a":"sha256:[0-9a-f]{64}"')
        with self.assertRaises(runner.VectorError):
            e.request('put', {'descriptors': [{'value': {'a': {'$ref': 'unbound'}}}]})
        with self.assertRaises(runner.VectorError):
            e.request('put', {'descriptors': [{'value': {'$payload': 'unknown'}}]})


class RunVectorTest(unittest.TestCase):
    def test_binds_handles(self):
        vector = {'steps': [put_step([{'as': 'a', 'value': {}}], items=[OK_ITEM]),
                            put_step([{'value': {'target': {'$ref': 'a'}}}], items=[OK_ITEM])]}
        a = rev({})
        adapter = Scripted(envelope('put', 'ok', [], [{'status': 'ok', 'reasons': [], 'ref': a}]),
                           envelope('put', 'ok', [], [{'status': 'ok', 'reasons': [], 'ref': rev({'target': a})}]))
        self.assertEqual(runner.run_vector(adapter, 't', vector), [])
        self.assertIn(a, adapter.requests[1]['descriptors'][0])

    def test_missing_ref_fails(self):
        vector = {'steps': [put_step([{'as': 'a', 'value': {}}], items=[OK_ITEM])]}
        problems = runner.run_vector(Scripted(envelope('put', 'ok', [], [OK_ITEM])), 't', vector)
        self.assertEqual(len(problems), 1)
        self.assertIn('not the revision', problems[0])
        problems = runner.run_vector(Scripted(envelope('put', 'ok', [], [{**OK_ITEM, 'ref': REF_A}])), 't', vector)
        self.assertIn('IDN-CAN-2', problems[0])

    def test_no_binding_after_a_refusal(self):
        failed = {'status': 'failed', 'reasons': ['field_type']}
        vector = {'steps': [put_step([{'as': 'a', 'value': {}}], 'failed', ['field_type'], items=[failed])]}
        adapter = Scripted(envelope('put', 'failed', ['field_type'], [{**failed, 'reasons': envelope('put', 'failed', ['field_type'])['reasons']}]))
        self.assertEqual(runner.run_vector(adapter, 't', vector), [])

    def test_store_change(self):
        vector = {'steps': [put_step([{'value': {}}], 'failed', ['field_type'], store_unchanged=True)]}
        problems = runner.run_vector(Scripted(envelope('put', 'failed', ['field_type']), change_store=True), 't', vector)
        self.assertEqual(problems, ['step 1 (put): the writer store changed (STS-MAL-2)'])

    def test_context_handle_and_identity(self):
        ok = {'status': 'ok', 'reasons': []}
        vector = {'steps': [{'operation': 'open_context', 'request': {}, 'as_context': 'c', 'expect': ok},
                            {'operation': 'get', 'request': {'context': {'$context': 'c'}}, 'expect': ok}]}
        opened = {**envelope('open_context', 'ok', []), 'context': 'ctx-1'}
        adapter = Scripted(opened, {**envelope('get', 'ok', []), 'context': 'ctx-1'})
        self.assertEqual(runner.run_vector(adapter, 't', vector), [])
        self.assertEqual(adapter.requests[1], {'context': 'ctx-1'})
        problems = runner.run_vector(Scripted(opened, envelope('get', 'ok', [])), 't', vector)
        self.assertEqual(problems, ['step 2 (get): context is None, not the request context (CTX-OPN-3)'])
        problems = runner.run_vector(Scripted(envelope('open_context', 'ok', [])), 't', vector)
        self.assertEqual(problems, ['step 1 (open_context): context is None, not a context identity (CTX-OPN-2)'])
        empty = {**envelope('open_context', 'ok', []), 'context': ''}
        self.assertEqual(runner.run_vector(Scripted(empty, {**envelope('get', 'ok', []), 'context': ''}), 't', vector), [])

    def test_store_reads_have_no_context(self):
        step = {'operation': 'item_history', 'request': {'collection': 'handbook', 'logical_id': 'a'},
                'expect': {'status': 'ok', 'reasons': []}}
        self.assertEqual(runner.run_vector(Scripted(envelope('item_history', 'ok', [])), 't', {'steps': [step]}), [])
        result = {**envelope('item_history', 'ok', []), 'context': 'ctx-1'}
        problems = runner.run_vector(Scripted(result), 't', {'steps': [step]})
        self.assertEqual(problems, ["step 1 (item_history): context is 'ctx-1', not null (LIF-ITM-4)"])

    def test_context_of_refusals(self):
        refused = {'operation': 'open_context', 'request': {},
                   'expect': {'status': 'not_found', 'reasons': ['unknown_ref']}}
        opened = {**envelope('open_context', 'not_found', ['unknown_ref']), 'context': 'ctx-1'}
        problems = runner.run_vector(Scripted(opened), 't', {'steps': [refused]})
        self.assertEqual(problems, ["step 1 (open_context): context is 'ctx-1', not null (CTX-OPN-2)"])
        malformed = {'operation': 'get', 'request': {'context': 42},
                     'expect': {'status': 'failed', 'reasons': ['field_type']}}
        self.assertEqual(runner.run_vector(Scripted(envelope('get', 'failed', ['field_type'])), 't', {'steps': [malformed]}), [])
        unknown = {'operation': 'sample_operation', 'request': {'context': 42},
                   'expect': {'status': 'unsupported', 'reasons': ['unsupported_operation']}}
        result = envelope('sample_operation', 'unsupported', ['unsupported_operation'])
        self.assertEqual(runner.run_vector(Scripted(result), 't', {'steps': [unknown]}), [])
        with self.assertRaises(runner.VectorError):
            runner.Expander('t', {}).value({'$context': 'c'}, {})

    def test_bundle_handle_faults_and_fresh_store(self):
        ok = {'status': 'ok', 'reasons': []}
        vector = {'steps': [{'operation': 'export', 'request': {'roots': []}, 'as_bundle': 'x', 'expect': ok},
                            {'operation': 'import', 'fresh_store': True, 'request': {'bundle': {'$bundle': 'x', 'fault': 'drop_payload'}}, 'expect': ok},
                            {'operation': 'import', 'request': {'bundle': {'$bundle': 'x', 'fault': 'format'}}, 'expect': ok}]}
        a, b = (source(x) for x in (b'a', b'b'))
        bundle = {'format': 'aavanam-bundle/v1', 'roots': [], 'descriptors': [a[0], b[0]], 'payloads': {b[1]: 'Yg==', a[1]: 'YQ=='}}
        adapter = Scripted({**envelope('export', 'ok', []), 'bundle': bundle}, envelope('import', 'ok', []), envelope('import', 'ok', []))
        resets = []
        adapter.reset = lambda: resets.append(1)
        self.assertEqual(runner.run_vector(adapter, 't', vector), [])
        self.assertEqual(len(resets), 2)
        self.assertEqual(adapter.requests[1]['bundle']['payloads'], {a[1]: 'YQ=='})
        self.assertEqual(adapter.requests[2]['bundle']['format'], 'sample/bundle/v1')
        self.assertEqual(bundle['payloads'], {b[1]: 'Yg==', a[1]: 'YQ=='})
        problems = runner.run_vector(Scripted(envelope('export', 'ok', [])), 't', {'steps': vector['steps'][:1]})
        self.assertIn('fields of BUN-MAN-1', problems[0])

    def test_export_bundle_checks(self):
        ok = {'status': 'ok', 'reasons': []}
        step = {'operation': 'export', 'request': {'roots': ['r']}, 'expect': ok}
        text, digest = source(b'a')
        good = {'format': 'aavanam-bundle/v1', 'roots': ['r'], 'descriptors': [text], 'payloads': {digest: 'YQ=='}}
        opaque = json.dumps({'kind': 'source', 'payload': json.loads(text)['payload'], 'ext': {'x': {'digest': [], 'media_type': 'a', 'size': 1}}})
        cases = [({}, 'fields of BUN-MAN-1'), ({**good, 'format': 'x'}, 'format'), ({**good, 'roots': ['q']}, 'roots'),
                 ({**good, 'descriptors': [text, text]}, 'more than one time'), ({**good, 'payloads': {}}, 'byte closure'),
                 ({**good, 'payloads': {digest: '*'}}, 'not base64'), ({**good, 'descriptors': ['{']}, 'not JSON'),
                 ({**good, 'payloads': {digest: 'Yg=='}}, 'digest and size'),
                 ({**good, 'descriptors': ['{"kind":"x",' + text[1:]]}, 'duplicate key'),
                 ({**good, 'descriptors': [json.dumps({'kind': []})]}, 'not the byte closure')]
        for bundle, words in cases:
            problems = runner.run_vector(Scripted({**envelope('export', 'ok', []), 'bundle': bundle}), 't', {'steps': [step]})
            self.assertTrue(problems and words in problems[0], (words, problems))
        for bundle in (good, {**good, 'descriptors': [opaque]}):
            self.assertEqual(runner.run_vector(Scripted({**envelope('export', 'ok', []), 'bundle': bundle}), 't', {'steps': [step]}), [])
        refused = {**step, 'expect': {'status': 'not_found', 'reasons': ['not_in_context']}}
        actual = {**envelope('export', 'not_found', ['not_in_context']), 'bundle': good}
        self.assertIn('refused export has a bundle', runner.run_vector(Scripted(actual), 't', {'steps': [refused]})[0])
        with self.assertRaises(runner.VectorError):
            runner.Expander('t', {}).value({'$bundle': 'x'}, {})

    def test_export_compares_canonical_descriptors(self):
        ok = {'status': 'ok', 'reasons': []}
        refs = ['sha256:' + c * 64 for c in 'abc']
        put = put_step([{'as': 's', 'value': {'kind': 'snapshot', 'collection': 'c', 'units': [], 'schemas': refs[1:][::-1], 'ext': {'n': 1}}}],
                       items=[ok])
        export = {'operation': 'export', 'request': {'roots': [{'$ref': 's'}]},
                  'expect': {**ok, 'items': [{**ok, 'fields': {'ref': {'$ref': 's'}}}]}}
        bundle = {'format': 'aavanam-bundle/v1', 'roots': [refs[0]], 'payloads': {},
                  'descriptors': [json.dumps({'kind': 'snapshot', 'collection': 'c', 'schemas': refs[1:]})]}
        refs[0] = rev(put['request']['descriptors'][0]['value'])
        bundle['roots'] = [refs[0]]
        for collection, problems in (('c', 0), ('d', 1)):
            bundle['descriptors'] = [json.dumps({'kind': 'snapshot', 'collection': collection, 'schemas': refs[1:], 'ext': {'n': 1.0}})]
            adapter = Scripted({**envelope('put', 'ok', [], [{'status': 'ok', 'reasons': [], 'ref': refs[0]}])},
                               {**envelope('export', 'ok', [], [{'status': 'ok', 'reasons': [], 'ref': refs[0]}]), 'bundle': bundle})
            self.assertEqual(len(runner.run_vector(adapter, 't', {'steps': [put, export]})), problems, collection)
        for text in ('{"kind": "snapshot", "n": NaN}', '{"kind": "snapshot", "n": 1e400}'):
            bundle['descriptors'] = [text]
            adapter = Scripted({**envelope('put', 'ok', [], [{'status': 'ok', 'reasons': [], 'ref': refs[0]}])},
                               {**envelope('export', 'ok', [], [{'status': 'ok', 'reasons': [], 'ref': refs[0]}]), 'bundle': bundle})
            self.assertTrue(any('not finite' in p for p in runner.run_vector(adapter, 't', {'steps': [put, export]})), text)
        for result in (None, [], 'ok'):
            self.assertTrue(runner.run_vector(Scripted(result), 't', {'steps': [put]}))

    def test_exception(self):
        vector = {'steps': [put_step([{'value': {}}])]}
        problems = runner.run_vector(Scripted(ValueError('boom')), 't', vector)
        self.assertIn('raised ValueError', problems[0])

    def test_stops_at_first_failed_step(self):
        vector = {'steps': [put_step([{'value': {}}]), put_step([{'value': {}}])]}
        adapter = Scripted(envelope('put', 'failed', ['field_type']), envelope('put', 'ok', []))
        self.assertEqual(len(runner.run_vector(adapter, 't', vector)), 2)
        self.assertEqual(len(adapter.requests), 1)


def source(data: bytes) -> tuple[str, str]:
    """Return the text of a source descriptor for the bytes, and their digest."""
    digest = 'sha256:' + hashlib.sha256(data).hexdigest()
    return json.dumps({'kind': 'source', 'payload': {'digest': digest, 'media_type': 'text/plain', 'size': len(data)}}), digest


def echo(vector: dict) -> list[dict]:
    """Return the envelopes that the steps of a vector expect, with a ref on each ok put item."""
    vector = runner.check_spec.with_fixture(vector)
    expander = runner.Expander('echo', vector)
    results, stored, sent, fields = [], {}, {}, runner.check_spec.schemas()
    for n, step in enumerate(vector['steps']):
        if step.get('fresh_store'):
            expander.contexts.clear()
        e, op = step['expect'], step['operation']
        request, names = expander.request(op, step.get('request', {}))
        items = []
        sent.update(request.get('payloads', {}) if op == 'put' else {})
        for k, i in enumerate(expander.value(e.get('items', []), {})):
            item = {'status': i['status'], 'reasons': envelope('', '', i['reasons'])['reasons'], **i.get('fields', {})}
            if 'reason_details' in i:
                item['reasons'] = [{'code': r['code'], 'refs': r.get('refs', []), 'capabilities': r.get('capabilities', [])}
                                   for r in i['reason_details']]
            if op == 'put' and i['status'] == 'ok':
                item['ref'] = runner.revision(json.loads(request['descriptors'][k]), fields)
                if names[k]:
                    expander.refs[names[k]] = item['ref']
                    stored[item['ref']] = request['descriptors'][k]
            items.append(item)
        if op in runner.CREATE_OPERATIONS and e['status'] == 'ok':
            field, value = runner.created(op, request)
            items[0][field] = value
            if op == 'create_snapshot':
                stored[value] = json.dumps(runner.snapshot_descriptor(request), sort_keys=True)
            if 'as_ref' in step:
                expander.refs[step['as_ref']] = value
        result = envelope(op, e['status'], e['reasons'], items[::-1] if e.get('unordered') else items)
        if 'reason_details' in e:
            result['reasons'] = [{'code': r['code'], 'refs': r.get('refs', []), 'capabilities': r.get('capabilities', [])}
                                 for r in expander.value(e['reason_details'], {})]
        for r in result['reasons']:
            if r['code'] == 'reserved_profile':
                r['capabilities'] = [runner.check_spec.reserved()[op]]
        if op == 'open_context' and e['status'] == 'ok':
            result['context'] = f'context-{n}'  # CTX-OPN-2
            if 'as_context' in step:
                expander.contexts[step['as_context']] = result['context']
        if op == 'export' and e['status'] == 'ok':
            texts = [stored[i['ref']] for i in items]
            digests = {p['digest'] for t in texts for p in runner.payload_objects(json.loads(t), fields)}
            result['bundle'] = {'format': 'aavanam-bundle/v1', 'roots': request['roots'], 'descriptors': texts,
                                'payloads': {d: sent[d] for d in digests}}
            if 'as_bundle' in step:
                expander.bundles[step['as_bundle']] = result['bundle']
        if op == 'render':
            ok = e['status'] in ('ok', 'partial')
            result['document'] = runner.render_document(request, items) if ok else None
            result['document_id'] = runner.digest(runner.canonical(result['document']).encode()) if ok else None
        if op in runner.LOCK_OPERATIONS and e['status'] == 'ok':
            result['lock'] = runner.revision(runner.lock_descriptor(op, request), fields)
        if op != 'put' and op not in runner.STORE_READS and 'as_context' not in step and 'context' in request:  # LIF-ITM-4
            result['context'] = request['context']
        results.append(result)
    return results


class CanonicalTest(unittest.TestCase):
    def test_rfc_8785_examples(self):
        # The values come from RFC 8785, appendix B and section 3.2.3.
        numbers = {0.0: '0', -0.0: '0', 5e-324: '5e-324', 1.7976931348623157e308: '1.7976931348623157e+308',
                   9007199254740992.0: '9007199254740992', 295147905179352830000.0: '295147905179352830000',
                   333333333.3333332: '333333333.3333332', 1e21: '1e+21', 1e-7: '1e-7', 0.000001: '0.000001', 1.0: '1'}
        for x, text in numbers.items():
            self.assertEqual(runner.canonical(x), text)
        for x in (float('nan'), float('inf'), 10 ** 400):
            self.assertRaises(ValueError, runner.canonical, x)
        deep = json.loads('[' * 900 + ']' * 900)
        self.assertEqual(runner.canonical({'ext': deep, 'a': [1, {'b': None}]}), '{"a":[1,{"b":null}],"ext":' + '[' * 900 + ']' * 900 + '}')
        self.assertEqual(runner.canonical([[], {}, '']), '[[],{},""]')
        keys = {'\u20ac': 1, '\r': 2, '\ufb33': 3, '1': 4, '\U0001f600': 5, '\u0080': 6, '\u00f6': 7}
        self.assertEqual([json.loads(runner.canonical(keys))[k] for k in json.loads(runner.canonical(keys))], [2, 4, 6, 7, 1, 5, 3])

    def test_revision_ignores_set_order_and_empty_sets(self):
        a, b = 'sha256:' + 'a' * 64, 'sha256:' + 'b' * 64
        one = {'kind': 'snapshot', 'collection': 'c', 'schemas': [a, b]}
        self.assertEqual(rev(one), rev({'collection': 'c', 'kind': 'snapshot', 'schemas': [b, a], 'units': []}))
        self.assertNotEqual(rev(one), rev({**one, 'ext': {}}))


class EchoTest(unittest.TestCase):
    def test_correct_envelopes_pass_each_vector(self):
        # This tests the runner only. The envelopes come from the vectors, not from an implementation.
        for vid, vector in runner.load().items():
            adapter = Scripted(*echo(vector))
            self.assertEqual(runner.run_vector(adapter, vid, vector), [], vid)


class MainTest(unittest.TestCase):
    def test_bad_vectors_exit_2(self):
        for body in ({'steps': []}, {'steps': None}, []):
            with tempfile.TemporaryDirectory() as d:
                (Path(d) / 'bad.json').write_text(json.dumps(body))
                out = io.StringIO()
                with redirect_stdout(out):
                    code = runner.main(['--adapter', 'null_adapter:NullAdapter', '--vectors', d])
                self.assertEqual(code, 2, body)
                self.assertIn('ERROR', out.getvalue())
                self.assertNotIn('PASS', out.getvalue())

    def test_missing_vector_folder_exits_2(self):
        with tempfile.TemporaryDirectory() as d, redirect_stdout(io.StringIO()):
            self.assertEqual(runner.main(['--adapter', 'null_adapter:NullAdapter', '--vectors', str(Path(d) / 'none')]), 2)

    def test_bad_items_exit_2(self):
        source = json.loads((runner.VECTORS / 'kinds/put-source-ok.json').read_text())
        expects = [{'status': 'ok', 'reasons': [], 'items': None},
                   {'status': 'failed', 'reasons': ['field_type'], 'items': [{'status': 'partial', 'reasons': ['field_type']}]}]
        for expect in expects:
            with tempfile.TemporaryDirectory() as d:
                vector = {**source, 'steps': [{**source['steps'][0], 'expect': expect}]}
                vector.pop('pair', None)
                (Path(d) / 'bad.json').write_text(json.dumps(vector))
                with redirect_stdout(io.StringIO()):
                    self.assertEqual(runner.main(['--adapter', 'null_adapter:NullAdapter', '--vectors', d]), 2, expect)


    def test_bad_context_steps_exit_2(self):
        source = json.loads((runner.VECTORS / 'kinds/put-source-ok.json').read_text())
        step = source['steps'][0]
        ok = {'status': 'ok', 'reasons': []}
        bad = [{**step, 'as_contxt': 'c'},
               {**step, 'as_context': 1},
               {'operation': 'get', 'request': {'context': {'$context': 'c'}}, 'expect': ok},
               {**step, 'expect': {**step['expect'], 'items': [{**ok, 'field': {}}]}},
               {**step, 'expect': {**step['expect'], 'items': [{**ok, 'fields': []}]}},
               {**step, 'expect': {**step['expect'], 'unordered': 'yes'}},
               {**step, 'expect': {**step['expect'], 'unordered': True}},
               {**step, 'as_bundle': 2},
               {**step, 'fresh_store': 'yes'},
               {'operation': 'import', 'request': {'bundle': {'$bundle': 'x'}}, 'expect': ok}]
        for s in bad:
            with tempfile.TemporaryDirectory() as d:
                (Path(d) / 'bad.json').write_text(json.dumps({**source, 'steps': [s]}))
                with redirect_stdout(io.StringIO()):
                    self.assertEqual(runner.main(['--adapter', 'null_adapter:NullAdapter', '--vectors', d]), 2, s)


class FixtureTest(unittest.TestCase):
    def fixture(self, change=None, files=None):
        base = {'descriptor_profile': 'aavanam/base/v1'}
        body = {'format': 'aavanam-fixture/v1', 'collection': 'c', 'root': 'snap',
                'payloads': {'text': {'media_type': 'text/plain', 'file': 'a.txt'}},
                'descriptors': [
                    {'as': 'src', 'value': {**base, 'kind': 'source', 'payload': {'$payload': 'text'}}},
                    {'as': 'rd', 'value': {**base, 'kind': 'reading', 'source': {'$ref': 'src'}, 'producer': 'p',
                                           'coverage_profile': 'c', 'payload': {'$payload': 'text'}}},
                    {'as': 'ex', 'value': {**base, 'kind': 'excerpt', 'parent': {'$ref': 'rd'},
                                           'ranges': {'$ranges': 'text', 'text': 'beta'},
                                           'payload': {'$text': 'beta', 'media_type': 'text/plain'}}},
                    {'as': 'snap', 'value': {**base, 'kind': 'snapshot', 'collection': 'c'}}]}
        if change:
            change(body)
        with tempfile.TemporaryDirectory() as d:
            folder = Path(d) / 'fixtures' / 'one'
            folder.mkdir(parents=True)
            for name, text in (files or {'a.txt': 'alpha beta'}).items():
                (folder / name).write_text(text)
            (folder / 'fixture.json').write_text(json.dumps(body))
            return runner.check_spec.check_fixtures(Path(d) / 'fixtures')

    def test_lock_reference(self):
        for op in sorted(runner.LOCK_OPERATIONS):
            ok, refused = {'status': 'ok', 'reasons': []}, {'status': 'not_found', 'reasons': ['unknown_ref']}
            request = {'binding_set': REF_A, 'context_inputs': [], 'inputs': [REF_A], 'consumer': 'c', 'completeness': 'd'}
            lock = rev(runner.lock_descriptor(op, request))
            self.assertEqual(runner.lock_problems(op, request, ok, {'lock': lock}), [])
            self.assertIn('not the revision', runner.lock_problems(op, request, ok, {})[0])
            self.assertIn('not the revision', runner.lock_problems(op, request, ok, {'lock': REF_A})[0])
            self.assertIn('has no lock', runner.lock_problems(op, request, refused, {'lock': lock})[0])
            self.assertEqual(runner.lock_problems(op, request, refused, {'lock': None}), [])
        self.assertEqual(runner.lock_problems('get', {}, {'status': 'ok', 'reasons': []}, {}), [])
        vector = runner.load()['scenarios/emitter-handbook']
        results = echo(vector)
        unwritten = runner.run_vector(Scripted(*results, write_locks=False), 'e', vector)
        self.assertTrue(any('did not have the lock' in p for p in unwritten))
        self.assertTrue(any('so it has no lock' in p for p in unwritten))  # LCK-EXE-4, LCK-BLD-3
        last = vector['steps'][-1]['request']
        lock = {'descriptor_profile': runner.BASE_PROFILE, 'kind': 'build-lock',
                **{f: last[f] for f in ('inputs', 'consumer', 'completeness')}}
        earlier = [s for s in vector['steps'][:-1] if s['operation'] not in runner.LOCK_OPERATIONS]
        stored = dict(vector, steps=earlier + [put_step([{'value': lock}], items=[{'status': 'ok', 'reasons': []}])] + vector['steps'][-1:])
        self.assertEqual(runner.run_vector(Scripted(*echo(stored), write_locks=False), 'e', stored), [])
        results[-1].pop('lock')
        self.assertTrue(any('not the revision of the lock' in p
                            for p in runner.run_vector(Scripted(*results), 'e', vector)))

    def test_lock_probe_item(self):
        request = {'binding_set': REF_A, 'context_inputs': []}
        lock = rev(runner.lock_descriptor('execution_inputs', request))

        class Probe:
            def __init__(self, item):
                self.item = item

            def fingerprint(self, store='writer'):
                return 0

            def call(self, operation, request, store='writer'):
                envelope = {'status': 'ok', 'reasons': [], 'operation': 'put', 'operation_profile': 'p', 'context': None}
                result = {**envelope, **self.item.pop('envelope', {}), 'items': [{k: v for k, v in {'ref': lock, **self.item}.items() if v != 'drop'}]}
                return {k: v for k, v in result.items() if v != 'drop'}

        def problems(item):
            return runner.stored_lock_problems(Probe(item), 'execution_inputs', request, lock)

        self.assertEqual(problems({'status': 'ok', 'reasons': []}), [])
        self.assertIn('does not give ok', problems({'status': 'failed', 'reasons': ['storage_error']})[0])
        self.assertIn('does not give ok', problems({'status': 'ok', 'reasons': [], 'envelope': {'reasons': ['storage_error']}})[0])
        self.assertIn('STS-ENV-1', problems({'status': 'ok', 'reasons': [], 'envelope': {'context': 'drop'}})[0])
        self.assertIn('ref is', problems({'status': 'ok', 'reasons': [], 'ref': REF_A})[0])
        self.assertIn('ref is', problems({'status': 'ok', 'reasons': [], 'ref': 'drop'})[0])
        self.assertIn('not null (STS-ENV-1)', problems({'status': 'ok', 'reasons': [], 'envelope': {'context': 'c'}})[0])

    def test_reserved_profile_reason(self):
        cs = runner.check_spec
        profiles = cs.reserved()
        self.assertEqual(set(profiles), {op for op, doc in self.operation_docs().items() if doc == '16'})
        for vid in [v for v in runner.load() if v.startswith('reserved/')]:
            vector = runner.load()[vid]
            self.assertEqual(runner.run_vector(Scripted(*echo(vector)), vid, vector), [], vid)
        vector = runner.load()['reserved/semantic-search']
        for reason in ({'capabilities': []}, {'capabilities': ['aavanam/gc/v1']}, {'refs': [REF_A]}):
            result = echo(vector)[0]
            result['reasons'][0].update(reason)
            problems = runner.run_vector(Scripted(result), 'r', vector)
            self.assertTrue(any('RSV-RSP-1' in p for p in problems), reason)
        for field in ({'lock': REF_A}, {'bundle': {}}):
            result = dict(echo(runner.load()['reserved/query-build-inputs'])[0], **field)
            problems = runner.run_vector(Scripted(result), 'r', runner.load()['reserved/query-build-inputs'])
            self.assertTrue(any('STS-ENV-4' in p for p in problems), field)
        result = dict(echo(vector)[0], reasons=1)
        self.assertNotEqual(runner.run_vector(Scripted(result), 'r', vector), [])
        self.assertEqual(runner.reserved_problems('get', {'status': 'ok', 'reasons': []}, {}, profiles), [])

    def operation_docs(self):
        text = runner.check_spec.STATUSES.read_text()
        return {op: doc for op, doc, _ in runner.check_spec.table(text, 'The operation table:', 3, '', [])}

    def test_reserved_table(self):
        cs = runner.check_spec
        statuses, reserved = cs.STATUSES.read_text(), cs.RESERVED.read_text()
        cases = [
            (statuses.replace('| `gc_collect` | 16 | `unsupported` |\n', ''), reserved, 'gc_collect has no row'),
            (statuses.replace('| `gc_collect` | 16 |', '| `gc_collect` | 09 |'), reserved, 'gc_collect has no row'),
            (statuses + '', reserved.replace(', `lease_release`', ''), 'lease_release is reserved but not'),
            (statuses, reserved.replace('`aavanam/gc/v1`', '`ext:gc/v1`'), 'not a reserved profile name'),
            (statuses, reserved.replace('`aavanam/gc/v1`', '`aavanam/lease/v1`'), 'listed twice'),
            (statuses, reserved.replace('`gc_collect` |', '`gc_collect`, `semantic_search` |'), 'semantic_search is listed twice'),
        ]
        with tempfile.TemporaryDirectory() as d:
            old = cs.STATUSES, cs.RESERVED
            cs.STATUSES, cs.RESERVED = Path(d) / 's.md', Path(d) / 'r.md'
            try:
                for s, r, want in cases:
                    cs.STATUSES.write_text(s)
                    cs.RESERVED.write_text(r)
                    errors = cs.check_reserved()
                    self.assertTrue(any(want in e for e in errors), (want, errors))
                cs.STATUSES.write_text(statuses)
                cs.RESERVED.write_text(reserved)
                self.assertEqual(cs.check_reserved(), [])
            finally:
                cs.STATUSES, cs.RESERVED = old

    def test_fixture_steps(self):
        cs = runner.check_spec
        with tempfile.TemporaryDirectory() as d:
            folder = Path(d) / 'one'
            folder.mkdir()
            (folder / 'a.txt').write_bytes('\u00b0 beta'.encode())
            base = {'descriptor_profile': 'aavanam/base/v1'}
            (folder / 'fixture.json').write_text(json.dumps({
                'format': 'aavanam-fixture/v1', 'collection': 'c', 'root': 'ex',
                'payloads': {'text': {'media_type': 'text/plain', 'file': 'a.txt'}},
                'descriptors': [{'as': 'ex', 'value': {**base, 'kind': 'excerpt', 'parent': {'$ref': 'ex'},
                                                       'ranges': {'$ranges': 'text', 'text': 'beta'},
                                                       'payload': {'$text': 'beta', 'media_type': 'text/plain'}}}]}))
            own = {'step': 'own'}
            v = cs.with_fixture({'fixture': 'one', 'steps': [own]}, Path(d))
            data = '\u00b0 beta'.encode()
            value = v['steps'][0]['request']['descriptors'][0]['value']
            self.assertEqual(value['ranges'], [{'start': data.find(b'beta'), 'end': len(data)}])
            self.assertEqual(v['payloads'][value['payload']['$payload']]['text'], 'beta')
            self.assertEqual(v['payloads']['text']['text'], data.decode())
            self.assertEqual(v['steps'][-1], own)
            for bad, words in (({'steps': None}, 'own steps'), ({'steps': []}, 'own steps'),
                               ({'steps': [own], 'payloads': []}, 'not a JSON object')):
                with self.assertRaisesRegex(ValueError, words):
                    cs.with_fixture({'fixture': 'one', **bad}, Path(d))
            with self.assertRaisesRegex(ValueError, 'also a payload'):
                cs.with_fixture({'fixture': 'one', 'payloads': {'text': {}}, 'steps': [own]}, Path(d))
            for name in ('two', '../one', 7):
                with self.assertRaisesRegex(ValueError, 'not a folder'):
                    cs.with_fixture({'fixture': name, 'steps': [own]}, Path(d))
            body = json.loads((folder / 'fixture.json').read_text())
            body['descriptors'][0]['value']['producer_config'] = {'$text': 'alpha', 'media_type': 'text/csv'}
            (folder / 'fixture.json').write_text(json.dumps(body))
            v = cs.with_fixture({'fixture': 'one', 'steps': [own]}, Path(d))
            self.assertEqual((v['payloads']['ex#payload']['text'], v['payloads']['ex#producer_config']['text']), ('beta', 'alpha'))
            for ranges, words in (({'$ranges': 'zz', 'text': 'beta'}, 'not valid'), ({'$ranges': 'text', 'text': 'gamma'}, 'one time')):
                body['descriptors'][0]['value']['ranges'] = ranges
                (folder / 'fixture.json').write_text(json.dumps(body))
                with self.assertRaisesRegex(ValueError, words):
                    cs.with_fixture({'fixture': 'one', 'steps': [own]}, Path(d))
            body['payloads']['ex#payload'] = body['payloads'].pop('text')
            body['descriptors'][0]['value']['ranges'] = {'$ranges': 'ex#payload', 'text': 'beta'}
            (folder / 'fixture.json').write_text(json.dumps(body))
            with self.assertRaisesRegex(ValueError, 'also a fixture payload'):
                cs.with_fixture({'fixture': 'one', 'steps': [own]}, Path(d))
            (folder / 'fixture.json').write_text('{"payloads": {}}')
            with self.assertRaisesRegex(ValueError, 'not valid'):
                cs.with_fixture({'fixture': 'one', 'steps': [own]}, Path(d))

    def test_fixture_list(self):
        cs = runner.check_spec
        with tempfile.TemporaryDirectory() as d:
            for name, text in (('one', 'alpha beta'), ('two', 'gamma delta')):
                (Path(d) / name).mkdir()
                (Path(d) / name / 'a.txt').write_text(text)
                (Path(d) / name / 'fixture.json').write_text(json.dumps({
                    'payloads': {'text': {'media_type': 'text/plain', 'file': 'a.txt'}},
                    'descriptors': [
                        {'as': 'src', 'value': {'kind': 'source', 'payload': {'$payload': 'text'}}},
                        {'as': 'ex', 'value': {'kind': 'excerpt', 'parent': {'$ref': 'src'},
                                               'ranges': {'$ranges': 'text', 'text': text.split()[1]},
                                               'payload': {'$text': text.split()[1], 'media_type': 'text/plain'}}}]}))
            own = {'step': 'own'}
            v = cs.with_fixture({'fixture': ['one', 'two'], 'steps': [own]}, Path(d))
            puts = [s['request']['descriptors'][0] for s in v['steps'][:-1]]
            self.assertEqual([p['as'] for p in puts], ['one:src', 'one:ex', 'two:src', 'two:ex'])
            self.assertEqual(puts[0]['value']['payload'], {'$payload': 'one:text'})
            self.assertEqual(puts[3]['value']['parent'], {'$ref': 'two:src'})
            self.assertEqual(puts[3]['value']['ranges'], [{'start': len('gamma '), 'end': len('gamma delta')}])
            self.assertEqual(sorted(v['payloads']), ['one:ex#payload', 'one:text', 'two:ex#payload', 'two:text'])
            self.assertEqual(v['steps'][-1], own)
            for names in (['one'], ['one', 'one'], ['one', 7], []):
                with self.assertRaisesRegex(ValueError, 'two or more different names'):
                    cs.with_fixture({'fixture': names, 'steps': [own]}, Path(d))
            with self.assertRaisesRegex(ValueError, 'not a folder'):
                cs.with_fixture({'fixture': ['one', 'three'], 'steps': [own]}, Path(d))
            with self.assertRaisesRegex(ValueError, 'also a payload'):
                cs.with_fixture({'fixture': ['one', 'two'], 'payloads': {'two:text': {}}, 'steps': [own]}, Path(d))

    def test_fixtures_in_the_repository_pass(self):
        self.assertEqual(runner.check_spec.check_fixtures(), [])

    def test_good_fixture(self):
        self.assertEqual(self.fixture(), [])

    def test_bad_fixtures(self):
        def entry(k):
            return lambda b: b['descriptors'][k]['value']
        cases = [
            (lambda b: b.update(format='x'), 'format'),
            (lambda b: b.pop('root'), 'exactly the fields'),
            (lambda b: entry(1)(b).update(source={'$ref': 'zz'}), 'not bound'),
            (lambda b: entry(0)(b).update(payload={'$payload': 'zz'}), 'not declared'),
            (lambda b: entry(2)(b).update(ranges={'$ranges': 'text', 'text': 'gamma'}), 'one time'),
            (lambda b: entry(2)(b).update(ranges={'$ranges': 'text', 'text': 'a'}), 'one time'),
            (lambda b: entry(0)(b).update(colour='red'), 'not in the schema'),
            (lambda b: entry(1)(b).pop('producer'), 'required field producer'),
            (lambda b: entry(0)(b).update(kind='nothing'), 'unknown kind'),
            (lambda b: entry(0)(b).update(payload={'$payload': 'text', 'fault': 'x'}), 'bad handle'),
            (lambda b: b['descriptors'][3]['value'].update(collection='d'), 'different collection'),
            (lambda b: b.update(root='src'), 'not the handle of a snapshot'),
            (lambda b: b['payloads'].update(more={'media_type': 'text/plain', 'file': 'a.txt'}), 'more is not used'),
            (lambda b: b['payloads'].update(text={'media_type': 'text/plain', 'file': '../a.txt'}), 'not a file'),
            (lambda b: entry(2)(b).update(parent={'$ref': 'src'}), 'not a target'),
            (lambda b: entry(1)(b).update(producer=123), 'not a string'),
            (lambda b: entry(1)(b).update(source=[{'$ref': 'src'}]), 'not a $ref handle'),
            (lambda b: entry(3)(b).update(units=[]), None),
            (lambda b: entry(3)(b).update(schemas={'$ref': 'src'}), 'not a list'),
            (lambda b: entry(2)(b).update(payload={'$text': 'fabricated', 'media_type': 'text/plain'}), 'not the text of $ranges'),
            (lambda b: entry(1)(b).update(source={'$ref': []}), 'string value'),
            (lambda b: b.update(root=[]), 'not the handle of a snapshot'),
            (lambda b: entry(1)(b).update(producer='\ud800'), 'not valid I-JSON Unicode'),
            (lambda b: entry(1)(b).update(ext={'n': float('nan')}), 'I-JSON number'),
            (lambda b: b['descriptors'].insert(1, {'as': 'src2', 'value': dict(entry(0)(b))}) or
             entry(4)(b).update(schemas=[{'$ref': 'src'}, {'$ref': 'src2'}]), 'duplicate'),
            (lambda b: b['payloads'].update(text2={'media_type': 'text/plain', 'file': 'a.txt'}) or
             b['descriptors'].insert(1, {'as': 'src2', 'value': {**entry(0)(b), 'payload': {'$payload': 'text2'}}}) or
             entry(4)(b).update(schemas=[{'$ref': 'src'}, {'$ref': 'src2'}]), 'duplicate'),
            (lambda b: b['payloads']['text'].update(media_type='\ud800'), 'not valid I-JSON Unicode'),
            (lambda b: entry(1)(b).update(logical_id='\uffff'), 'not valid I-JSON Unicode'),
            (lambda b: entry(1)(b).update(logical_id='\ufdd0'), 'not valid I-JSON Unicode'),
            (lambda b: b['descriptors'].insert(3, {'as': 'sup', 'value': {'descriptor_profile': 'aavanam/base/v1', 'kind': 'supersession'}}),
             'cannot use the kind supersession'),
        ]
        for change, words in cases:
            problems = self.fixture(change)
            if words is None:
                self.assertEqual(problems, [])
            else:
                self.assertTrue(any(words in p for p in problems), (words, problems))
        self.assertTrue(any('not a declared payload' in p for p in self.fixture(files={'a.txt': 'alpha beta', 'b.txt': ''})))
        cs = runner.check_spec
        fields = {**cs.schemas()[''], **cs.schemas()['snapshot']}
        plain = {'descriptor_profile': 'aavanam/base/v1', 'kind': 'snapshot', 'collection': 'c', 'n': 1}
        self.assertEqual(cs.canonical_identity(cs.resolve({**plain, 'units': [], 'n': 1.0}, {}, {}, {}), fields),
                         cs.canonical_identity(plain, fields))
        data, ranges = {'p': b'alpha beta', 'q': b'alpha beta'}, lambda n: {'$ranges': n, 'text': 'beta'}
        self.assertEqual(cs.resolve(ranges('p'), {}, {}, data), [{'start': 6, 'end': 10}])
        self.assertEqual(cs.resolve(ranges('p'), {}, {}, data), cs.resolve(ranges('q'), {}, {}, data))
        problems = runner.check_spec.check_fixture_field('d', 'evidence', 'subject_selector', 'selector', '0..1', 7, {}, None, {})
        self.assertIn('cannot use a selector field', problems[0])


class NullAdapterTest(unittest.TestCase):
    def test_null_implementation_fails(self):
        out = io.StringIO()
        with redirect_stdout(out):
            code = runner.main(['--adapter', 'null_adapter:NullAdapter'])
        self.assertEqual(code, 1)
        self.assertNotIn('ERROR', out.getvalue())
        vectors = runner.load()
        passed = [line.split()[1] for line in out.getvalue().splitlines() if line.startswith('PASS')]
        for vid in passed:
            statuses = {s['expect']['status'] for s in vectors[vid]['steps']}
            self.assertEqual(statuses, {'unsupported'}, vid)
        self.assertLess(len(passed), len(vectors))
        self.assertIsInstance(NullAdapter().call('put', {}), dict)


class RenderTest(unittest.TestCase):
    REQUEST = {'context': 'ctx', 'roots': [REF_A]}

    def result(self, *items):
        document = runner.render_document(self.REQUEST, list(items))
        return {'status': 'ok', 'items': list(items), 'document': document,
                'document_id': runner.digest(runner.canonical(document).encode())}

    def test_document_of_the_items(self):
        subject = {'status': 'ok', 'reasons': [], 'entry': 'subject', 'ref': REF_A, 'kind': 'source'}
        gap = {'status': 'ok', 'reasons': [], 'entry': 'gap', 'subject': REF_A, 'gap': 'no_check', 'ref': None}
        good = self.result(subject, gap)
        self.assertEqual(runner.render_problems('render', self.REQUEST, good), [])
        self.assertEqual(good['document']['entries'], sorted(good['document']['entries'], key=runner.canonical))
        self.assertIn('document_id', runner.render_problems('render', self.REQUEST, {**good, 'document_id': REF_A})[0])
        wrong = {**good, 'document': {**good['document'], 'omitted': [REF_A]}}
        self.assertIn('RND-DOC-3', runner.render_problems('render', self.REQUEST, wrong)[0])

    def test_no_added_label(self):
        labelled = {'status': 'ok', 'reasons': [], 'entry': 'subject', 'ref': REF_A, 'kind': 'source', 'verified': True}
        self.assertIn('RND-SCP-5', runner.render_problems('render', self.REQUEST, self.result(labelled))[0])
        gap = {'status': 'ok', 'reasons': [], 'entry': 'gap', 'subject': REF_A, 'gap': 'verified', 'ref': None}
        self.assertIn('RND-SCP-4', runner.render_problems('render', self.REQUEST, self.result(gap))[0])
        self.assertIn('RND-SCP-5', runner.render_problems('render', self.REQUEST, {**self.result(), 'verified_subjects': [REF_A]})[0])
        for bad in ({'entry': []}, {'entry': 'gap', 'gap': []}):
            item = {'status': 'ok', 'reasons': [], **bad}
            self.assertIn('not a string', runner.render_problems('render', self.REQUEST, self.result(item))[0])

    def test_refusal_has_no_document(self):
        self.assertEqual(runner.render_problems('render', self.REQUEST, {'status': 'failed', 'document': None, 'document_id': None}), [])
        self.assertIn('no field document', runner.render_problems('render', self.REQUEST, {'status': 'failed'})[0])
        self.assertIn('RND-DOC-3', runner.render_problems('render', self.REQUEST, {'status': 'failed', 'document': {}})[0])
        labelled = {'status': 'not_found', 'reasons': [], 'entry': 'subject', 'ref': REF_A, 'verified': True}
        refused = {'status': 'not_found', 'items': [labelled], 'document': None, 'document_id': None}
        self.assertIn('RND-DOC-2', runner.render_problems('render', self.REQUEST, refused)[0])

    def test_adapter_cannot_change_the_request(self):
        class Clearing(Scripted):
            def call(self, operation, request, store='writer'):
                request['roots'].clear()  # the document of a changed request
                return super().call(operation, request, store)
        empty = {'context': 'ctx', 'roots': []}
        document = runner.render_document(empty, [])
        result = {**envelope('render', 'ok', []), 'document': document, 'document_id': runner.digest(runner.canonical(document).encode())}
        step = {'operation': 'render', 'request': copy.deepcopy(self.REQUEST), 'expect': {'status': 'ok', 'reasons': [], 'items': []}}
        problems = runner.run_vector(Clearing(result), 't', {'steps': [step]})
        self.assertTrue(any('RND-DOC-3' in p for p in problems), problems)


class PublicationTest(unittest.TestCase):
    SNAP = {'operation': 'create_snapshot', 'request': {'collection': 'c'}, 'as_ref': 'n',
            'expect': {'status': 'ok', 'reasons': [], 'items': [OK_ITEM]}}

    def candidate_step(self):
        return {'operation': 'create_candidate', 'as_ref': 'k',
                'request': {'snapshot': {'$ref': 'n'}, 'checks': [], 'policy': 'p', 'expected_head': None},
                'expect': {'status': 'ok', 'reasons': [], 'items': [OK_ITEM]}}

    def test_snapshot_revision_and_handle(self):
        n = rev({'descriptor_profile': runner.BASE_PROFILE, 'kind': 'snapshot', 'collection': 'c'})
        step = {'operation': 'get', 'request': {'ref': {'$ref': 'n'}}, 'expect': {'status': 'ok', 'reasons': []}}
        adapter = Scripted(envelope('create_snapshot', 'ok', [], [{**OK_ITEM, 'ref': n}]), envelope('get', 'ok', []), change_store=True)
        self.assertEqual(runner.run_vector(adapter, 't', {'steps': [self.SNAP, step]}), [])
        self.assertEqual(adapter.requests[1]['ref'], n)
        problems = runner.run_vector(Scripted(envelope('create_snapshot', 'ok', [], [{**OK_ITEM, 'ref': REF_A}])), 't', {'steps': [self.SNAP]})
        self.assertIn('PUB-SNP-3', problems[0])

    def test_candidate_identity(self):
        n = rev({'descriptor_profile': runner.BASE_PROFILE, 'kind': 'snapshot', 'collection': 'c'})
        record = {'checks': [], 'expected_head': None, 'format': 'aavanam-candidate/v1', 'policy': 'p', 'snapshot': n}
        k = 'sha256:' + hashlib.sha256(json.dumps(record, separators=(',', ':'), sort_keys=True).encode()).hexdigest()
        snap = envelope('create_snapshot', 'ok', [], [{**OK_ITEM, 'ref': n}])
        good = Scripted(snap, envelope('create_candidate', 'ok', [], [{**OK_ITEM, 'candidate': k}]))
        self.assertEqual(runner.run_vector(good, 't', {'steps': [self.SNAP, self.candidate_step()]}), [])
        bad = Scripted(snap, envelope('create_candidate', 'ok', [], [{**OK_ITEM, 'candidate': REF_A}]))
        self.assertIn('PUB-CND-3', runner.run_vector(bad, 't', {'steps': [self.SNAP, self.candidate_step()]})[0])

    def test_acceptance_policy(self):
        seen = {}

        class Accepting(Scripted):
            def accept(self, policy, validate):
                seen['policy'], seen['validate'] = policy, validate

        n = rev({'descriptor_profile': runner.BASE_PROFILE, 'kind': 'snapshot', 'collection': 'c'})
        vector = {'acceptance': {'policy': 'p', 'accept': [[{'$ref': 'n'}, 'r']]}, 'steps': [self.SNAP]}
        self.assertEqual(runner.run_vector(Accepting(envelope('create_snapshot', 'ok', [], [{**OK_ITEM, 'ref': n}])), 't', vector), [])
        self.assertEqual(seen['policy'], 'p')
        self.assertTrue(seen['validate'](n, 'r'))
        self.assertFalse(seen['validate'](n, 'other'))

    def test_gates_before_first_step(self):
        n = rev({'descriptor_profile': runner.BASE_PROFILE, 'kind': 'snapshot', 'collection': 'c'})
        adapter = Scripted(envelope('create_snapshot', 'ok', [], [{**OK_ITEM, 'ref': n}]))
        self.assertEqual(runner.run_vector(adapter, 't', {'gates': ['c', 'd'], 'steps': [self.SNAP]}), [])
        self.assertEqual(adapter.gates, ['c', 'd'])

    def test_gate_error_fails_the_vector(self):
        class NoGate(Scripted):
            def gate(self, collection):
                raise AttributeError('gate')

        problems = runner.run_vector(NoGate(), 't', {'gates': ['c'], 'steps': [self.SNAP]})
        self.assertEqual(len(problems), 1)
        self.assertIn('PUB-GAT-1', problems[0])

    def test_gates_form(self):
        self.assertEqual(runner.check_spec.check_gates('v', ['c']), [])
        for bad in ([], 'c', [''], [1], ['c', 'c']):
            self.assertTrue(runner.check_spec.check_gates('v', bad), bad)

    def test_acceptance_skips_unbound_handles(self):
        seen = {}

        class Accepting(Scripted):
            def accept(self, policy, validate):
                seen['validate'] = validate

        n = rev({'descriptor_profile': runner.BASE_PROFILE, 'kind': 'snapshot', 'collection': 'c'})
        vector = {'acceptance': {'policy': 'p', 'accept': [[{'$ref': 'later'}, 'r'], [{'$ref': 'n'}, 'r']]}, 'steps': [self.SNAP]}
        self.assertEqual(runner.run_vector(Accepting(envelope('create_snapshot', 'ok', [], [{**OK_ITEM, 'ref': n}])), 't', vector), [])
        self.assertTrue(seen['validate'](n, 'r'))
        self.assertFalse(seen['validate']('sha256:' + '0' * 64, 'r'))

    def test_create_must_write(self):
        n = rev({'descriptor_profile': runner.BASE_PROFILE, 'kind': 'snapshot', 'collection': 'c'})
        lazy = Scripted(envelope('create_snapshot', 'ok', [], [{**OK_ITEM, 'ref': n}]), write_created=False)
        self.assertIn('did not write', runner.run_vector(lazy, 't', {'steps': [self.SNAP]})[0])

    def test_validator_calls(self):
        published = {'status': 'ok', 'reasons': [], 'items': [{'status': 'ok', 'reasons': [], 'fields': {'disposition': 'published'}}]}
        repeat = {'status': 'ok', 'reasons': [], 'items': [{'status': 'ok', 'reasons': [], 'fields': {'disposition': 'already_published'}}]}
        request = {'candidate': 'k', 'receipt': 'r'}
        self.assertEqual(runner.validator_problems('publish', request, published, [('k', 'r', True, None)]), [])
        self.assertIn('PUB-RCP-2', runner.validator_problems('publish', request, published, [])[0])
        self.assertIn('pair of the request', runner.validator_problems('publish', request, published, [('k', 'x', True, None)])[0])
        self.assertIn('did not accept', runner.validator_problems('publish', request, published, [('k', 'r', False, None)])[0])
        self.assertIn('PUB-HED-5', runner.validator_problems('publish', request, repeat, [('k', 'r', True, None)])[0])
        self.assertEqual(runner.validator_problems('publish', request, repeat, []), [])
        for code, decision in (('receipt_rejected', True), ('head_moved', False)):
            refusal = {'status': 'incompatible', 'reasons': [code],
                       'items': [{'status': 'incompatible', 'reasons': [code], 'fields': {}}]}
            self.assertIn('PUB-HED-3', runner.validator_problems('publish', request, refusal, [('k', 'r', decision, None)])[0])
            self.assertEqual(runner.validator_problems('publish', request, refusal, [('k', 'r', not decision, None)]), [])
        gated = {'status': 'incompatible', 'reasons': ['activation_required'],
                 'items': [{'status': 'incompatible', 'reasons': ['activation_required'], 'fields': {}}]}
        self.assertEqual(runner.validator_problems('publish', request, gated, []), [])
        self.assertIn('PUB-ACT-3', runner.validator_problems('publish', request, gated, [('k', 'r', True, None)])[0])
        for code in sorted(runner.ACTIVATION_REASONS):
            refusal = {'status': 'incompatible', 'reasons': [code], 'items': [{'status': 'incompatible', 'reasons': [code], 'fields': {}}]}
            self.assertEqual(runner.validator_problems('publish', request, refusal, []), [], code)
        with_act = {**request, 'activation': 'a'}
        self.assertEqual(runner.validator_problems('publish', with_act, published, [('k', 'r', True, 'a')]), [])
        self.assertIn('activation', runner.validator_problems('publish', with_act, published, [('k', 'r', True, None)])[0])
        self.assertIn('activation', runner.validator_problems('publish', request, published, [('k', 'r', True, 'a')])[0])

    def test_complete_refusal_has_no_lock(self):
        complete = runner.complete_operations()
        self.assertTrue({'put', 'create_snapshot', 'create_candidate', 'publish'} <= complete)
        refusal = {'status': 'not_found', 'lock': None, 'bundle': None}
        self.assertEqual(runner.complete_problems('publish', refusal, complete), [])
        self.assertIn('STS-ENV-4', runner.complete_problems('publish', {**refusal, 'lock': REF_A}, complete)[0])
        self.assertEqual(runner.complete_problems('publish', {'status': 'ok', 'lock': REF_A}, complete), [])
        self.assertIn('PUB-HED-5', runner.validator_problems('put', {}, {'status': 'ok', 'reasons': []}, [('k', 'r', True)])[0])

    def test_acceptance_handles_are_checked(self):
        errors = runner.check_spec.check_acceptance('v', {'policy': 'p', 'accept': [[{'$ref': 'k'}, {'$ref': 'r'}]]}, {'k'})
        self.assertEqual(errors, ['v: acceptance handle r is not bound by a step'])
        self.assertEqual(len(runner.check_spec.check_acceptance('v', {'policy': 'p'}, set())), 1)
        bad = runner.check_spec.check_acceptance('v', {'policy': 'p', 'accept': [[{'$ref': 7}, 'r']]}, set())
        self.assertIn('string or a', bad[0])


class StoresTest(unittest.TestCase):
    """A vector over the writer, device and output stores (conformance/README.md)."""

    def three_stores(self):
        """The emitter scenario on the device: its locks go to the output store."""
        vector = copy.deepcopy(runner.load()['scenarios/emitter-handbook'])
        for step in vector['steps']:
            step['store'] = 'device'
            if step['operation'] in runner.LOCK_OPERATIONS:
                step['lock_store'] = 'output'
        return vector

    def test_calls_go_to_the_step_store(self):
        vector = self.three_stores()
        adapter = Scripted(*echo(vector), lock_store='output')
        self.assertEqual(runner.run_vector(adapter, 'v', vector), [])
        fixture = len(adapter.stores) - len(vector['steps'])
        self.assertEqual(adapter.stores, ['writer'] * fixture + ['device'] * len(vector['steps']))
        self.assertGreater(fixture, 0)
        self.assertEqual(adapter.kept['device'] & adapter.kept['output'], set())

    def test_lock_check_reads_the_lock_store(self):
        vector = self.three_stores()
        problems = runner.run_vector(Scripted(*echo(vector), lock_store='device'), 'v', vector)
        self.assertTrue(any('did not have the lock' in p for p in problems), problems)
        for step in vector['steps']:
            step.pop('lock_store', None)
        self.assertEqual(runner.run_vector(Scripted(*echo(vector), lock_store='device'), 'v', vector), [])

    def test_device_lock_leaves_the_device(self):
        vector = self.three_stores()

        class Both(Scripted):
            """Write each device lock to the output store and also to the device store."""

            def call(self, operation, request, store='writer'):
                result = super().call(operation, request, store)
                if operation in runner.LOCK_OPERATIONS and store == 'device':
                    self.state['device'] += 1
                return result

        problems = runner.run_vector(Both(*echo(vector), lock_store='output'), 'v', vector)
        self.assertTrue(any('the device store changed, but the lock goes to the output store' in p for p in problems), problems)

    def test_fresh_store_resets_all_stores(self):
        vector = self.three_stores()
        again = copy.deepcopy(vector['steps'])
        again[0]['fresh_store'] = True
        vector['steps'] += again
        adapter = Scripted(*echo(vector), lock_store='output')
        self.assertEqual(runner.run_vector(adapter, 'v', vector), [])  # each lock is new in the empty output store
        self.assertEqual(adapter.stores[-len(again):], ['device'] * len(again))
        vector['steps'] = vector['steps'][:-len(again)] + again[1:]
        results = echo(vector)
        vector['steps'][len(vector['steps']) - len(again) + 1]['fresh_store'] = True
        with self.assertRaisesRegex(runner.VectorError, 'context handle c is not bound'):
            runner.run_vector(Scripted(*results, lock_store='output'), 'v', vector)

    def test_unchanged_covers_all_stores(self):
        refused = {'status': 'not_found', 'reasons': ['unknown_ref'], 'items': [], 'store_unchanged': True}
        vector = {'steps': [{'operation': 'get', 'store': 'device', 'request': {'refs': [REF_A]}, 'expect': refused}]}

        class Other(Scripted):
            def call(self, operation, request, store='writer'):
                self.state['output'] += 1
                return super().call(operation, request, store)
        result = envelope('get', 'not_found', ['unknown_ref'], [])
        self.assertEqual(runner.run_vector(Scripted(result), 'v', vector), [])
        self.assertIn('the output store changed', runner.run_vector(Other(result), 'v', vector)[0])

    def test_check_spec_store_fields(self):
        vector = self.three_stores()
        vector['steps'][0]['lock_store'] = 'output'
        vector['steps'][1]['store'] = 'disk'
        vector['steps'][2]['store'] = 'writer'
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / 'scenarios').mkdir()
            (Path(d) / 'scenarios' / 'three.json').write_text(json.dumps(vector))
            errors = runner.check_spec.check(Path(d))
        self.assertTrue(any('only execution_inputs and build_inputs have lock_store' in e for e in errors), errors)
        self.assertTrue(any('store is not one of writer, device, output' in e for e in errors), errors)
        self.assertTrue(any('context handle c is bound in the device store, not in the writer store' in e for e in errors), errors)
        vector = self.three_stores()
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / 'scenarios').mkdir()
            (Path(d) / 'scenarios' / 'three.json').write_text(json.dumps(vector))
            self.assertEqual([e for e in runner.check_spec.check(Path(d)) if 'three' in e], [])

    def test_check_spec_device_rules(self):
        vector = self.three_stores()
        lock = next(n for n, s in enumerate(vector['steps']) if 'lock_store' in s)
        del vector['steps'][lock]['lock_store']
        put = {'descriptors': [{'value': {'kind': 'binding-set'}}]}
        vector['steps'].insert(lock, {'operation': 'put', 'store': 'device', 'request': put, 'expect': {
            'status': 'unsupported', 'reasons': ['read_only_store'], 'items': [{'status': 'unsupported', 'reasons': ['read_only_store']}]}})
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / 'scenarios').mkdir()
            (Path(d) / 'scenarios' / 'three.json').write_text(json.dumps(vector))
            errors = runner.check_spec.check(Path(d))
        self.assertTrue(any('the lock of a step on the device store goes to the output store (BUN-IMP-7)' in e for e in errors), errors)
        self.assertTrue(any('a put that gives read_only_store has no items (BUN-IMP-6)' in e for e in errors), errors)


if __name__ == '__main__':
    unittest.main()
