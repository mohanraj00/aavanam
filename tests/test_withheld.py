"""The deferred checks of withheld bytes (BUN-IMP-9, BUN-IMP-10). A vector cannot make a bundle with a false excerpt."""
from __future__ import annotations

import json
import unittest

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
        store = Store()
        self.check(store, store, 'xyz', ('failed', ['excerpt_mismatch']))  # BUN-IMP-10

    def test_writer_accepts_bytes_that_agree_with_a_withheld_excerpt(self):
        store = Store()
        self.check(store, store, 'abc', ('ok', []))

    def test_output_store_refuses_bytes_that_do_not_agree_with_a_device_excerpt(self):
        device = Store(role='device')
        self.check(device, device.output, 'xyz', ('failed', ['excerpt_mismatch']))  # BUN-IMP-8, BUN-IMP-10
        device.close()

    def test_output_store_accepts_bytes_that_agree_with_a_device_excerpt(self):
        device = Store(role='device')
        self.check(device, device.output, 'abc', ('ok', []))
        device.close()

    def test_bytes_of_a_wrong_size_are_refused(self):
        store = Store()
        bundle, _, other = crafted_bundle('abc')
        text = json.loads(bundle['descriptors'][2])
        text['payload']['size'] = 4  # the descriptor claims a size that the bytes do not have
        bundle['descriptors'][2] = canonical(text)
        bundle['roots'] = [revision(text)]
        self.assertEqual(store.call('import', {'bundle': bundle})['status'], 'ok')
        self.assertEqual(codes(put(store, other['descriptor'], other['payloads'])), ('failed', ['size_mismatch']))


if __name__ == '__main__':
    unittest.main()
