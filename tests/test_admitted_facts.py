"""Check the admitted-facts mapping and both storage closures (ADP-FACT-1 to ADP-FACT-5)."""
import base64
import json
import unittest

from support import ROOT, Store
from aavanam.admitted import put_admitted_facts


INVOICE = ROOT / 'fixtures/admitted-invoice'
EXPORTS = ROOT / 'fixtures/admitted-export'
SERVING = 'sample-invoice-serving'
EVIDENCE = 'sample-invoice-evidence'
LOCATOR = 'https://example.org/invoices/sample'
PRODUCER_TAG = 'v0.14.0'
PINNED_EXPORT = EXPORTS / f'producer-{PRODUCER_TAG}.json'


class AdmittedFactsTest(unittest.TestCase):
    def setUp(self):
        self.store = Store()
        self.addCleanup(self.store.close)
        self.store.section(SERVING, 'serving')
        self.store.section(EVIDENCE, 'evidence')
        self.source = (INVOICE / 'invoice.txt').read_bytes()
        self.reading = (INVOICE / 'reading.txt').read_bytes()
        self.facts = json.loads((EXPORTS / 'invoice.json').read_text(encoding='utf-8'))

    def put(self, facts, **overrides):
        args = dict(source=self.source, reading=self.reading, source_media_type='text/plain',
                    source_locator=LOCATOR, producer='example/document-reader/v1',
                    native_scheme='example/admitted-citation/v1', serving=SERVING, evidence=EVIDENCE)
        return put_admitted_facts(self.store, json.dumps(facts), **(args | overrides))

    def call(self, operation, **request):
        result = self.store.call(operation, request)
        self.assertEqual(result['status'], 'ok', result)
        return result

    def check_citations(self, facts):
        serving, evidence = self.put(facts)
        context = self.call('open_context', roots=[serving, evidence])['context']
        self.assertEqual(self.put(list(reversed(facts))), (serving, evidence))
        export = self.call('export', context=context, roots=[serving])['bundle']
        descriptors = [json.loads(text) for text in export['descriptors']]
        self.assertEqual({d['kind'] for d in descriptors},
                         {'snapshot', 'knowledge-unit', 'binding-set', 'schema',
                          'data-resource', 'overlay-set', 'overlay'})
        self.assertEqual(len(export['payloads']), 3)
        self.assertTrue(all(b'Draft' not in base64.b64decode(p) for p in export['payloads'].values()))

        evidence_bundle = self.call('export', context=context, roots=[evidence])['bundle']
        evidence_root = self.store.descriptor(evidence)
        self.assertEqual(len(evidence_root['native_mappings']), 2)
        self.assertEqual(len(evidence_bundle['payloads']), 7)
        self.assertEqual({json.loads(text)['kind'] for text in evidence_bundle['descriptors']},
                         {'snapshot', 'relation-set', 'native-mapping', 'evidence',
                          'excerpt', 'reading', 'source', 'data-resource', 'schema'})
        expected = {
            'count': (2, b'Count: 2', [7, 15], [11, 19]),
            'code': ('A17', b'Code: A17', [16, 25], [20, 29]),
        }
        seen = set()
        fixture = json.loads((INVOICE / 'fixture.json').read_text(encoding='utf-8'))
        fixture_reading = next(d['value'] for d in fixture['descriptors'] if d['as'] == 'reading')
        for mapping_ref in evidence_root['native_mappings']:
            mapping = self.store.descriptor(mapping_ref)
            record = self.store.descriptor(mapping['subject'])
            table, = self.call('query_data', context=context, resources=[record['subject']],
                               metadata=['source_locator'])['items']
            row, = table['rows']
            field = row['field']
            seen.add(field)
            value, excerpt_bytes, span, byte_range = expected[field]
            self.assertEqual(row['value'], value)
            self.assertIs(type(row['value']), type(value))
            self.assertEqual(table['metadata'], {'source_locator': LOCATOR})
            citation, = self.call('evidence', context=context, subject=record['subject'])['items']
            self.assertEqual(citation['ref'], mapping['subject'])
            self.assertEqual(record['ext']['citation'],
                             {'selector_profile': 'aavanam/nfc-text/v1', 'start': span[0], 'end': span[1]})
            excerpt = self.store.descriptor(record['supports'])
            self.assertEqual(excerpt['ranges'], [{'start': byte_range[0], 'end': byte_range[1]}])
            self.assertEqual(self.store.blob(excerpt['payload']['digest']), excerpt_bytes)
            reading = self.store.descriptor(excerpt['parent'])
            self.assertEqual(reading['coverage_profile'], 'aavanam/coverage/v1')
            self.assertEqual(reading['coverage_profile'], fixture_reading['coverage_profile'])
            self.assertEqual(self.store.blob(reading['payload']['digest']), self.reading)
            source = self.store.descriptor(reading['source'])
            self.assertEqual(self.store.blob(source['payload']['digest']), self.source)
            fact = next(f for f in facts if f['field'] == field)
            self.assertEqual(record['ext']['receipt_digest'], fact['receipt_digest'])
            self.assertEqual(json.loads(mapping['native_id']),
                             [fact['document_digest'], fact['text_digest'], fact['receipt_digest'], *span, field, value])
            self.assertIsNone(self.store.blob(fact['receipt_digest']))
        self.assertEqual(seen, set(expected))

    def test_the_neutral_export_gives_two_typed_facts_and_exact_citations(self):
        self.check_citations(self.facts)

    def test_a_bad_item_refuses_the_pair_before_any_write(self):
        before = self.store.fingerprint()
        bad_digest = 'sha256:' + '0' * 64
        for change in (
                {'document_digest': bad_digest}, {'text_digest': bad_digest}, {'receipt_digest': 'bad'},
                {'span': {'start': 1, 'end': 1}}, {'span': {'start': 2, 'end': 1}},
                {'span': {'start': 0, 'end': len(self.reading)}}, {'span': {'start': False, 'end': 1}},
                {'outcome': 'rejected'}, {'outcome': 'needs_verification'}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.put([self.facts[0], self.facts[1] | change])
            self.assertEqual(self.store.fingerprint(), before)

    def test_no_admitted_items_write_no_records(self):
        before = self.store.fingerprint()
        self.assertIsNone(self.put([]))
        self.assertEqual(self.store.fingerprint(), before)

    def test_two_receipts_for_the_same_fact_give_two_evidence_records(self):
        item = self.facts[0]
        second_receipt = self.facts[1]['receipt_digest']
        serving, evidence = self.put([item, item | {'receipt_digest': second_receipt}])
        context = self.call('open_context', roots=[serving, evidence])['context']
        root = self.store.descriptor(evidence)
        mappings = [self.store.descriptor(ref) for ref in root['native_mappings']]
        subject = self.store.descriptor(mappings[0]['subject'])['subject']
        items = self.call('evidence', context=context, subject=subject)['items']
        self.assertEqual(len(items), 2)
        self.assertEqual({self.store.descriptor(i['ref'])['ext']['receipt_digest'] for i in items},
                         {item['receipt_digest'], second_receipt})

    @unittest.skipUnless(PINNED_EXPORT.is_file(),
                         f'Pending producer {PRODUCER_TAG}. The versioned admitted-facts export is unreleased.')
    def test_the_pinned_producer_export_gives_the_same_citations(self):
        # Supply a normalized export of the synthetic invoice from this exact release.
        self.check_citations(json.loads(PINNED_EXPORT.read_text(encoding='utf-8')))
