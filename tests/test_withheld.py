"""Deferred checks and concurrent writes (BUN-IMP-9 to BUN-IMP-12). The bundles have planted faults."""
from __future__ import annotations

import base64
import json
import tempfile
import unittest
from pathlib import Path

from support import BASE, Store, canonical, payload, put, revision


def crafted_bundle(claimed: str) -> tuple[dict, str, dict]:
    """A bundle with an excerpt of the reading `abcdef`. The excerpt says that range 0 to 3 has the bytes `claimed`.

    The bytes of the excerpt are withheld, so the import does not do BUN-EXC-3. Return the bundle, the
    excerpt revision and the payloads of a source whose bytes are `claimed`.
    """
    src_obj, src_data = payload('sample source')
    rd_obj, rd_data = payload('abcdef')
    ex_obj, ex_data = payload(claimed)
    src = {'descriptor_profile': BASE, 'kind': 'source', 'payload': src_obj}
    rd = {'descriptor_profile': BASE, 'kind': 'reading', 'source': revision(src), 'producer': 'sample/reader/v1',
          'coverage_profile': 'aavanam/coverage/v1', 'payload': rd_obj}
    ex = {'descriptor_profile': BASE, 'kind': 'excerpt', 'parent': revision(rd), 'ranges': [{'start': 0, 'end': 3}], 'payload': ex_obj}
    bundle = {'format': 'aavanam-bundle/v1', 'roots': [revision(ex)], 'descriptors': [canonical(x) for x in (src, rd, ex)],
              'payloads': {**src_data, **rd_data}, 'withheld': [ex_obj['digest']]}
    other = {'descriptor_profile': BASE, 'kind': 'source', 'logical_id': 'sample-other', 'payload': ex_obj}
    return bundle, revision(ex), {'descriptor': other, 'payloads': ex_data}


def codes(result: dict) -> tuple[str, list[str]]:
    return result['status'], sorted(r['code'] for r in result['reasons'])


