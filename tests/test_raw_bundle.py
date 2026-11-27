"""The raw bundle form and the delta bundle (BUN-RAW-1 to BUN-RAW-5, BUN-DLT-1 to BUN-DLT-5). The faults are planted."""
from __future__ import annotations

import io
import json
import unittest

from support import BASE, Store, canonical, payload, put, revision

from aavanam.store import raw_form, raw_parts


def codes(result: dict) -> tuple[str, list[str]]:
    return result['status'], [r['code'] for r in result['reasons']]


class OnePiece(io.RawIOBase):
    """A stream that records the size of each read, so a test can see that the import reads piece by piece."""

    def __init__(self, data: bytes):
        self.inner, self.sizes = io.BytesIO(data), []

    def readline(self, size=-1):
        return self.inner.readline()

    def read(self, size=-1):
        self.sizes.append(size)
        return self.inner.read(size)


def artifact(text: str, kind: str = 'sample/document/v1') -> tuple[dict, dict]:
    obj, data = payload(text)
    return {'descriptor_profile': BASE, 'kind': 'artifact', 'artifact_type': kind, 'payload': obj}, data


def unit_of(store: Store, text: str, kind: str = 'sample/document/v1') -> tuple[str, str]:
    """Put an artifact, a BindingSet and a unit. Return the unit and the artifact."""
    doc, data = artifact(text, kind)
    bindings = {'descriptor_profile': BASE, 'kind': 'binding-set', 'bindings': {'doc': revision(doc)}}
    unit = {'descriptor_profile': BASE, 'kind': 'knowledge-unit', 'binding_set': revision(bindings)}
    for desc, more in ((doc, data), (bindings, {}), (unit, {})):
        assert put(store, desc, more)['status'] == 'ok'
    return revision(unit), revision(doc)


def export(store: Store, roots: list[str], **more) -> dict:
    context = store.call('open_context', {'roots': roots})['context']
    result = store.call('export', {'context': context, 'roots': roots, **more})
    assert result['status'] == 'ok', result
    return result


