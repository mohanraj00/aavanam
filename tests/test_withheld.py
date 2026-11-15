"""Deferred checks and concurrent writes (BUN-IMP-9 to BUN-IMP-11). The bundles have planted faults."""
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
            self.assertIsNone(target.blob(bundle['withheld'][0]))  # the bytes stay out of the store
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
        self.assertIsNone(device.output.blob(src_obj['digest']))

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
                    self.assertIsNone(device.output.blob(bundle['withheld'][0]))
                else:
                    self.assertEqual(codes(result), ('ok', []))
                    self.assertEqual([i['status'] for i in result['items']], ['ok'] * 3)
                    self.assertEqual(device.output.blob(bundle['withheld'][0]), b'abc')
                self.assertIsNone(device.blob(bundle['withheld'][0]))

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
                self.assertIsNone(store.blob(key))
            other.write({}, {k: base64.b64decode(v) for k, v in encoded.items()})
            states.append(store.fingerprint())
            return write(objects, blobs, *args, **kwargs)

        store.write = add_then_write  # private test replacement, no public API hook
        result = store.call('import', {'bundle': bundle})
        self.assertEqual(len(states), 1)
        return result, store, states[0]


if __name__ == '__main__':
    unittest.main()
