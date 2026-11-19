"""The seam test of the activation gate (PUB-GAT-1 to PUB-GAT-3, PUB-ACT-1 to PUB-ACT-5, PUB-HED-3 to PUB-HED-5, issue #4).

A workflow producer activates an update and publishes it to a gated collection. This test checks the store side:

- the head moves only with a valid activation;
- a refused activation leaves the head unchanged and gives the reason;
- a repeat with the same request key gives the earlier outcome.

The activation records come from entelos v0.5.0 at commit bacfb4b6d743803db3127cf75b93620669774bff (issue #94).
The signed records live in `tests/entelos_records/` and `examples/library_hours/`. `resolve` maps the record
to the store descriptor. The shared check_record applies record rules 1 to 8 before resolve checks the manifest.

The content and record contracts map to PUB-ACT-1 as follows.

| Descriptor value | Typed record field and consumer mapping |
|---|---|
| candidate | candidate.ref -> consumer store candidate |
| lock | candidate.ref -> checked manifest -> inputs.lock digest and size -> stored lock |
| base | base -> consumer published snapshot; empty gives no base |
| approval | decision_ref -> store receipt wrapper |
| report | report_ref -> store receipt wrapper |
| partition | partition, checked against the manifest |
| effective_from, effective_until | from, until |
| request_key, producer, runner, approver | the record fields with the same names |

The consumer mapping
--------------------

Each record candidate.ref and base names the SHA-256 of exact manifest bytes.
The manifest lists the content members and inputs.lock, in ascending item_id order. The item names
use descriptor byte digests and sizes. The stored descriptor text is canonical JSON (IDN-CAN-2).
Its byte digest equals its store reference. The manifest identity and store candidate identity stay separate.

resolve first checks record rules 1 to 8. It then checks the manifest hash, format, fields, schema, item order and the record head fields.
It then selects the lock bytes, candidate, published base and receipt wrappers through the typed fields above.
A failed check raises a refusal reason before any store call. A known base must be the consumer head,
except for a repeat of an earlier publication (PUB-HED-5). The guard requires the complete descriptor
from resolve in both callers. No caller can add or replace a dependency after resolve.

Receipt payloads contain decision_ref and report_ref only. Receipt content stays in the producer.
The profile and kind are local constants. These updates remove no member, so removed is absent (PUB-ACT-2).
The collection and partition names agree by fixture design. This is no general mapping of the two head models.

Offline signature verification uses `entelos-verify` from the pinned entelos release with a trusted key set (SIGNED.md).
The test reads the binary path from `AAVANAM_ENTELOS_VERIFY`. If the variable is not set, only the signature tests skip.

Each expected outcome comes from the clauses of document 13, SIGNED.md and ACTIVATION.md, not from a run of the store or the verifier.
"""
from __future__ import annotations

import base64
import copy
import json
import os
from pathlib import Path
import runpy
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

from support import BASE, ROOT, Store, candidate, payload, put_ref

ENTELOS_RECORDS = ROOT / 'tests/entelos_records'
KEYSET = ENTELOS_RECORDS / 'keyset.json'
FIRST_SIGNED = ENTELOS_RECORDS / 'first_signed.json'
SECOND_SIGNED = ENTELOS_RECORDS / 'second_signed.json'
EVENT_SIGNED = ENTELOS_RECORDS / 'event_signed.json'

EXAMPLE_DIR = ROOT / 'examples/library_hours'
EXAMPLE_KEYSET = EXAMPLE_DIR / 'keyset.json'
EXAMPLE_SIGNED = EXAMPLE_DIR / 'first_signed.json'

COLLECTION = 'sample-notices'
POLICY = 'sample-policy'

MAPPING = runpy.run_path(str(ROOT / 'examples/library_hours/activation_mapping.py'))
FIELDS = MAPPING['FIELDS']
check_record = MAPPING['check_record']
json_object = MAPPING['json_object']
manifest_text = MAPPING['manifest_text']
resolve = MAPPING['resolve']
digest = MAPPING['digest']


def load_signed(path: Path) -> dict:
    return json_object(path.read_text(encoding='utf-8'))


def record_of(signed_doc: dict) -> dict:
    raw = base64.b64decode(signed_doc['record'])
    return json_object(raw.decode('utf-8'))


