"""Check the admitted-facts mapping and both storage closures (ADP-FACT-1 to ADP-FACT-5, ADP-FXT-1 to ADP-FXT-7)."""
import base64
import copy
import hashlib
import json
import unittest

from support import ROOT, Store
from aavanam.admitted import put_admitted_facts, put_export_facts


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

    def read(self, context, ref, selector=None):
        """The descriptor and the selected bytes of one member (CRD-READ-1 to CRD-READ-3)."""
        request = {'context': context, 'ref': ref} | ({'selector': selector} if selector else {})
        found, = self.call('read', **request)['items']
        return found['descriptor'], None if found['data'] is None else base64.b64decode(found['data'])

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
        evidence_root, _ = self.read(context, evidence)
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
            mapping, _ = self.read(context, mapping_ref)
            record, _ = self.read(context, mapping['subject'])
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
            excerpt, data = self.read(context, record['supports'])
            self.assertEqual(excerpt['ranges'], [{'start': byte_range[0], 'end': byte_range[1]}])
            self.assertEqual(data, excerpt_bytes)
            self.assertEqual(self.read(context, excerpt['parent'], record['ext']['citation'])[1], excerpt_bytes)
            reading, data = self.read(context, excerpt['parent'])
            self.assertEqual(reading['coverage_profile'], 'aavanam/coverage/v1')
            self.assertEqual(reading['coverage_profile'], fixture_reading['coverage_profile'])
            self.assertEqual(data, self.reading)
            self.assertEqual(self.read(context, reading['source'])[1], self.source)
            fact = next(f for f in facts if f['field'] == field)
            self.assertEqual(record['ext']['receipt_digest'], fact['receipt_digest'])
            self.assertEqual(json.loads(mapping['native_id']),
                             [fact['document_digest'], fact['text_digest'], fact['receipt_digest'], *span, field, value])
            self.assertNotIn(fact['receipt_digest'], export['payloads'] | evidence_bundle['payloads'])
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
        root, _ = self.read(context, evidence)
        mappings = [self.read(context, ref)[0] for ref in root['native_mappings']]
        subject = self.read(context, mappings[0]['subject'])[0]['subject']
        items = self.call('evidence', context=context, subject=subject)['items']
        self.assertEqual(len(items), 2)
        self.assertEqual({self.read(context, i['ref'])[0]['ext']['receipt_digest'] for i in items},
                         {item['receipt_digest'], second_receipt})

    @unittest.skipUnless(PINNED_EXPORT.is_file(),
                         f'Pending producer {PRODUCER_TAG}. The versioned admitted-facts export is unreleased.')
    def test_the_pinned_producer_export_gives_the_same_citations(self):
        # Supply a normalized export of the synthetic invoice from this exact release.
        self.check_citations(json.loads(PINNED_EXPORT.read_text(encoding='utf-8')))


KINDS = ROOT / 'fixtures/admitted-kinds'
DELIVERY_SERVING = 'sample-delivery-serving'
DELIVERY_EVIDENCE = 'sample-delivery-evidence'
DELIVERY_LOCATOR = 'https://example.org/delivery/0007/note'
# The rows and the cited bytes of each fact come from the fixture definition (fixtures/admitted-kinds).
KIND_ROWS = {
    'weight': [['weight', None, None, '1250', 'kg', 'scalar']],
    'shipped': [['shipped', None, None, '2026-01-05', None, 'date']],
    'line': [['line', '2', 'count', '40', None, 'record'], ['line', '2', 'part', 'bolt', None, 'record'],
             ['line', '2', 'size', 'M8', None, 'record']],
}
CITED = {'weight': b'1250 kg', 'shipped': b'2026-01-05', 'line': b'bolt, 40, M8', 'note': b'Keep dry.'}
COLUMNS = ['field', 'key', 'member', 'value', 'unit', 'kind']