class RawBundleTest(unittest.TestCase):
    def setUp(self):
        self.writer = Store(debug=True)
        self.addCleanup(self.writer.close)
        self.unit, self.doc = unit_of(self.writer, 'one\ntwo')  # a payload with a line feed byte
        self.empty, _ = unit_of(self.writer, '', 'sample/empty/v1')  # a payload with no bytes
        self.bundle = export(self.writer, [self.unit, self.empty])['bundle']
        self.raw = raw_form(self.bundle)

    def device(self) -> Store:
        device = Store(debug=True, role='device')
        self.addCleanup(device.close)
        return device

    def test_the_raw_form_has_the_manifest_line_and_the_payload_bytes(self):  # BUN-RAW-1 to BUN-RAW-3
        line, _, section = self.raw.partition(b'\n')
        manifest = json.loads(line)
        self.assertEqual(manifest['format'], 'aavanam-bundle-raw/v1')
        self.assertEqual({k: manifest[k] for k in ('roots', 'descriptors')}, {k: self.bundle[k] for k in ('roots', 'descriptors')})
        self.assertEqual(sorted(e['size'] for e in manifest['payloads']), [0, len(b'one\ntwo')])
        self.assertEqual(sorted(e['digest'] for e in manifest['payloads']), sorted(self.bundle['payloads']))
        self.assertEqual(section, b'one\ntwo')  # no base64 text and no separator
        self.assertNotIn(b'b25l', self.raw)
        found, data = raw_parts(self.raw)
        self.assertEqual((found, sorted(data.values())), (manifest, [b'', b'one\ntwo']))

    def test_a_raw_import_gives_the_content_of_the_bundle_import(self):  # BUN-RAW-5
        plain, raw = self.device(), self.device()
        expected = plain.call('import', {'bundle': self.bundle})
        result = raw.call('import', self.raw)
        self.assertEqual(result['status'], 'ok')
        self.assertEqual(sorted(i['ref'] for i in result['items']), sorted(i['ref'] for i in expected['items']))
        self.assertEqual(raw.fingerprint(), plain.fingerprint())
        self.assertEqual(raw.call('import', bytearray(self.raw))['status'], 'ok')  # a repeat does not change the store (BUN-UPD-2)
        self.assertEqual(raw.fingerprint(), plain.fingerprint())

    def test_the_import_reads_one_entry_at_a_time(self):  # BUN-RAW-2, BUN-RAW-4
        stream = OnePiece(self.raw)
        device = self.device()
        self.assertEqual(device.call('import', stream)['status'], 'ok')
        sizes = [e['size'] for e in json.loads(self.raw.partition(b'\n')[0])['payloads']]
        self.assertEqual(stream.sizes, sizes + [1])  # the bytes of each entry, then the check for a byte after the last entry

    def test_each_fault_gives_its_reason_and_no_change(self):  # BUN-RAW-4
        line, _, section = self.raw.partition(b'\n')
        text = line.decode()
        entry = next(e for e in json.loads(line)['payloads'] if e['size'])
        one = json.dumps(entry, separators=(',', ':'), sort_keys=True)
        self.assertIn(one, text)

        def manifest(old, new):
            return text.replace(old, new, 1).encode() + b'\n' + section
        flipped = self.raw[:-1] + bytes([self.raw[-1] ^ 1])
        cases = {
            'no line feed byte': (line, 'malformed_json'),
            'a manifest that is not JSON': (b'{' + self.raw, 'malformed_json'),
            'a manifest that is not UTF-8': (b'\xff' + self.raw, 'malformed_json'),
            'a duplicate key': (manifest('{', '{"format":"aavanam-bundle-raw/v1",'), 'duplicate_key'),
            'the bundle format': (manifest('aavanam-bundle-raw/v1', 'aavanam-bundle/v1'), 'field_type'),
            'a manifest that is a list': (b'[]\n' + section, 'field_type'),
            'a field that is not in the manifest': (manifest('{', '{"more":1,'), 'field_type'),
            'a size that is not an integer': (manifest('"size":7', '"size":7.5'), 'field_type'),
            'a size that is a boolean': (manifest('"size":7', '"size":true'), 'field_type'),
            'a negative size': (manifest('"size":7', '"size":-1'), 'field_type'),
            'an entry with a field more': (manifest('"size":7', '"more":1,"size":7'), 'field_type'),
            'two entries with one digest': (manifest(one, one + ',' + one), 'field_type'),
            'a withheld digest that is an entry': (manifest('{', '{"withheld":["%s"],' % entry['digest']), 'field_type'),
            'a size with more bytes than the input': (manifest('"size":7', '"size":1099511627776'), 'field_type'),
            'a size that no read permits': (manifest('"size":7', '"size":1e30'), 'field_type'),
            'bytes with a different digest': (flipped, 'digest_mismatch'),
            'a section that ends early': (self.raw[:-1], 'field_type'),
            'a byte after the last entry': (self.raw + b'\x00', 'field_type'),
        }
        for name, (data, code) in cases.items():
            with self.subTest(name):
                device = Store(role='device')  # no debug: no raw exception crosses the API (STS-MAL-1)
                self.addCleanup(device.close)
                before = device.fingerprint()
                result = device.call('import', data)
                self.assertEqual((codes(result), result['items']), (('failed', [code]), []))
                self.assertEqual(device.fingerprint(), before)

    def test_a_request_that_is_not_an_object_or_a_raw_bundle_fails(self):  # BUN-IMP-1
        device = self.device()
        for request in ('text', 7, None, [self.raw]):
            self.assertEqual(codes(device.call('import', request)), ('failed', ['field_type']))
        self.assertEqual(codes(self.writer.call('export', self.raw)), ('failed', ['field_type']))  # only import takes a raw bundle

    def test_the_descriptor_checks_follow_the_stream_checks(self):  # BUN-RAW-5: the payload bytes step of STS-MAL-3 stays
        doc = json.loads(next(t for t in self.bundle['descriptors'] if '"sample/document/v1"' in t))
        doc['payload']['size'] += 1
        changed = {**self.bundle, 'descriptors': [canonical(doc)], 'roots': [revision(doc)]}
        result = self.device().call('import', raw_form(changed))
        self.assertEqual(codes(result), ('failed', ['size_mismatch']))