class Seam:
    """A gated collection and a consumer table keyed by the content manifest reference."""

    def __init__(self, records: dict[str, dict] | None = None):
        if records is None:
            records = {
                'first': record_of(load_signed(FIRST_SIGNED)),
                'second': record_of(load_signed(SECOND_SIGNED)),
            }
        self.records = records
        self.record_kind = 'activation'
        self.store = Store()
        self.store.gate(COLLECTION)  # PUB-GAT-1: before the first call
        self.validations = []
        self.store.accept(POLICY, lambda cand, rcpt, act: self.validations.append((cand, rcpt, act)) or True)
        self.acceptance = self.receipt('accepted', 'example/reviewer')
        self.consumer = {'objects': {}, 'candidates': {}, 'published': {}, 'head': None, 'receipts': {}}
        self.manifests, self.stored = {}, {}
        first, second = self.schema('Sample notice, first text.'), self.schema('Sample notice, second text.')
        self.stored = {'snapshot-1': self.snapshot(first), 'snapshot-2': self.snapshot(second),
                       'lock-1': self.lock(first), 'lock-2': self.lock(second)}
        for name in ('first', 'second'):
            record = self.records[name]
            number = '1' if name == 'first' else '2'
            base = '' if name == 'first' else self.records['first']['candidate']['ref']
            text = manifest_text(self.store, self.stored['snapshot-' + number], record['partition'], base,
                                 self.stored['lock-' + number], self.consumer['objects'])
            ref = digest(text.encode('utf-8'))
            assert ref == record['candidate']['ref'], (ref, record['candidate']['ref'])
            self.manifests[ref] = text
            self.prepare_receipts(record)
        self.stored['candidate-1'] = candidate(self.store, self.stored['snapshot-1'], POLICY)
        self.consumer['candidates'][self.records['first']['candidate']['ref']] = self.stored['candidate-1']

    def first_is_head(self) -> None:
        """Make the second store candidate after the first publication (PUB-CND-2)."""
        self.stored['candidate-2'] = candidate(self.store, self.stored['snapshot-2'], POLICY, self.stored['snapshot-1'])
        self.consumer['candidates'][self.records['second']['candidate']['ref']] = self.stored['candidate-2']

    def receipt(self, text: str, producer: str = 'sample-reference-adapter') -> str:
        obj, data = payload(text)
        return put_ref(self.store, {'descriptor_profile': BASE, 'kind': 'receipt', 'producer': producer,
                                    'payload': obj}, data)

    def prepare_receipts(self, record: dict) -> None:
        # Receipt content stays in entelos. The wrappers contain only the typed record references.
        for key in ('decision_ref', 'report_ref'):
            ref = record[key]
            if ref not in self.consumer['receipts']:
                self.consumer['receipts'][ref] = self.receipt(ref)

    def schema(self, text: str) -> str:
        obj, data = payload(text)
        return put_ref(self.store, {'descriptor_profile': BASE, 'kind': 'schema', 'logical_id': 'sample-notice',
                                    'schema_profile': 'sample/schema/v1', 'payload': obj}, data)

    def snapshot(self, member: str) -> str:
        result = self.store.call('create_snapshot', {'collection': COLLECTION, 'schemas': [member]})
        assert result['status'] == 'ok', result
        return result['items'][0]['ref']

    def lock(self, member: str) -> str:
        return put_ref(self.store, {'descriptor_profile': BASE, 'kind': 'build-lock', 'inputs': [member],
                                    'consumer': 'sample-consumer', 'completeness': 'sample/declared/v1'})

    def descriptor(self, record: dict) -> dict:
        return resolve(record, self.record_kind, self.manifests, self.consumer)

    def activate(self, record: dict) -> tuple[dict, str]:
        """Resolve before any write, then publish (PUB-ACT-1, PUB-HED-3)."""
        descriptor = self.descriptor(record)
        activation = put_ref(self.store, descriptor)
        result = self.store.call('publish', {'candidate': descriptor['candidate'], 'receipt': self.acceptance,
                                             'activation': activation})
        if result['status'] == 'ok':
            ref = record['candidate']['ref']
            self.consumer['published'].setdefault(ref, result['items'][0]['head'])
            self.consumer['head'] = result['items'][0]['head']
        return result, activation

    def changed_manifest(self, record: dict, change) -> dict:
        """Give changed manifest bytes their own reference and keep the consumer candidate mapping."""
        out = copy.deepcopy(record)
        old = out['candidate']['ref']
        obj = json.loads(self.manifests[old])
        change(obj)
        text = json.dumps(obj, separators=(',', ':'))
        ref = digest(text.encode('utf-8'))
        self.manifests[ref] = text
        if old in self.consumer['candidates']:
            self.consumer['candidates'][ref] = self.consumer['candidates'][old]
        out['candidate']['ref'] = ref
        return out

    def head(self) -> str | None:
        """Read the head of the collection, or None (CTX-VIW-2)."""
        entry = self.store.call('open_context', {'heads': [COLLECTION]})['items'][0]
        if entry['status'] == 'ok':
            return entry['ref']
        assert [r['code'] for r in entry['reasons']] == ['no_head'], entry
        return None

    def publications(self) -> list[dict]:
        return self.store.call('publications', {'collection': COLLECTION})['items']


