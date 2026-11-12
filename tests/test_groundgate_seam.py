"""The seam tests of aavanam adapters with a pinned groundgate.

The chained-log adapter (PRD-PRO-1 to PRD-PRO-3, issue #7) and the admitted-facts adapter (ADP-FACT-1 to
ADP-FACT-5, issue #54) use the same pin: the groundgate 0.14.0 release, the first release with the facts@1
export (groundgate#584).
CI installs the pin from `tests/requirements-seam.txt` and sets AAVANAM_SEAM=1, so an absent package or a different pin fails there.
Without AAVANAM_SEAM=1, an absent package skips this test, because the reference implementation uses only the
standard library (constitution principle 9).
"""
import json
import os
import tempfile
import unittest
from importlib import metadata
from types import SimpleNamespace

from support import ROOT, Store

from aavanam.admitted import put_export_facts
from aavanam.chained import ChainedLogStore
from aavanam.jsontext import canonical

PINNED = '0.14.0'
try:
    from groundgate import verify
    from groundgate.canonical import jcs
    from groundgate.store import LOGS, NotFound, StoreError, check_bytes, contract, key, reviewer_key
    from groundgate.store import FolderStore
    from groundgate.store.export import admitted_facts
    from groundgate.store.packets import InputGone, admit_packet, load_packet, put_reading
except ImportError:
    verify = None

# A synthetic invoice. The value and the evidence span are in the text.
TEXT = 'Invoice 0001 for sample parts.\nTotal due: 1250 units.\n'
SCHEMA = {'fields': {'total': {'type': 'number'}}}
START = TEXT.index('1250')
CANDIDATES = [{'id': 'c1', 'field': 'total', 'value': '1250',
               'evidence': {'start': START, 'end': START + 4, 'text': '1250'}}]


def adapter():
    rules = SimpleNamespace(key=key, check_bytes=check_bytes, jcs=jcs, LOGS=LOGS, NotFound=NotFound, StoreError=StoreError)
    return ChainedLogStore(Store(), 'groundgate', rules)


def require_pin(test):
    if verify is None:
        if os.environ.get('AAVANAM_SEAM') == '1':
            test.fail('AAVANAM_SEAM=1 needs groundgate ' + PINNED)
        test.skipTest('groundgate is not installed')
    test.assertEqual(metadata.version('groundgate'), PINNED)