class ExportFactsTest(unittest.TestCase):
    """The optional export extension (ADP-FXT-1 to ADP-FXT-7) over the neutral delivery note."""

    def setUp(self):
        self.store = Store()
        self.addCleanup(self.store.close)
        self.store.section(DELIVERY_SERVING, 'serving')
        self.store.section(DELIVERY_EVIDENCE, 'evidence')
        self.source = (KINDS / 'source.txt').read_bytes()
        self.reading = (KINDS / 'reading.txt').read_bytes()
        self.export = json.loads((EXPORTS / 'kinds.json').read_text(encoding='utf-8'))
        self.declaration = json.loads((EXPORTS / 'kinds-declaration.json').read_text(encoding='utf-8'))

    def put(self, export, declaration=None):
        return put_export_facts(self.store, json.dumps(export), declaration or self.declaration,
                                source=self.source, reading=self.reading, source_media_type='text/plain',
                                source_locator=DELIVERY_LOCATOR, producer='example/document-reader/v1',
                                native_scheme='example/admitted-citation/v1',
                                serving=DELIVERY_SERVING, evidence=DELIVERY_EVIDENCE)

    def call(self, operation, **request):
        result = self.store.call(operation, request)
        self.assertEqual(result['status'], 'ok', result)
        return result

    def citations(self, export, declaration=None):
        """Read each fact back through the store. Return the evidence record of each field and the digest mappings."""
        serving, evidence = self.put(export, declaration)
        context = self.call('open_context', roots=[serving, evidence])['context']
        found = {}
        for mapping_ref in self.store.descriptor(evidence)['native_mappings']:
            mapping = self.store.descriptor(mapping_ref)
            record = self.store.descriptor(mapping['subject'])
            if record['kind'] != 'evidence':
                found[record['kind']] = mapping
                continue
            target = self.store.descriptor(record['subject'])
            citation, = self.call('evidence', context=context, subject=record['subject'])['items']
            self.assertEqual(citation['ref'], mapping['subject'])
            if target['kind'] == 'artifact':
                self.assertEqual(target['artifact_type'], 'aavanam/admitted-facts-export/passage')
                self.assertEqual(self.store.blob(target['payload']['digest']), b'Keep dry.')
                field = 'note'
            else:
                table, = self.call('query_data', context=context, resources=[record['subject']],
                                   metadata=['source_locator'])['items']
                self.assertEqual(table['metadata'], {'source_locator': DELIVERY_LOCATOR})
                field = table['rows'][0]['field']
                self.assertEqual([[row[c] for c in COLUMNS] for row in table['rows']], KIND_ROWS[field])
            excerpt = self.store.descriptor(record['supports'])
            self.assertEqual(self.store.blob(excerpt['payload']['digest']), CITED[field])
            found[field] = record
        return found

    def test_each_kind_reads_back_with_its_citation(self):  # ADP-FXT-3 to ADP-FXT-7
        found = self.citations(self.export)
        self.assertEqual(set(found), {'weight', 'shipped', 'line', 'note', 'source', 'reading'})
        self.assertEqual(found['source']['checked'], self.declaration['source_digest'])
        self.assertEqual(found['reading']['checked'], self.declaration['text_digest'])
        prefix = self.declaration['source_digest']['prefix'].encode('utf-8')
        self.assertEqual(found['source']['native_id'], 'sha256:' + hashlib.sha256(prefix + self.source).hexdigest())
        facts = {fact['field']: fact for fact in self.export['facts']}
        for field in CITED:
            ext, citation = found[field]['ext'], facts[field]['citation']
            self.assertEqual(ext['citation'], {'selector_profile': 'aavanam/bytes/v1', **citation['span']})
            self.assertEqual(ext['receipt_digest'], citation['receipt_sha256'])
            self.assertEqual(ext['url'], citation['url'])
            self.assertEqual(ext['export_identity'], self.export['export_sha256'])
            self.assertEqual(ext.get('locator'), citation.get('locator'))
            self.assertIsNone(self.store.blob(citation['receipt_sha256']))

    def test_code_point_spans_and_plain_digests_give_the_same_values(self):  # ADP-FXT-3, ADP-FXT-5
        text = self.reading.decode('utf-8')
        export = copy.deepcopy(self.export)
        for fact in export['facts']:
            cited = CITED[fact['field']].decode('utf-8')
            start = text.index(cited)
            fact['citation'].update(span={'start': start, 'end': start + len(cited)},
                                    source_sha256='sha256:' + hashlib.sha256(self.source).hexdigest(),
                                    text_sha256='sha256:' + hashlib.sha256(self.reading).hexdigest())
        declaration = self.declaration | {
            'span_unit': 'code-points',
            'source_digest': {'form': 'aavanam/sha256/v1', 'scheme': 'example/source-digest/v1'},
            'text_digest': {'form': 'aavanam/sha256/v1', 'scheme': 'example/text-digest/v1'}}
        found = self.citations(export, declaration)
        for fact in export['facts']:
            self.assertEqual(found[fact['field']]['ext']['citation'],
                             {'selector_profile': 'aavanam/nfc-text/v1', **fact['citation']['span']})

    def test_a_bad_fact_or_declaration_refuses_the_pair_before_any_write(self):  # ADP-FXT-2 to ADP-FXT-5
        before = self.store.fingerprint()
        weight, shipped, line, note = self.export['facts']
        cafe = self.reading.index('é'.encode('utf-8'))

        def fact(base, **change):
            return {**base, **change}

        def cite(base, **change):
            return {**base, 'citation': {**base['citation'], **change}}

        facts = [
            fact(weight, kind='table'), fact(weight, value=None), fact(weight, value=[1250]),
            fact(weight, field=''), fact(weight, key=7), fact(weight, extra=None),
            fact(shipped, value='2026-02-30'), fact(shipped, value='2026-1-5'), fact(shipped, unit='d'),
            fact(line, value={}), fact(line, value={'part': {'name': 'bolt'}}), fact(line, value={'part': None}),
            fact(note, value=''), cite(weight, locator=note['citation']['locator']),
            cite(note, locator={'page': 1}), cite(weight, source='knowledge'), cite(weight, ref='r1'),
            cite(weight, span=None), cite(weight, span={'start': 3, 'end': 3}),
            cite(weight, span={'start': 0, 'end': len(self.reading) + 1}),
            cite(weight, span={'start': cafe + 1, 'end': cafe + 2}),
            cite(weight, source_sha256='sha256:' + '0' * 64), cite(weight, text_sha256='sha256:' + '0' * 64),
            cite(weight, receipt_sha256='bad'), cite(weight, extra=None),
            {k: v for k, v in weight.items() if k != 'citation'},
        ]
        exports = [{**self.export, 'facts': [shipped, bad]} for bad in facts] + [
            {**self.export, 'format': 'facts@2'}, {**self.export, 'extra': None},
            {k: v for k, v in self.export.items() if k != 'export_sha256'}, {**self.export, 'export_sha256': 1}]
        for export in exports:
            with self.subTest(export=export), self.assertRaises(ValueError):
                self.put(export)
        jcs_source = {**self.declaration['text_digest'], 'scheme': 'example/source-digest/v1'}
        for declaration in (self.declaration | {'span_unit': 'utf16'}, self.declaration | {'extra': None},
                            self.declaration | {'identity_member': 'facts'},
                            self.declaration | {'source_digest': {'form': 'example/md5', 'scheme': 'x'}},
                            self.declaration | {'source_digest': jcs_source},
                            self.declaration | {'source_digest': {**self.declaration['source_digest'],
                                                                  'scheme': self.declaration['text_digest']['scheme']}}):
            with self.subTest(declaration=declaration), self.assertRaises(ValueError):
                self.put(self.export, declaration)
        self.assertEqual(self.store.fingerprint(), before)
        self.assertIsNotNone(self.put(self.export))

    def test_facts_that_differ_only_in_url_or_locator_keep_two_citations(self):  # ADP-FXT-7, IDN-NAT-3
        note = self.export['facts'][3]
        moved = {**note, 'citation': {**note['citation'], 'locator': {**note['citation']['locator'], 'page': 2}}}
        linked = {**note, 'citation': {**note['citation'], 'url': 'https://example.org/delivery/0008'}}
        serving, evidence = self.put({**self.export, 'facts': [note, moved, linked]})
        context = self.call('open_context', roots=[serving, evidence])['context']
        artifact, = self.store.descriptor(self.store.descriptor(
            self.store.descriptor(serving)['units'][0])['binding_set'])['bindings'].values()
        self.assertEqual(len(self.call('evidence', context=context, subject=artifact)['items']), 3)

    def test_an_export_with_no_facts_writes_no_records(self):  # ADP-FACT-2
        before = self.store.fingerprint()
        self.assertIsNone(self.put({**self.export, 'facts': []}))
        self.assertEqual(self.store.fingerprint(), before)