class DeferredCheckTest(unittest.TestCase):
    def preexisting_output(self, bundle, excerpt, other):
        """Keep bytes in the output store before the device import (BUN-IMP-12)."""
        device = Store(debug=True, role='device')
        self.addCleanup(device.close)
        self.assertEqual(put(device.output, other['descriptor'], other['payloads'])['status'], 'ok')
        self.assertEqual(device.call('import', {'bundle': bundle})['status'], 'ok')
        note = {'descriptor_profile': BASE, 'kind': 'observation', 'subjects': [excerpt],
                'observation_type': 'sample/note/v1', 'scope': {}, 'values': {}}
        self.assertEqual(put(device.output, note)['status'], 'ok')
        snap = device.output.call('create_snapshot', {'collection': 'sample-output', 'observations': [revision(note)]})
        self.assertEqual(snap['status'], 'ok')
        opened = device.output.call('open_context', {'roots': [snap['items'][0]['ref']]})
        self.assertEqual(opened['status'], 'ok')
        return device, opened['context']

    def test_output_read_refuses_preexisting_bytes_for_a_device_excerpt(self):
        bundle, excerpt, other = crafted_bundle('xyz')
        device, context = self.preexisting_output(bundle, excerpt, other)
        before = (device.fingerprint(), device.output.fingerprint())
        result = device.output.call('read', {'context': context, 'ref': excerpt})
        self.assertEqual(codes(result), ('not_found', ['payload_missing']))  # BUN-IMP-12
        self.assertNotIn('data', result['items'][0])
        self.assertEqual((device.fingerprint(), device.output.fingerprint()), before)

    def test_output_read_accepts_preexisting_bytes_for_a_device_excerpt(self):
        bundle, excerpt, other = crafted_bundle('abc')
        device, context = self.preexisting_output(bundle, excerpt, other)
        before = (device.fingerprint(), device.output.fingerprint())
        result = device.output.call('read', {'context': context, 'ref': excerpt})
        self.assertEqual(codes(result), ('ok', []))
        self.assertEqual(base64.b64decode(result['items'][0]['data']), b'abc')
        self.assertEqual((device.fingerprint(), device.output.fingerprint()), before)

    def test_unrelated_deferred_excerpt_does_not_block_a_trusted_object(self):
        for role in ('writer', 'device'):
            with self.subTest(role=role):
                bundle, excerpt, other = crafted_bundle('xyz')
                parent_digest = json.loads(bundle['descriptors'][1])['payload']['digest']
                del bundle['payloads'][parent_digest]
                bundle['payloads'].update(other['payloads'])
                bundle['withheld'] = [parent_digest]
                store = Store(debug=True, role=role)
                self.addCleanup(store.close)
                target = store.output if role == 'device' else store
                trusted = revision(other['descriptor'])
                self.assertEqual(put(target, other['descriptor'], other['payloads'])['status'], 'ok')
                note = {'descriptor_profile': BASE, 'kind': 'observation', 'subjects': [trusted],
                        'observation_type': 'sample/note/v1', 'scope': {}, 'values': {}}
                self.assertEqual(put(target, note)['status'], 'ok')
                snap = target.call('create_snapshot', {'collection': 'sample-trusted', 'observations': [revision(note)]})
                context = target.call('open_context', {'roots': [snap['items'][0]['ref']]})['context']
                self.assertEqual(store.call('import', {'bundle': bundle})['status'], 'ok')
                ex_note = {**note, 'subjects': [excerpt]}
                self.assertEqual(put(target, ex_note)['status'], 'ok')
                ex_snap = target.call('create_snapshot', {'collection': 'sample-excerpt', 'observations': [revision(ex_note)]})
                ex_context = target.call('open_context', {'roots': [ex_snap['items'][0]['ref']]})['context']
                before = (store.fingerprint(), target.fingerprint())
                result = target.call('read', {'context': context, 'ref': trusted})
                self.assertEqual(codes(result), ('ok', []))  # BUN-IMP-12: only the trusted descriptor
                self.assertEqual(base64.b64decode(result['items'][0]['data']), b'xyz')
                for selector in (None, {'selector_profile': 'aavanam/bytes/v1', 'start': 0, 'end': 3}):
                    request = {'context': ex_context, 'ref': excerpt}
                    if selector is not None:
                        request['selector'] = selector
                    result = target.call('read', request)
                    self.assertEqual(codes(result), ('not_found', ['payload_missing']))
                exported = target.call('export', {'context': context, 'roots': [trusted]})
                self.assertEqual(codes(exported), ('ok', []))
                self.assertEqual(exported['bundle']['payloads'], other['payloads'])
                self.assertEqual((store.fingerprint(), target.fingerprint()), before)
                parent_obj, parent_data = payload('abcdef')
                parent = {'descriptor_profile': BASE, 'kind': 'source', 'payload': parent_obj}
                self.assertEqual(codes(put(target, parent, parent_data)), ('failed', ['excerpt_mismatch']))
                self.assertEqual(codes(target.call('read', {'context': context, 'ref': trusted})), ('ok', []))
                self.assertEqual((store.fingerprint(), target.fingerprint()), before)

    def test_each_output_byte_read_checks_each_device_descriptor(self):
        claimed = '{"columns":["id"],"key":["id"],"rows":[["sample"]]}'
        for valid in (False, True):
            with self.subTest(valid=valid):
                bundle, excerpt, other = crafted_bundle(claimed)
                if valid:
                    reading = json.loads(bundle['descriptors'][1])
                    reading['payload'], reading_data = payload(claimed + ' suffix')
                    ex = json.loads(bundle['descriptors'][2])
                    ex['parent'] = revision(reading)
                    ex['ranges'] = [{'start': 0, 'end': len(claimed)}]
                    bundle['payloads'] = {**payload('sample source')[1], **reading_data}
                    bundle['descriptors'][1:] = [canonical(reading), canonical(ex)]
                    excerpt = revision(ex)
                    bundle['roots'] = [excerpt]
                device, _ = self.preexisting_output(bundle, excerpt, other)
                output = device.output
                obj = other['descriptor']['payload']
                schema_obj, schema_data = payload('sample schema')
                schema = {'descriptor_profile': BASE, 'kind': 'schema', 'schema_profile': 'sample/schema/v1',
                          'payload': schema_obj}
                resource = {'descriptor_profile': BASE, 'kind': 'data-resource', 'data_profile': 'aavanam/table/v1',
                            'schema': revision(schema), 'payload': obj}
                artifact = {'descriptor_profile': BASE, 'kind': 'artifact', 'artifact_type': 'sample/text/v1', 'payload': obj}
                reading = {'descriptor_profile': BASE, 'kind': 'reading', 'source': revision(other['descriptor']),
                           'producer': 'sample/reader/v1', 'coverage_profile': 'aavanam/coverage/v1', 'payload': obj}
                for desc, data in ((schema, schema_data), (resource, {}), (artifact, {}), (reading, {})):
                    self.assertEqual(put(output, desc, data)['status'], 'ok')
                refs = [excerpt, revision(other['descriptor']), revision(resource), revision(artifact), revision(reading)]
                note = {'descriptor_profile': BASE, 'kind': 'observation', 'subjects': sorted(refs),
                        'observation_type': 'sample/note/v1', 'scope': {}, 'values': {}}
                self.assertEqual(put(output, note)['status'], 'ok')
                snap = output.call('create_snapshot', {'collection': 'sample-byte-reads', 'observations': [revision(note)]})
                self.assertEqual(snap['status'], 'ok', snap)
                context = output.call('open_context', {'roots': [snap['items'][0]['ref']]})['context']
                before = (device.fingerprint(), output.fingerprint())
                expected = ('ok', []) if valid else ('not_found', ['payload_missing'])
                for ref in refs:
                    result = output.call('read', {'context': context, 'ref': ref})
                    self.assertEqual(codes(result), expected if ref == excerpt else ('ok', []), result)
                    if valid or ref != excerpt:
                        self.assertEqual(base64.b64decode(result['items'][0]['data']), claimed.encode())
                selectors = [
                    (revision(artifact), {'selector_profile': 'aavanam/bytes/v1', 'start': 0, 'end': 3}),
                    (revision(reading), {'selector_profile': 'aavanam/nfc-text/v1', 'start': 0, 'end': 3}),
                    (revision(resource), {'selector_profile': 'aavanam/row/v1', 'index': 0})]
                selectors.append((excerpt, selectors[0][1]))
                for ref, selector in selectors:
                    result = output.call('read', {'context': context, 'ref': ref, 'selector': selector})
                    self.assertEqual(codes(result), expected if ref == excerpt else ('ok', []), result)
                selector = selectors[0][1]
                selected = output.select(revision(artifact), selector)
                self.assertEqual(selected, claimed[:3].encode())
                compared = output.call('compare', {'context': context, 'before': revision(artifact),
                                                   'after': revision(artifact), 'selector': selector})
                self.assertEqual(codes(compared), ('ok', []))
                compared = output.call('compare', {'context': context, 'before': excerpt,
                                                   'after': revision(artifact), 'selector': selector})
                self.assertEqual(codes(compared), expected)
                query = output.call('query_data', {'context': context, 'resources': [revision(resource)]})
                self.assertEqual(codes(query), ('ok', []))
                self.assertEqual(query['items'][0]['rows'], [{'id': 'sample'}])
                search = output.call('search', {'context': context, 'query': 'sample'})
                self.assertEqual(codes(search), ('ok', []))
                matches = {i['ref'] for i in search['items']}
                self.assertIn(revision(reading), matches)
                self.assertEqual(excerpt in matches, valid)
                text = output.call('search_text', {'context': context, 'query': 'sample'})
                self.assertEqual(codes(text), ('ok', []))
                self.assertEqual([i['ref'] for i in text['items']], [revision(artifact)])
                exported = output.call('export', {'context': context, 'roots': [snap['items'][0]['ref']]})
                self.assertEqual(codes(exported), ('ok', []))
                if valid:
                    self.assertEqual(base64.b64decode(exported['bundle']['payloads'][obj['digest']]), claimed.encode())
                else:
                    self.assertNotIn(obj['digest'], exported['bundle']['payloads'])
                    self.assertIn(obj['digest'], exported['bundle']['withheld'])
                self.assertTrue(all(i['withheld'] == (not valid) for i in exported['items'] if i['ref'] in refs))
                single = output.call('export', {'context': context, 'roots': [revision(artifact)]})
                self.assertEqual(codes(single), ('ok', []))
                self.assertEqual(base64.b64decode(single['bundle']['payloads'][obj['digest']]), claimed.encode())
                self.assertFalse(single['items'][0]['withheld'])
                self.assertEqual((device.fingerprint(), output.fingerprint()), before)

    def test_each_read_path_checks_its_own_deferred_payload(self):
        claimed = '{"columns":["id"],"key":["id"],"rows":[["sample"]]}'
        for valid in (False, True):
            with self.subTest(valid=valid):
                device = Store(debug=True, role='device')
                self.addCleanup(device.close)
                output = device.output
                obj, data = payload(claimed)
                trusted = {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}
                self.assertEqual(put(output, trusted, data)['status'], 'ok')
                deferred_obj = {**obj, 'size': obj['size'] + (0 if valid else 1)}
                schema_obj, schema_data = payload('sample schema')
                schema = {'descriptor_profile': BASE, 'kind': 'schema', 'schema_profile': 'sample/schema/v1',
                          'payload': schema_obj}
                resource = {'descriptor_profile': BASE, 'kind': 'data-resource', 'data_profile': 'aavanam/table/v1',
                            'schema': revision(schema), 'payload': deferred_obj}
                artifact = {'descriptor_profile': BASE, 'kind': 'artifact', 'artifact_type': 'sample/text/v1',
                            'payload': deferred_obj}
                reading = {'descriptor_profile': BASE, 'kind': 'reading', 'source': revision(trusted),
                           'producer': 'sample/reader/v1', 'coverage_profile': 'aavanam/coverage/v1',
                           'payload': deferred_obj}
                refs = [revision(x) for x in (resource, artifact, reading)]
                note = {'descriptor_profile': BASE, 'kind': 'observation', 'subjects': sorted(refs),
                        'observation_type': 'sample/note/v1', 'scope': {}, 'values': {}}
                snap = {'descriptor_profile': BASE, 'kind': 'snapshot', 'collection': 'sample-own-checks',
                        'observations': [revision(note)]}
                bundle = {'format': 'aavanam-bundle/v1', 'roots': [revision(snap)],
                          'descriptors': [canonical(x) for x in (trusted, schema, resource, artifact, reading, note, snap)],
                          'payloads': schema_data, 'withheld': [obj['digest']]}
                self.assertEqual(device.call('import', {'bundle': bundle})['status'], 'ok')
                context = output.call('open_context', {'roots': [revision(snap)]})['context']
                before = (device.fingerprint(), output.fingerprint())
                expected = ('ok', []) if valid else ('not_found', ['payload_missing'])
                selectors = [
                    (revision(resource), {'selector_profile': 'aavanam/row/v1', 'index': 0}),
                    (revision(artifact), {'selector_profile': 'aavanam/bytes/v1', 'start': 0, 'end': 3}),
                    (revision(reading), {'selector_profile': 'aavanam/nfc-text/v1', 'start': 0, 'end': 3})]
                for ref, selector in selectors:
                    for selected in (False, True):
                        request = {'context': context, 'ref': ref}
                        if selected:
                            request['selector'] = selector
                        self.assertEqual(codes(output.call('read', request)), expected)
                self.assertEqual(codes(output.call('read', {'context': context, 'ref': revision(trusted)})), ('ok', []))
                selected = output.select(revision(artifact), selectors[1][1])
                self.assertEqual(selected, claimed[:3].encode() if valid else {
                    'status': 'not_found', 'reasons': [{'code': 'payload_missing', 'refs': [revision(artifact)],
                                                     'capabilities': []}]})
                self.assertEqual(codes(output.call('compare', {'context': context, 'before': revision(artifact),
                                      'after': revision(artifact), 'selector': selectors[1][1]})), expected)
                query = output.call('query_data', {'context': context, 'resources': [revision(resource)]})
                self.assertEqual(codes(query), expected)
                if valid:
                    self.assertEqual(query['items'][0]['rows'], [{'id': 'sample'}])
                search = output.call('search', {'context': context, 'query': 'sample'})
                self.assertEqual(codes(search), ('ok', []))
                self.assertEqual([i['ref'] for i in search['items']], [revision(reading)] if valid else [])
                text = output.call('search_text', {'context': context, 'query': 'sample'})
                self.assertEqual(codes(text), expected)
                if valid:
                    self.assertEqual([i['ref'] for i in text['items']], [revision(artifact)])
                self.assertEqual((device.fingerprint(), output.fingerprint()), before)

    def test_output_read_checks_size_and_digest_of_preexisting_bytes(self):
        for fault in ('size', 'digest', None):
            with self.subTest(fault=fault):
                bundle, excerpt, other = crafted_bundle('abc')
                if fault == 'size':
                    ex = json.loads(bundle['descriptors'][2])
                    ex['payload']['size'] += 1
                    excerpt = revision(ex)
                    bundle['descriptors'][2] = canonical(ex)
                    bundle['roots'] = [excerpt]
                device, context = self.preexisting_output(bundle, excerpt, other)
                if fault == 'digest':
                    # Planted storage fault: these bytes do not have the key of their row.
                    device.output.db.execute('UPDATE blobs SET data = ? WHERE digest = ?',
                                             (b'xyz', bundle['withheld'][0]))
                result = device.output.call('read', {'context': context, 'ref': excerpt})
                self.assertEqual(codes(result), ('not_found', ['payload_missing']) if fault else ('ok', []))

    def test_output_read_waits_for_a_deferred_parent_check(self):
        bundle, excerpt, other = crafted_bundle('abc')
        reading_digest = json.loads(bundle['descriptors'][1])['payload']['digest']
        del bundle['payloads'][reading_digest]
        bundle['payloads'].update(other['payloads'])
        bundle['withheld'] = [reading_digest]  # the excerpt bytes are present in the device store
        device, context = self.preexisting_output(bundle, excerpt, other)
        self.assertEqual(device.deferred_refs(reading_digest), {excerpt, revision(json.loads(bundle['descriptors'][1]))})
        self.assertEqual(device.deferred_refs(other['descriptor']['payload']['digest']), set())
        request = {'context': context, 'ref': excerpt}
        self.assertEqual(codes(device.output.call('read', request)), ('not_found', ['payload_missing']))
        parent_obj, parent_data = payload('abcdef')
        parent = {'descriptor_profile': BASE, 'kind': 'source', 'logical_id': 'sample-parent', 'payload': parent_obj}
        self.assertEqual(codes(put(device.output, parent, parent_data)), ('ok', []))
        self.assertEqual(codes(device.output.call('read', request)), ('ok', []))

    def test_output_read_checks_its_own_deferred_descriptors_against_new_device_bytes(self):
        for claimed in ('xyz', 'abc'):
            with self.subTest(claimed=claimed):
                bundle, excerpt, other = crafted_bundle(claimed)
                device = Store(debug=True, role='device')
                self.addCleanup(device.close)
                self.assertEqual(device.output.call('import', {'bundle': bundle})['status'], 'ok')
                supplied = {'format': 'aavanam-bundle/v1', 'roots': [revision(other['descriptor'])],
                            'descriptors': [canonical(other['descriptor'])], 'payloads': other['payloads']}
                self.assertEqual(device.call('import', {'bundle': supplied})['status'], 'ok')
                self.assertIsNotNone(device.blob(bundle['withheld'][0], ref=revision(other['descriptor'])))
                self.assertEqual(device.output.blob(bundle['withheld'][0], ref=excerpt), None if claimed == 'xyz' else b'abc')

    def test_each_store_waits_for_the_parent_check_before_it_serves_excerpt_bytes(self):
        for role in ('writer', 'device'):
            with self.subTest(role=role):
                bundle, excerpt, other = crafted_bundle('abc')
                reading_digest = json.loads(bundle['descriptors'][1])['payload']['digest']
                del bundle['payloads'][reading_digest]
                bundle['payloads'].update(other['payloads'])
                bundle['withheld'] = [reading_digest]
                note = {'descriptor_profile': BASE, 'kind': 'observation', 'subjects': [excerpt],
                        'observation_type': 'sample/note/v1', 'scope': {}, 'values': {}}
                snap = {'descriptor_profile': BASE, 'kind': 'snapshot', 'collection': 'sample-parent',
                        'observations': [revision(note)]}
                bundle['descriptors'] += [canonical(note), canonical(snap)]
                bundle['roots'] = [revision(snap)]
                store = Store(debug=True, role=role)
                self.addCleanup(store.close)
                self.assertEqual(store.call('import', {'bundle': bundle})['status'], 'ok')
                context = store.call('open_context', {'roots': [revision(snap)]})['context']
                self.assertEqual(codes(store.call('read', {'context': context, 'ref': excerpt})),
                                 ('not_found', ['payload_missing']))
                self.assertEqual(store.raw_blob(other['descriptor']['payload']['digest']), b'abc')

    def check(self, store: Store, target: Store, claimed: str, expected: tuple[str, list[str]]) -> None:
        bundle, excerpt, other = crafted_bundle(claimed)
        imported = store.call('import', {'bundle': bundle})
        self.assertEqual(imported['status'], 'ok', imported)  # BUN-IMP-9 skips BUN-EXC-3
        before = target.fingerprint()
        result = put(target, other['descriptor'], other['payloads'])
        self.assertEqual(codes(result), expected)
        if expected[0] != 'ok':
            self.assertEqual(codes(result['items'][0])[1], expected[1])
            self.assertEqual(target.fingerprint(), before)  # STS-ENV-4
            self.assertIsNone(target.raw_blob(bundle['withheld'][0]))  # the bytes stay out of the store
            self.assertIsNotNone(target.descriptor(excerpt))

    def test_writer_refuses_bytes_that_do_not_agree_with_a_withheld_excerpt(self):
        store = Store(debug=True)
        self.addCleanup(store.close)
        self.check(store, store, 'xyz', ('failed', ['excerpt_mismatch']))  # BUN-IMP-10

    def test_writer_accepts_bytes_that_agree_with_a_withheld_excerpt(self):
        store = Store(debug=True)
        self.addCleanup(store.close)
        self.check(store, store, 'abc', ('ok', []))

    def test_output_store_refuses_bytes_that_do_not_agree_with_a_device_excerpt(self):
        device = Store(debug=True, role='device')
        self.addCleanup(device.close)
        self.check(device, device.output, 'xyz', ('failed', ['excerpt_mismatch']))  # BUN-IMP-8, BUN-IMP-10

    def test_output_store_accepts_bytes_that_agree_with_a_device_excerpt(self):
        device = Store(debug=True, role='device')
        self.addCleanup(device.close)
        self.check(device, device.output, 'abc', ('ok', []))

    def test_bytes_of_a_wrong_size_are_refused(self):
        store = Store(debug=True)
        self.addCleanup(store.close)
        bundle, _, other = crafted_bundle('abc')
        text = json.loads(bundle['descriptors'][2])
        text['payload']['size'] = 4  # the descriptor claims a size that the bytes do not have
        bundle['descriptors'][2] = canonical(text)
        bundle['roots'] = [revision(text)]
        self.assertEqual(store.call('import', {'bundle': bundle})['status'], 'ok')
        self.assertEqual(codes(put(store, other['descriptor'], other['payloads'])), ('failed', ['size_mismatch']))

    def test_parent_bytes_that_an_earlier_import_withheld_are_in_the_record(self):
        for claimed in ('xyz', 'abc'):
            with self.subTest(claimed=claimed):
                store = Store(debug=True)
                self.addCleanup(store.close)
                bundle, _, other = crafted_bundle(claimed)
                reading = json.loads(bundle['descriptors'][1])['payload']['digest']
                first = {**bundle, 'roots': [revision(json.loads(bundle['descriptors'][1]))],
                         'descriptors': bundle['descriptors'][:2], 'withheld': [reading]}
                first['payloads'] = {k: v for k, v in bundle['payloads'].items() if k != reading}
                self.assertEqual(store.call('import', {'bundle': first})['status'], 'ok')
                # The parent descriptor is in the store, but its bytes are still absent (BUN-IMP-10).
                second = {**bundle, 'descriptors': bundle['descriptors'][2:], 'payloads': {}}
                self.assertEqual(store.call('import', {'bundle': second})['status'], 'ok')
                self.assertEqual(codes(put(store, other['descriptor'], other['payloads'])), ('ok', []))
                rd_obj, rd_data = payload('abcdef')
                parent = {'descriptor_profile': BASE, 'kind': 'source', 'logical_id': 'sample-parent', 'payload': rd_obj}
                before = store.fingerprint()
                expected = ('failed', ['excerpt_mismatch']) if claimed == 'xyz' else ('ok', [])
                self.assertEqual(codes(put(store, parent, rd_data)), expected)
                if claimed == 'xyz':
                    self.assertEqual(store.fingerprint(), before)  # STS-ENV-4

    def test_output_store_refuses_bytes_for_a_descriptor_of_the_device_store(self):
        src_obj, src_data = payload('xyz')
        rd_obj, rd_data = payload('abcdef')
        src = {'descriptor_profile': BASE, 'kind': 'source', 'payload': src_obj}
        rd = {'descriptor_profile': BASE, 'kind': 'reading', 'source': revision(src), 'producer': 'sample/reader/v1',
              'coverage_profile': 'aavanam/coverage/v1', 'payload': rd_obj}
        ex = {'descriptor_profile': BASE, 'kind': 'excerpt', 'parent': revision(rd), 'ranges': [{'start': 0, 'end': 3}], 'payload': src_obj}
        bundle = {'format': 'aavanam-bundle/v1', 'roots': [revision(ex)], 'descriptors': [canonical(x) for x in (src, rd, ex)],
                  'payloads': rd_data, 'withheld': [src_obj['digest']]}
        device = Store(debug=True, role='device')
        self.addCleanup(device.close)
        self.assertEqual(device.call('import', {'bundle': bundle})['status'], 'ok')
        before = device.output.fingerprint()
        result = put(device.output, src, src_data)  # the device store already has this descriptor (BUN-IMP-8, BUN-IMP-10)
        self.assertEqual(codes(result), ('failed', ['excerpt_mismatch']))
        self.assertEqual(codes(result['items'][0])[1], ['excerpt_mismatch'])
        self.assertEqual(device.output.fingerprint(), before)
        self.assertIsNone(device.output.raw_blob(src_obj['digest']))

    def test_direct_put_and_deferred_put_give_the_same_reason(self):
        for claimed, expected in (('xyz', ('failed', ['excerpt_mismatch'])), ('abc', ('ok', []))):
            with self.subTest(claimed=claimed):
                bundle, _, other = crafted_bundle(claimed)
                direct, deferred = Store(debug=True), Store(debug=True)
                self.addCleanup(direct.close)
                self.addCleanup(deferred.close)
                result = direct.call('put', {'descriptors': bundle['descriptors'],
                                            'payloads': {**bundle['payloads'], **other['payloads']}})
                self.assertEqual(codes(result), expected)  # BUN-EXC-3
                self.assertEqual(deferred.call('import', {'bundle': bundle})['status'], 'ok')
                self.assertEqual(codes(put(deferred, other['descriptor'], other['payloads'])), expected)  # BUN-IMP-10

    def test_output_batch_keeps_failure_attribution_for_existing_descriptors(self):
        for claimed in ('xyz', 'abc'):
            with self.subTest(claimed=claimed):
                bundle, _, other = crafted_bundle(claimed)
                source = json.loads(bundle['descriptors'][0])
                source['payload'] = other['descriptor']['payload']
                reading = json.loads(bundle['descriptors'][1])
                reading['source'] = revision(source)
                excerpt = json.loads(bundle['descriptors'][2])
                excerpt['parent'] = revision(reading)
                bundle['descriptors'] = [canonical(x) for x in (source, reading, excerpt)]
                bundle['roots'] = [revision(excerpt)]
                bundle['payloads'] = payload('abcdef')[1]
                device = Store(debug=True, role='device')
                self.addCleanup(device.close)
                self.assertEqual(device.call('import', {'bundle': bundle})['status'], 'ok')
                before = device.output.fingerprint()
                extra = {'descriptor_profile': BASE, 'kind': 'binding-set'}
                result = device.output.call('put', {
                    'descriptors': [canonical(x) for x in (source, other['descriptor'], extra)],
                    'payloads': other['payloads']})
                if claimed == 'xyz':
                    self.assertEqual(codes(result), ('failed', ['excerpt_mismatch', 'excerpt_mismatch']))
                    self.assertEqual([codes(i) for i in result['items']], [
                        ('failed', ['excerpt_mismatch']), ('failed', ['excerpt_mismatch']),
                        ('failed', ['batch_aborted'])])  # BUN-IMP-10: use the original batch
                    self.assertEqual(device.output.fingerprint(), before)
                    self.assertIsNone(device.output.raw_blob(bundle['withheld'][0]))
                else:
                    self.assertEqual(codes(result), ('ok', []))
                    self.assertEqual([i['status'] for i in result['items']], ['ok'] * 3)
                    self.assertEqual(device.output.raw_blob(bundle['withheld'][0]), b'abc')
                self.assertIsNone(device.raw_blob(bundle['withheld'][0]))

    def test_import_rechecks_excerpt_bytes_in_the_transaction(self):
        for claimed in ('xyz', 'abc'):
            for role in ('writer', 'device'):
                with self.subTest(claimed=claimed, role=role):
                    bundle, excerpt, other = crafted_bundle(claimed)
                    result, store, before = self.raced_import(bundle, other['payloads'], role)
                    expected = ('failed', ['excerpt_mismatch']) if claimed == 'xyz' else ('ok', [])
                    self.assertEqual(codes(result), expected)  # BUN-IMP-11
                    if claimed == 'xyz':
                        self.assertEqual(result['items'], [])
                        self.assertEqual(store.fingerprint(), before)
                        self.assertIsNone(store.descriptor(excerpt))
                    else:
                        self.assertIsNotNone(store.descriptor(excerpt))

    def test_import_rechecks_payload_size_in_the_transaction(self):
        for size in (99, 3):
            for role in ('writer', 'device'):
                with self.subTest(size=size, role=role):
                    obj, data = payload('abc')
                    source = {'descriptor_profile': BASE, 'kind': 'source', 'payload': {**obj, 'size': size}}
                    bundle = {'format': 'aavanam-bundle/v1', 'roots': [revision(source)],
                              'descriptors': [canonical(source)], 'payloads': {}, 'withheld': [obj['digest']]}
                    result, store, before = self.raced_import(bundle, data, role)
                    expected = ('failed', ['size_mismatch']) if size == 99 else ('ok', [])
                    self.assertEqual(codes(result), expected)  # BUN-IMP-11
                    if size == 99:
                        self.assertEqual(store.fingerprint(), before)
                        self.assertIsNone(store.descriptor(revision(source)))
                    else:
                        self.assertEqual(store.descriptor(revision(source))['payload']['size'], 3)

    def test_import_rechecks_payload_digest_in_the_transaction(self):
        for data in (b'xyz', b'abc'):
            with self.subTest(data=data):
                obj, _ = payload('abc')
                source = {'descriptor_profile': BASE, 'kind': 'source', 'payload': obj}
                bundle = {'format': 'aavanam-bundle/v1', 'roots': [revision(source)],
                          'descriptors': [canonical(source)], 'payloads': {}, 'withheld': [obj['digest']]}
                result, store, before = self.raced_import(bundle, {obj['digest']: base64.b64encode(data)}, 'writer')
                expected = ('failed', ['digest_mismatch']) if data == b'xyz' else ('ok', [])
                self.assertEqual(codes(result), expected)
                if data == b'xyz':
                    self.assertEqual(store.fingerprint(), before)
                    self.assertIsNone(store.descriptor(revision(source)))

    def test_deferred_checks_survive_a_later_open(self):
        for claimed in ('xyz', 'abc'):
            with self.subTest(claimed=claimed), tempfile.TemporaryDirectory() as folder:
                path = str(Path(folder) / 'store.db')
                store = Store(path, debug=True)
                bundle, _, other = crafted_bundle(claimed)
                try:
                    self.assertEqual(store.call('import', {'bundle': bundle})['status'], 'ok')
                finally:
                    store.close()
                reopened = Store(path, debug=True)
                try:
                    before = reopened.fingerprint()
                    expected = ('failed', ['excerpt_mismatch']) if claimed == 'xyz' else ('ok', [])
                    self.assertEqual(codes(put(reopened, other['descriptor'], other['payloads'])), expected)
                    if claimed == 'xyz':
                        self.assertEqual(reopened.fingerprint(), before)
                finally:
                    reopened.close()

    def raced_import(self, bundle, encoded, role):
        """Add bytes through a second connection after the first checks, before the write transaction."""
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        path = str(Path(folder.name) / 'store.db')
        store, other = Store(path, debug=True, role=role), Store(path, debug=True)
        self.addCleanup(store.close)
        self.addCleanup(other.close)
        write, states = store.write, []

        def add_then_write(objects, blobs, *args, **kwargs):
            for key in encoded:
                self.assertIsNone(store.raw_blob(key))
            other.write({}, {k: base64.b64decode(v) for k, v in encoded.items()})
            states.append(store.fingerprint())
            return write(objects, blobs, *args, **kwargs)

        store.write = add_then_write  # private test replacement, no public API hook
        result = store.call('import', {'bundle': bundle})
        self.assertEqual(len(states), 1)
        return result, store, states[0]


if __name__ == '__main__':
    unittest.main()