def codes(result: dict) -> list[str]:
    return sorted(r['code'] for r in result['items'][0]['reasons'])


class ActivationSeamTest(unittest.TestCase):
    def setUp(self):
        self.signed = {
            'first': load_signed(FIRST_SIGNED),
            'second': load_signed(SECOND_SIGNED),
        }
        self.records = {name: record_of(doc) for name, doc in self.signed.items()}
        self.seam = Seam(self.records)
        self.addCleanup(self.seam.store.close)

    def verify(self, keys_path: Path, signed_path: Path) -> tuple[int, dict]:
        verify = os.environ.get('AAVANAM_ENTELOS_VERIFY')
        if not verify:
            self.skipTest('AAVANAM_ENTELOS_VERIFY is not set')
        if not Path(verify).is_file():
            self.fail(f'AAVANAM_ENTELOS_VERIFY={verify} is not an existing file')
        proc = subprocess.run(
            [verify, '--keys', str(keys_path), str(signed_path)],
            capture_output=True,
            text=True,
            check=False,
        )
        return proc.returncode, json_object(proc.stdout.strip())

    def refused(self, result: dict, status: str, reasons: list[str], head, before: str):
        """A refusal gives the reason, and it changes no head and no other part of the store (PUB-HED-6, STS-ENV-4)."""
        self.assertEqual((result['status'], codes(result)), (status, reasons))
        self.assertEqual(self.seam.head(), head)
        self.assertEqual(self.seam.store.fingerprint(), before)

    def test_signed_records_verify_offline_and_resolve_to_descriptors(self):
        cases = [
            ('first notice update', KEYSET, FIRST_SIGNED),
            ('second notice update', KEYSET, SECOND_SIGNED),
            ('library hours example', EXAMPLE_KEYSET, EXAMPLE_SIGNED),
        ]
        for label, keys_path, signed_path in cases:
            with self.subTest(case=label):
                code, res = self.verify(keys_path, signed_path)
                self.assertEqual(code, 0)
                self.assertEqual(res.get('status'), 'ok')
                self.assertEqual(res['data'].get('record_kind'), 'activation')
                self.assertEqual(res['data'].get('key_status'), 'active')
                doc = load_signed(signed_path)
                rec = record_of(doc)
                self.assertEqual(res['data']['record'], rec)
                check_record(rec, res['data']['record_kind'])
        first_rec = self.records['first']
        descriptor = self.seam.descriptor(first_rec)
        self.assertEqual(descriptor['candidate'], self.seam.stored['candidate-1'])

    def test_refusal_changed_record_byte(self):
        # Positive pair
        code, res = self.verify(KEYSET, FIRST_SIGNED)
        self.assertEqual(code, 0)
        self.assertEqual(res.get('status'), 'ok')
        # Refusal: changed record byte -> record_mismatch, rule ref (SIGNED.md check S8)
        doc = load_signed(FIRST_SIGNED)
        rec_b64 = doc['record']
        doc['record'] = ('B' if rec_b64[0] != 'B' else 'C') + rec_b64[1:]
        with tempfile.NamedTemporaryFile('w', suffix='.json') as f:
            json.dump(doc, f)
            f.flush()
            code, res = self.verify(KEYSET, Path(f.name))
        self.assertEqual(code, 1)
        self.assertEqual(res.get('status'), 'record_mismatch')
        self.assertEqual(res['diagnostic'].get('code'), 'record_mismatch')
        self.assertTrue(res['diagnostic'].get('message', '').startswith('ref:'))

    def test_refusal_changed_signature(self):
        # Positive pair
        code, res = self.verify(KEYSET, FIRST_SIGNED)
        self.assertEqual(code, 0)
        self.assertEqual(res.get('status'), 'ok')
        # Refusal: changed signature -> signature_invalid (SIGNED.md check S11)
        doc = load_signed(FIRST_SIGNED)
        sig = doc['signature']
        doc['signature'] = ('B' if sig[0] != 'B' else 'C') + sig[1:]
        with tempfile.NamedTemporaryFile('w', suffix='.json') as f:
            json.dump(doc, f)
            f.flush()
            code, res = self.verify(KEYSET, Path(f.name))
        self.assertEqual(code, 1)
        self.assertEqual(res.get('status'), 'signature_invalid')
        self.assertEqual(res['diagnostic'].get('code'), 'signature_invalid')

    def test_refusal_key_not_in_key_set(self):
        # Positive pair
        code, res = self.verify(KEYSET, FIRST_SIGNED)
        self.assertEqual(code, 0)
        self.assertEqual(res.get('status'), 'ok')
        # Refusal: unknown key identifier -> unknown_key (SIGNED.md check S9)
        doc = load_signed(FIRST_SIGNED)
        doc['key_id'] = 'ed25519:' + '0' * 64
        with tempfile.NamedTemporaryFile('w', suffix='.json') as f:
            json.dump(doc, f)
            f.flush()
            code, res = self.verify(KEYSET, Path(f.name))
        self.assertEqual(code, 1)
        self.assertEqual(res.get('status'), 'unknown_key')
        self.assertEqual(res['diagnostic'].get('code'), 'unknown_key')

    def test_refusal_revoked_key(self):
        # Positive pair: active key accepts
        code, res = self.verify(KEYSET, FIRST_SIGNED)
        self.assertEqual(code, 0)
        self.assertEqual(res.get('status'), 'ok')
        # Refusal: revoked key in key set -> revoked_key (SIGNED.md check S10)
        keyset = json_object(KEYSET.read_text(encoding='utf-8'))
        keyset['keys'][0]['status'] = 'revoked'
        with tempfile.NamedTemporaryFile('w', suffix='.json') as f_keys, \
                tempfile.NamedTemporaryFile('w', suffix='.json') as f_doc:
            json.dump(keyset, f_keys)
            f_keys.flush()
            json.dump(self.signed['first'], f_doc)
            f_doc.flush()
            code, res = self.verify(Path(f_keys.name), Path(f_doc.name))
        self.assertEqual(code, 1)
        self.assertEqual(res.get('status'), 'revoked_key')
        self.assertEqual(res['diagnostic'].get('code'), 'revoked_key')

    def test_refusal_record_kind_not_activation(self):
        # Positive pair: signed activation record verifies and resolves
        code, res = self.verify(KEYSET, FIRST_SIGNED)
        self.assertEqual(code, 0)
        self.assertEqual(res.get('status'), 'ok')
        self.assertEqual(res['data']['record_kind'], 'activation')
        rec = record_of(self.signed['first'])
        check_record(rec, res['data']['record_kind'])

        # Case 1: valid signed event document verifies with entelos-verify, but consumer refuses
        code, res = self.verify(KEYSET, EVENT_SIGNED)
        self.assertEqual(code, 0)
        self.assertEqual(res.get('status'), 'ok')
        self.assertEqual(res['data']['record_kind'], 'event')
        event_rec = record_of(load_signed(EVENT_SIGNED))
        with self.assertRaisesRegex(ValueError, '^unexpected_record_kind$'):
            check_record(event_rec, res['data']['record_kind'])
        with self.assertRaisesRegex(ValueError, '^unexpected_record_kind$'):
            resolve(event_rec, res['data']['record_kind'], self.seam.manifests, self.seam.consumer)

        # Case 2: signed document with record_kind other -> invalid_signed_record (SIGNED.md check S4)
        doc = load_signed(FIRST_SIGNED)
        doc['record_kind'] = 'other'
        with tempfile.NamedTemporaryFile('w', suffix='.json') as f:
            json.dump(doc, f)
            f.flush()
            code, res = self.verify(KEYSET, Path(f.name))
        self.assertEqual(code, 1)
        self.assertEqual(res.get('status'), 'invalid_signed_record')
        self.assertEqual(res['diagnostic'].get('code'), 'invalid_signed_record')
        self.assertTrue(res['diagnostic'].get('message', '').startswith('schema:'))

    def test_each_record_obeys_the_consumer_rules(self):
        cases = [
            (FIRST_SIGNED, 'first notice record'),
            (SECOND_SIGNED, 'second notice record'),
            (EXAMPLE_SIGNED, 'library hours record'),
        ]
        for path, label in cases:
            with self.subTest(file=str(path), label=label):
                doc = load_signed(path)
                rec = record_of(doc)
                self.assertEqual(set(rec.keys()), FIELDS)
                check_record(rec, doc['record_kind'])

    def test_the_record_references_are_distinct(self):
        seen = set()
        for path in (FIRST_SIGNED, SECOND_SIGNED, EXAMPLE_SIGNED):
            doc = load_signed(path)
            record = record_of(doc)
            for field in ('event_ref', 'source_ref', 'snapshot_ref', 'report_ref', 'decision_ref'):
                with self.subTest(file=str(path), field=field):
                    self.assertNotIn(record[field], seen, 'Each record reference must be distinct across all records.')
                    seen.add(record[field])

    def test_each_descriptor_value_comes_from_resolve(self):
        seam = self.seam
        sentinel = {key: 'sample-' + key for key in ('descriptor_profile', 'kind', 'candidate', 'lock', 'base',
                    'approval', 'report', 'partition', 'effective_from', 'effective_until', 'request_key',
                    'producer', 'runner', 'approver')}
        record = self.records['first']
        expected = copy.deepcopy(sentinel)
        with patch(__name__ + '.resolve', return_value=sentinel) as mapped:
            self.assertIs(seam.descriptor(record), sentinel)
            mapped.assert_called_once_with(record, seam.record_kind, seam.manifests, seam.consumer)
            with patch(__name__ + '.put_ref', return_value='sample-activation') as written, \
                    patch.object(seam.store, 'call', return_value={'status': 'incompatible'}):
                seam.activate(record)
            written.assert_called_once_with(seam.store, expected)
            self.assertEqual(sentinel, expected)
        example = runpy.run_path(str(ROOT / 'examples/library_hours/library_hours.py'))
        activation_of = example['activation_of']
        with patch.dict(activation_of.__globals__, resolve=Mock(return_value=sentinel)), \
                patch.dict(activation_of.__globals__, put=lambda store, descriptor: self.assertIs(descriptor, sentinel)):
            activation_of(seam.store, record, seam.record_kind, seam.manifests, seam.consumer)
            activation_of.__globals__['resolve'].assert_called_once_with(
                record, seam.record_kind, seam.manifests, seam.consumer)
        self.assertEqual(sentinel, expected)

    def test_the_typed_fields_select_each_dependency(self):
        seam = self.seam
        for name, number in (('first', '1'), ('second', '2')):
            record = self.records[name]
            descriptor = seam.descriptor(record)
            ref = record['candidate']['ref']
            manifest = json.loads(seam.manifests[ref])
            item = next(item for item in manifest['items'] if item['item_id'] == 'inputs.lock')
            lock, data = seam.consumer['objects'][item['digest']]
            self.assertEqual((descriptor['lock'], digest(data), len(data)),
                             (seam.stored['lock-' + number], item['digest'], item['size']))
            self.assertEqual(lock, item['digest'])  # the store reference hashes the stored descriptor bytes
            self.assertEqual(descriptor['candidate'], seam.consumer['candidates'][ref])
            self.assertNotEqual(ref, descriptor['candidate'])
            self.assertEqual(descriptor.get('base'), None if number == '1' else seam.stored['snapshot-1'])
            for field, reference in (('approval', 'decision_ref'), ('report', 'report_ref')):
                receipt = seam.store.descriptor(descriptor[field])
                self.assertEqual(receipt['kind'], 'receipt')
                self.assertEqual(seam.store.blob(receipt['payload']['digest'], ref=descriptor[field]).decode(),
                                 record[reference])
            self.assertEqual(seam.activate(record)[0]['status'], 'ok')
            if number == '1':
                seam.first_is_head()

    def resolution_refused(self, record: dict, reason: str):
        before = self.seam.store.fingerprint()
        with patch.object(self.seam.store, 'call', side_effect=AssertionError('store called')) as called:
            with self.assertRaisesRegex(ValueError, '^' + reason + '$'):
                self.seam.activate(record)
            called.assert_not_called()
        self.assertEqual(self.seam.store.fingerprint(), before)

    def test_resolve_checks_record_rules_before_other_checks_or_store_calls(self):
        seam = self.seam
        first = self.records['first']
        expected = resolve(first, seam.record_kind, seam.manifests, seam.consumer)  # positive pair
        self.assertEqual(expected['candidate'], seam.stored['candidate-1'])
        activation_of = runpy.run_path(str(ROOT / 'examples/library_hours/library_hours.py'))['activation_of']
        faults = [
            (None, None, 'unexpected_record_kind'),
            (first, 'event', 'unexpected_record_kind'),
            ([], 'activation', 'json'),
            ({**first, 'format': 'other/1'}, 'activation', 'unsupported_record_format'),
            ({**first, 'owner': 'sample'}, 'activation', 'unknown_field:owner'),
            ({key: value for key, value in first.items() if key != 'candidate'}, 'activation', 'schema'),
            ({**first, 'candidate': None}, 'activation', 'schema'),
            ({**first, 'candidate': {'kind': 'cel-bundle', 'ref': first['candidate']['ref']}},
             'activation', 'candidate_fields'),
            ({**first, 'function': 'content:other/other'}, 'activation', 'subject'),
            ({**first, 'until': '2025-12-31'}, 'activation', 'interval'),
        ]
        before = seam.store.fingerprint()
        for record, kind, reason in faults:
            with self.subTest(reason=reason, record=record), \
                    patch.object(seam.store, 'call', side_effect=AssertionError('store called')) as called:
                # Absent mapping tables must not mask a record rule reason.
                with self.assertRaisesRegex(ValueError, '^' + reason + '$'):
                    resolve(record, kind, None, None)
                with self.assertRaisesRegex(ValueError, '^' + reason + '$'):
                    activation_of(seam.store, record, kind, seam.manifests, seam.consumer)
                called.assert_not_called()
            self.assertEqual(seam.store.fingerprint(), before)
        self.assertEqual(seam.activate(first)[0]['status'], 'ok')

    def test_a_changed_manifest_byte_is_refused(self):
        record = self.records['first']
        self.seam.descriptor(record)  # positive pair
        ref = record['candidate']['ref']
        text = self.seam.manifests[ref]
        self.seam.manifests[ref] = text.replace('sample-notices', 'sample-noticet', 1)
        self.resolution_refused(record, 'candidate_mismatch')
        self.seam.manifests[ref] = text
        self.assertEqual(self.seam.activate(record)[0]['status'], 'ok')

    def test_a_manifest_without_the_lock_is_refused(self):
        first = self.records['first']
        self.seam.descriptor(first)  # positive pair
        record = self.seam.changed_manifest(first, lambda obj: obj.update(
            items=[item for item in obj['items'] if item['item_id'] != 'inputs.lock']))
        self.resolution_refused(record, 'missing_lock')
        self.assertEqual(self.seam.activate(first)[0]['status'], 'ok')

    def test_an_absent_lock_digest_or_wrong_size_is_refused(self):
        first = self.records['first']
        for change in ({'digest': 'sha256:' + '0' * 64}, {'size': 0}):
            with self.subTest(change=change):
                self.seam.descriptor(first)  # positive pair
                def fault(obj):
                    next(item for item in obj['items'] if item['item_id'] == 'inputs.lock').update(change)
                self.resolution_refused(self.seam.changed_manifest(first, fault), 'lock_not_found')
        self.assertEqual(self.seam.activate(first)[0]['status'], 'ok')

    def test_record_and_manifest_head_fields_must_agree(self):
        first = self.records['first']
        self.seam.descriptor(first)  # positive pair
        for field in ('base', 'collection', 'partition'):
            record = copy.deepcopy(first)
            record[field] = 'sha256:' + '0' * 64 if field == 'base' else 'sample-other'
            if field != 'base':
                record['function'] = 'content:' + record['collection'] + '/' + record['partition']
            with self.subTest(field=field):
                self.resolution_refused(record, field + '_mismatch')
        self.assertEqual(self.seam.activate(first)[0]['status'], 'ok')

    def test_an_unknown_or_old_published_base_is_refused(self):
        seam = self.seam
        self.assertEqual(seam.activate(self.records['first'])[0]['status'], 'ok')
        seam.first_is_head()
        second = self.records['second']
        seam.descriptor(second)  # positive pair
        unknown = 'sha256:' + '0' * 64
        record = seam.changed_manifest(second, lambda obj: obj.update(base=unknown))
        record['base'] = unknown
        self.resolution_refused(record, 'base_unknown')
        head = seam.consumer['head']
        seam.consumer['head'] = seam.stored['snapshot-2']
        self.resolution_refused(second, 'base_not_current')
        seam.consumer['head'] = head
        self.assertEqual(seam.activate(second)[0]['status'], 'ok')

    def test_the_manifest_rules_refuse_invalid_text(self):
        first = self.records['first']
        faults = [
            (lambda obj: obj.update(format='other/1'), 'format'),
            (lambda obj: obj.update(format='entelos.content/2'), 'unsupported_content_format'),
            (lambda obj: obj.update(extra=True), 'unknown_field'),
            (lambda obj: obj['items'][0].update(extra=True), 'unknown_field'),
            (lambda obj: obj['items'].reverse(), 'item_order'),
            (lambda obj: obj['items'].append(obj['items'][0]), 'duplicate_item'),
            (lambda obj: obj['items'][0].update(digest='sha256:A'), 'schema'),
            (lambda obj: obj['items'][0].update(size=True), 'schema'),
        ]
        for fault, reason in faults:
            with self.subTest(reason=reason):
                self.seam.descriptor(first)  # positive pair
                self.resolution_refused(self.seam.changed_manifest(first, fault), reason)
        # Duplicate JSON fields are refused even when the candidate hash agrees with the text.
        text = self.seam.manifests[self.records['first']['candidate']['ref']]
        text = text.replace('{', '{"base":"",', 1)
        ref = digest(text.encode())
        record = copy.deepcopy(first)
        record['candidate']['ref'] = ref
        self.seam.manifests[ref] = text
        self.resolution_refused(record, 'json')
        self.assertEqual(self.seam.activate(first)[0]['status'], 'ok')

    def test_the_rule_check_refuses_changed_records(self):
        record = self.records['first']
        check_record(record, 'activation')  # positive pair for each fault below
        faults = [
            ({'format': 'other/1'}, 'unsupported_record_format'),
            ({'owner': 'sample'}, 'unknown_field:owner'),
            ({'candidate': {**record['candidate'], 'digest': 'sample'}}, 'unknown_field:candidate.digest'),
            ({'approver': 7}, 'schema'),
            ({'event_ref': 'sha256:A'}, 'schema'),
            ({'applicability': {'sample': {'region': []}}}, 'schema'),
            ({'candidate': {'kind': 'cel-bundle', 'ref': record['candidate']['ref']}}, 'candidate_fields'),
            ({'function': 'content:other/other'}, 'subject'),
            ({'until': record['from']}, 'interval'),
            ({'until': '2026-02-30'}, 'interval'),
        ]
        for change, reason in faults:
            with self.subTest(change=change), self.assertRaisesRegex(ValueError, '^' + reason + '$'):
                check_record({**copy.deepcopy(record), **change}, 'activation')
        with self.assertRaisesRegex(ValueError, '^unexpected_record_kind$'):
            check_record(record, 'event')
        with self.assertRaisesRegex(ValueError, '^unexpected_record_kind$'):
            check_record(record, None)
        for text in ('[]', '{"candidate":{"ref":"one","ref":"two"}}', '{"value":NaN}'):
            with self.subTest(text=text), self.assertRaisesRegex(ValueError, '^json$'):
                json_object(text)

    def test_the_head_moves_only_with_a_valid_activation(self):
        seam, bound = self.seam, self.seam.stored
        self.assertIsNone(seam.head())
        # PUB-GAT-3: no activation in a gated collection. The store does not call the validator.
        before = seam.store.fingerprint()
        result = seam.store.call('publish', {'candidate': bound['candidate-1'], 'receipt': seam.acceptance})
        self.refused(result, 'incompatible', ['activation_required'], None, before)
        self.assertEqual(seam.validations, [])
        # PUB-HED-3 step 5.3: a valid activation moves the head, and the catalog records it (PUB-LOG-2).
        result, activation = seam.activate(self.records['first'])
        self.assertEqual(result['status'], 'ok', result)
        self.assertEqual((result['items'][0]['disposition'], result['items'][0]['head']), ('published', bound['snapshot-1']))
        self.assertEqual(seam.head(), bound['snapshot-1'])
        self.assertEqual(seam.validations, [(bound['candidate-1'], seam.acceptance, activation)])  # PUB-RCP-2
        # The second update moves the head again, from its base.
        seam.first_is_head()
        result, second = seam.activate(self.records['second'])
        self.assertEqual((result['status'], result['items'][0]['disposition']), ('ok', 'published'))
        self.assertEqual(seam.head(), bound['snapshot-2'])
        log = seam.publications()
        self.assertEqual([(p['activation'], p['previous_head'], p['head']) for p in log],
                          [(activation, None, bound['snapshot-1']), (second, bound['snapshot-1'], bound['snapshot-2'])])

    def test_a_refused_activation_leaves_the_head_and_gives_the_reason(self):
        seam, stored = self.seam, self.seam.stored
        first, second = self.records['first'], self.records['second']
        other = candidate(seam.store, stored['snapshot-2'], POLICY)

        def partition(record):
            out = seam.changed_manifest(record, lambda obj: obj.update(partition='sample-other'))
            out.update(partition='sample-other', function='content:' + COLLECTION + '/sample-other')
            return out

        def lock_of(record, name):
            ref, data = seam.consumer['objects'][stored[name]]
            def fault(obj):
                next(item for item in obj['items'] if item['item_id'] == 'inputs.lock').update(
                    digest=digest(data), size=len(data))
            return seam.changed_manifest(record, fault)

        def gate_refused(record, expected, request_candidate=None):
            descriptor = seam.descriptor(record)
            activation = put_ref(seam.store, descriptor)
            head = seam.head()
            before = seam.store.fingerprint()
            result = seam.store.call('publish', {'candidate': request_candidate or descriptor['candidate'],
                                                 'receipt': seam.acceptance, 'activation': activation})
            self.refused(result, 'incompatible', expected, head, before)

        seam.descriptor(first)  # positive pair for the record and manifest faults
        gate_refused(partition(first), ['activation_mismatch'])
        # A typed candidate.ref selects the other store candidate. The request still names the first.
        changed = seam.changed_manifest(first, lambda obj: obj.update(metadata={'sample': 'other'}))
        seam.consumer['candidates'][changed['candidate']['ref']] = other
        gate_refused(changed, ['activation_mismatch'], stored['candidate-1'])
        gate_refused(lock_of(first, 'lock-2'), ['lock_outside_snapshot'])
        # PUB-ACT-3 check 2: the activation names a base, but the candidate expects no head.
        consumer = copy.deepcopy(seam.consumer)
        try:
            base = self.records['second']['candidate']['ref']
            changed = seam.changed_manifest(first, lambda obj: obj.update(base=base))
            changed['base'] = base
            seam.consumer['published'][base] = stored['snapshot-2']
            seam.consumer['head'] = stored['snapshot-2']
            self.assertIsNone(seam.head())
            gate_refused(changed, ['activation_mismatch'])
        finally:
            seam.consumer = consumer
        self.assertEqual(seam.validations, [])
        self.assertEqual(seam.activate(first)[0]['status'], 'ok')
        seam.first_is_head()
        seam.descriptor(second)  # positive pair
        # This candidate expects no head. The record base selects the published first snapshot.
        changed = seam.changed_manifest(second, lambda obj: obj.update(metadata={'sample': 'old-candidate'}))
        seam.consumer['candidates'][changed['candidate']['ref']] = other
        gate_refused(changed, ['activation_mismatch'])
        gate_refused(lock_of(second, 'lock-1'), ['lock_outside_snapshot'])
        # A stale consumer can have no head, while the store candidate expects the first snapshot.
        # Change record and manifest base together. The store must still apply PUB-ACT-3 check 2.
        for wrong_partition in (False, True):
            record = partition(second) if wrong_partition else second
            changed = seam.changed_manifest(record, lambda obj: obj.update(base=''))
            changed['base'] = ''
            current = seam.consumer['head']
            seam.consumer['head'] = None
            descriptor = seam.descriptor(changed)
            seam.consumer['head'] = current
            activation = put_ref(seam.store, descriptor)
            before = seam.store.fingerprint()
            result = seam.store.call('publish', {'candidate': stored['candidate-2'], 'receipt': seam.acceptance,
                                                 'activation': activation})
            reasons = ['activation_mismatch'] * (2 if wrong_partition else 1)
            self.refused(result, 'incompatible', reasons, current, before)
        self.assertEqual(len(seam.publications()), 1)
        seam.store.accept(POLICY, lambda cand, rcpt, act: False)
        activation = put_ref(seam.store, seam.descriptor(second))
        before = seam.store.fingerprint()
        result = seam.store.call('publish', {'candidate': stored['candidate-2'], 'receipt': seam.acceptance,
                                             'activation': activation})
        self.refused(result, 'incompatible', ['receipt_rejected'], stored['snapshot-1'], before)
        seam.store.accept(POLICY, lambda cand, rcpt, act: True)
        self.assertEqual(seam.activate(second)[0]['status'], 'ok')

    def test_a_repeat_with_the_same_request_key_gives_the_earlier_outcome(self):
        seam, bound = self.seam, self.seam.stored
        first, second = self.records['first'], self.records['second']
        result, activation = seam.activate(first)
        self.assertEqual(result['items'][0]['disposition'], 'published')
        before = seam.store.fingerprint()
        # The same record gives the same activation object, and the earlier outcome (PUB-HED-5). The store does not change.
        again, same = seam.activate(first)
        self.assertEqual(same, activation)
        self.assertEqual((again['status'], again['items'][0]['disposition'], again['items'][0]['head']),
                         ('ok', 'already_published', bound['snapshot-1']))
        self.assertEqual(seam.store.fingerprint(), before)
        self.assertEqual(len(seam.validations), 1)  # no second validator call
        self.assertEqual(len(seam.publications()), 1)  # no second publication (PUB-LOG-2)
        # After the second update, a repeat of the first record does not move the head back.
        seam.first_is_head()
        self.assertEqual(seam.activate(second)[0]['items'][0]['disposition'], 'published')
        late, _ = seam.activate(first)
        self.assertEqual((late['status'], late['items'][0]['disposition'], late['items'][0]['head']),
                         ('ok', 'superseded', bound['snapshot-2']))
        self.assertEqual(seam.head(), bound['snapshot-2'])
        self.assertEqual(len(seam.publications()), 2)

    def test_a_repeat_of_a_refused_record_gives_the_same_refusal(self):
        seam, bound = self.seam, self.seam.stored
        record = seam.changed_manifest(self.records['first'], lambda obj: obj.update(partition='sample-other'))
        record.update(partition='sample-other', function='content:' + COLLECTION + '/sample-other')
        outcomes = []
        for _ in range(2):
            result, activation = seam.activate(record)
            outcomes.append((result['status'], codes(result), activation, seam.head(), seam.store.fingerprint()))
        self.assertEqual(outcomes[0], outcomes[1])
        self.assertEqual(outcomes[0][:2], ('incompatible', ['activation_mismatch']))
        self.assertIsNone(outcomes[0][3])
        self.assertEqual(seam.publications(), [])
        # The valid record with the same request key is then the first publication.
        result, _ = seam.activate(self.records['first'])
        self.assertEqual((result['items'][0]['disposition'], seam.head()), ('published', bound['snapshot-1']))


if __name__ == '__main__':
    unittest.main()