class DeltaBundleTest(unittest.TestCase):
    def setUp(self):
        self.writer = Store(debug=True)
        self.addCleanup(self.writer.close)
        self.device = Store(debug=True, role='device')
        self.addCleanup(self.device.close)
        self.old, _ = unit_of(self.writer, 'one')
        self.new, self.new_doc = unit_of(self.writer, 'two', 'sample/note/v1')

    def snapshot(self, *units) -> str:
        result = self.writer.call('create_snapshot', {'collection': 'sample-manual', 'units': list(units)})
        return result['items'][0]['ref']

    def test_a_delta_import_gives_the_content_of_the_full_import(self):  # BUN-DLT-2, BUN-DLT-5
        first, second = self.snapshot(self.old), self.snapshot(self.old, self.new)
        base = self.writer.call('open_context', {'roots': [first]})['context']
        full = export(self.writer, [second])['bundle']
        delta = export(self.writer, [second], base_context=base)['bundle']
        self.assertEqual(delta['base'], [first])
        self.assertEqual(len(delta['descriptors']), 4)  # the snapshot, the unit, its BindingSet and its artifact
        self.assertEqual(len(delta['payloads']), 1)
        self.assertLess(len(json.dumps(delta)), len(json.dumps(full)))
        other = Store(debug=True, role='device')
        self.addCleanup(other.close)
        for store, update in ((self.device, raw_form(delta)), (other, {'bundle': full})):
            self.assertEqual(store.call('import', {'bundle': export(self.writer, [first])['bundle']})['status'], 'ok')
            self.assertEqual(store.call('import', update)['status'], 'ok')
        self.assertEqual(self.device.fingerprint(), other.fingerprint())

    def test_the_base_context_can_be_expired_or_not_a_string(self):  # BUN-DLT-1
        second = self.snapshot(self.old, self.new)
        context = self.writer.call('open_context', {'roots': [second]})['context']
        for value, code in (('ctx:none', 'context_expired'), (7, 'field_type'), (None, 'field_type')):
            result = self.writer.call('export', {'context': context, 'roots': [second], 'base_context': value})
            self.assertEqual((codes(result), result.get('bundle'), result['context']), (('failed', [code]), None, context))

    def test_a_delta_bundle_with_no_base_root_in_the_store_changes_nothing(self):  # BUN-DLT-4, BUN-UPD-6
        first, second = self.snapshot(self.old), self.snapshot(self.old, self.new)
        base = self.writer.call('open_context', {'roots': [first]})['context']
        delta = export(self.writer, [second], base_context=base)['bundle']
        before = self.device.fingerprint()
        for update in ({'bundle': delta}, raw_form(delta)):
            result = self.device.call('import', update)
            self.assertEqual(codes(result), ('failed', ['bundle_incomplete', 'bundle_incomplete']))
            self.assertEqual(self.device.fingerprint(), before)
        for value in ([], 'text', [7], None):  # BUN-MAN-1: `base` is a list of one or more references
            self.assertEqual(codes(self.device.call('import', {'bundle': {**delta, 'base': value}})), ('failed', ['field_type']))

    def test_a_layered_base_context_gives_its_base_snapshot_and_its_layer(self):  # BUN-DLT-1, LYR-OPN-3
        layered = Store(debug=True, profiles=('aavanam/layer/v1',))
        self.addCleanup(layered.close)
        unit, _ = unit_of(layered, 'one')
        added, _ = unit_of(layered, 'two', 'sample/note/v1')
        base = layered.call('create_snapshot', {'collection': 'sample-manual', 'units': [unit]})['items'][0]['ref']
        layer = {'descriptor_profile': 'aavanam/layer/v1', 'kind': 'layer', 'base': 'sample-manual', 'scope': 'tenant-a', 'units': [added]}
        self.assertEqual(put(layered, layer)['status'], 'ok')
        opened = layered.call('open_context', {'roots': [base], 'layer': {'ref': revision(layer), 'scope': 'tenant-a'}})
        self.assertEqual(opened['status'], 'ok', opened)
        result = layered.call('export', {'context': opened['context'], 'roots': [base, revision(layer)], 'base_context': opened['context']})
        self.assertEqual((result['status'], result['items'], result['bundle']['descriptors']), ('ok', [], []))
        self.assertEqual(result['bundle']['base'], sorted([base, revision(layer)]))


if __name__ == '__main__':
    unittest.main()