class SeamTest(unittest.TestCase):
    def setUp(self):
        require_pin(self)

    def test_the_adapter_passes_the_store_contract(self):  # PRD-PRO-1
        for check in contract.CHECKS:
            with self.subTest(check.__name__):
                check(adapter)

    def test_a_run_writes_through_the_adapter_and_verifies(self):  # PRD-PRO-2
        store = adapter()
        packet_key, receipt = admit_packet(store, TEXT, SCHEMA, CANDIDATES, None, None, None)
        store.append(('answers', packet_key, reviewer_key('sample reviewer')), {'answer': 'confirm'})
        packet = load_packet(store, packet_key)
        self.assertEqual(packet.receipt, receipt.to_dict())
        result = verify(packet.receipt, packet.text, packet.schema, packet.candidates, **packet.inputs())
        self.assertTrue(result.ok, result.problems)
        self.assertEqual([r['answer'] for r in store.read(('answers', packet_key, reviewer_key('sample reviewer')))],
                         ['confirm'])

    def test_a_forget_of_the_text_gives_the_removal_record(self):  # PRD-RMV-1, PRD-PRO-3
        store = adapter()
        packet_key, _ = admit_packet(store, TEXT, SCHEMA, CANDIDATES, None, None, None)
        text_key = key('text@1', TEXT.encode('utf-8'))
        store.forget(text_key, 'the tenant asked')
        with self.assertRaises(InputGone) as gone:
            load_packet(store, packet_key)
        self.assertEqual(gone.exception.key, text_key)
        self.assertEqual(gone.exception.deleted['reason'], 'the tenant asked')
        self.assertRegex(gone.exception.deleted['time'], r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$')



# The neutral invoice of the admitted-facts fixture. invoice.txt is not NFC. reading.txt is its NFC text.
INVOICE = ROOT / 'fixtures/admitted-invoice'
INVOICE_SCHEMA = {'fields': {'count': {'type': 'integer'}, 'code': {'type': 'string'}, 'draft': {'type': 'integer'}}}
INVOICE_CANDIDATES = [
    {'id': 'a', 'field': 'count', 'value': 2, 'evidence': {'text': 'Count: 2'}},
    {'id': 'b', 'field': 'code', 'value': 'A17', 'evidence': {'text': 'Code: A17'}},
    # The evidence does not contain the value, so groundgate rejects it and the export omits it.
    {'id': 'c', 'field': 'draft', 'value': 98, 'evidence': {'text': 'Draft: 99'}},
]
SERVING = 'sample-invoice-serving'
EVIDENCE = 'sample-invoice-evidence'
LOCATOR = 'https://example.org/invoices/sample'
# The reader declaration of facts@1 (ADP-FXT-2). The digest forms are those of groundgate SPEC 6 and
# docs/design/store.md, section 4. The spans count UTF-8 bytes (groundgate SPEC 2.2). It is data, not glue code.
DECLARATION = {
    'format': 'facts@1', 'identity_member': 'packet_sha256', 'span_unit': 'utf8-bytes',
    'source_digest': {'form': 'aavanam/prefixed-sha256/v1', 'scheme': 'example/source-object/v1',
                      'prefix': 'groundgate-object/1:source@1\0'},
    'text_digest': {'form': 'aavanam/prefixed-jcs-sha256/v1', 'scheme': 'example/document-digest/v1',
                    'prefix': 'groundgate/0.12:document\0', 'member': 'text'},
}
# The expected facts come from the facts@1 contract (groundgate SPEC 5.1): the canonical value string of each
# admitted decision, and the cited text. The byte spans are those of the fixture in fixtures/admitted-export.
EXPECTED = {'count': ('2', b'Count: 2', (11, 19)), 'code': ('A17', b'Code: A17', (20, 29))}


class ExportSeamTest(unittest.TestCase):
    """The groundgate export of admitted facts through the admitted-facts adapter (issue #54)."""

    def setUp(self):
        require_pin(self)
        self.source = (INVOICE / 'invoice.txt').read_bytes()
        self.reading = (INVOICE / 'reading.txt').read_bytes()
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        path = os.path.join(folder.name, 'invoice.txt')
        with open(path, 'wb') as file:
            file.write(self.source)
        with FolderStore(os.path.join(folder.name, 'store')) as store:
            reading, _ = put_reading(store, path)
            packet_key, _ = admit_packet(store, self.reading.decode('utf-8'), INVOICE_SCHEMA, INVOICE_CANDIDATES,
                                         None, reading=reading)
            packet = load_packet(store, packet_key)
            result = verify(packet.receipt, packet.text, packet.schema, packet.candidates, **packet.inputs())
            self.assertTrue(result.ok, result.problems)
            self.export = admitted_facts(store, packet)
        self.store = Store()
        self.addCleanup(self.store.close)
        self.store.section(SERVING, 'serving')
        self.store.section(EVIDENCE, 'evidence')

    def put(self, export):
        return put_export_facts(self.store, json.dumps(export), DECLARATION, source=self.source, reading=self.reading,
                                source_media_type='text/plain', source_locator=LOCATOR,
                                producer='example/document-reader/v1', native_scheme='example/admitted-citation/v1',
                                serving=SERVING, evidence=EVIDENCE)

    def call(self, operation, **request):
        result = self.store.call(operation, request)
        self.assertEqual(result['status'], 'ok', result)
        return result

    def test_each_admitted_fact_reads_back_with_its_citation(self):  # ADP-FACT-5, ADP-FXT-1 to ADP-FXT-7
        serving, evidence = self.put(self.export)
        context = self.call('open_context', roots=[serving, evidence])['context']
        seen = set()
        for mapping_ref in self.store.descriptor(evidence)['native_mappings']:
            record = self.store.descriptor(self.store.descriptor(mapping_ref)['subject'])
            if record['kind'] != 'evidence':
                continue
            table, = self.call('query_data', context=context, resources=[record['subject']],
                               metadata=['source_locator'])['items']
            row, = table['rows']
            value, cited, (start, end) = EXPECTED[row['field']]
            seen.add(row['field'])
            self.assertEqual(row['value'], value)
            self.assertIs(type(row['value']), str)
            self.assertEqual((row['key'], row['member'], row['unit'], row['kind']), (None, None, None, 'scalar'))
            self.assertEqual(table['metadata'], {'source_locator': LOCATOR})
            citation, = self.call('evidence', context=context, subject=record['subject'])['items']
            self.assertEqual(citation['ref'], self.store.descriptor(mapping_ref)['subject'])
            self.assertEqual(record['ext']['citation'],
                             {'selector_profile': 'aavanam/bytes/v1', 'start': start, 'end': end})
            excerpt = self.store.descriptor(record['supports'])
            self.assertEqual(self.store.blob(excerpt['payload']['digest']), cited)
            fact = next(f for f in self.export['facts'] if f['field'] == row['field'])
            self.assertEqual(record['ext']['receipt_digest'], fact['citation']['receipt_sha256'])
            self.assertEqual(record['ext']['export_identity'], self.export['packet_sha256'])
        self.assertEqual(seen, set(EXPECTED))

    def test_a_fact_without_a_citation_or_a_changed_field_fails(self):
        fact = self.export['facts'][0]
        citation = fact['citation']
        before = self.store.fingerprint()
        changes = [
            {**self.export, 'format': 'facts@2'},
            {**self.export, 'extra': None},
            {**self.export, 'facts': [{**fact, 'citation': None}]},
            {**self.export, 'facts': [{k: v for k, v in fact.items() if k != 'citation'}]},
            {**self.export, 'facts': [{**fact, 'extra': None}]},
            {**self.export, 'facts': [{**fact, 'kind': 'table'}]},
            {**self.export, 'facts': [{**fact, 'kind': 'date'}]},
            {**self.export, 'facts': [{**fact, 'citation': {**citation, 'span': None}}]},
            {**self.export, 'facts': [{**fact, 'citation': {**citation, 'source': 'knowledge'}}]},
            {**self.export, 'facts': [{**fact, 'citation': {**citation, 'extra': None}}]},
            {**self.export, 'facts': [{**fact, 'citation': {k: v for k, v in citation.items() if k != 'url'}}]},
            {**self.export, 'facts': [{**fact, 'citation': {**citation, 'source_sha256': 'sha256:' + '0' * 64}}]},
            {**self.export, 'facts': [{**fact, 'citation': {**citation, 'text_sha256': 'sha256:' + '0' * 64}}]},
            # The byte 4 is inside the two bytes of the NFC e with acute.
            {**self.export, 'facts': [{**fact, 'citation': {**citation, 'span': {'start': 4, 'end': 9}}}]},
        ]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.put(change)
        self.assertEqual(self.store.fingerprint(), before)

    def test_a_key_and_a_unit_give_their_columns(self):  # ADP-FXT-4, ADP-FXT-6
        # The pinned release gives null for both. The facts@1 contract permits a string for each.
        fact = self.export['facts'][0]
        serving, _ = self.put({**self.export, 'facts': [{**fact, 'key': 'k1', 'unit': 'pcs'}]})
        context = self.call('open_context', roots=[serving])['context']
        resource, = self.store.descriptor(self.store.descriptor(
            self.store.descriptor(serving)['units'][0])['binding_set'])['bindings'].values()
        table, = self.call('query_data', context=context, resources=[resource], where={'unit': 'pcs'})['items']
        row, = table['rows']
        self.assertEqual((row['field'], row['key'], row['value'], row['unit']), (fact['field'], 'k1', fact['value'], 'pcs'))


if __name__ == '__main__':
    unittest.main()
