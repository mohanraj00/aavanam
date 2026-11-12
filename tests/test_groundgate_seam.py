"""The seam tests of aavanam adapters with a pinned groundgate.

The chained-log adapter (PRD-PRO-1 to PRD-PRO-3, issue #7) and the admitted-facts adapter (ADP-FACT-1 to
ADP-FACT-5, issue #54) use the same pin: the groundgate 0.14.0 release, the first release with the facts@1
export (groundgate#584).
CI installs the pin from `tests/requirements-seam.txt` and sets AAVANAM_SEAM=1, so an absent package or a different pin fails there.
Without AAVANAM_SEAM=1, an absent package skips this test, because the reference implementation uses only the
standard library (constitution principle 9).
"""
import hashlib
import json
import os
import tempfile
import unittest
import unicodedata
from importlib import metadata
from types import SimpleNamespace

from support import ROOT, Store

from aavanam.admitted import put_admitted_facts
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
FACT_FIELDS = {'field', 'key', 'value', 'unit', 'citation'}
CITATION_FIELDS = {'source', 'ref', 'url', 'source_sha256', 'text_sha256', 'span', 'receipt_sha256'}
# The expected facts come from the facts@1 contract (groundgate SPEC 5.1): the canonical value string of each
# admitted decision, and the cited text. The code-point spans are those of the fixture in fixtures/admitted-export.
EXPECTED = {'count': ('2', b'Count: 2', (7, 15)), 'code': ('A17', b'Code: A17', (16, 25))}


def store_key(kind, data):
    """The groundgate store key (groundgate docs/design/store.md, section 4)."""
    return 'sha256:' + hashlib.sha256(b'groundgate-object/1:' + kind.encode('ascii') + b'\0' + data).hexdigest()


def document_digest(text):
    """The groundgate document digest (groundgate SPEC 2.1 and 6)."""
    body = canonical({'text': text}).encode('utf-8')
    return 'sha256:' + hashlib.sha256(b'groundgate/0.12:document\0' + body).hexdigest()


def items_from_export(export, source, reading):
    """Read a facts@1 export into the items of ADP-FACT-1. Refuse each fact that the profile cannot keep.

    The application does this step, because the producer format is not an aavanam format. The export digests use
    groundgate schemes, so check them against the supplied bytes, then give the plain SHA-256 digests.
    The export spans count UTF-8 bytes (groundgate SPEC 2.2). ADP-SEL-4 counts code points.
    """
    if set(export) != {'format', 'packet_sha256', 'facts'} or export['format'] != 'facts@1':
        raise ValueError('the export is not facts@1')
    text = reading.decode('utf-8')
    if unicodedata.normalize('NFC', text) != text:
        raise ValueError('the reading is not NFC')
    items = []
    for fact in export['facts']:
        if not isinstance(fact, dict) or set(fact) != FACT_FIELDS:
            raise ValueError('a fact has different fields')
        citation = fact['citation']
        if not isinstance(citation, dict) or set(citation) != CITATION_FIELDS:
            raise ValueError('a fact has no citation or a citation has different fields')
        if citation['source'] != 'document' or citation['span'] is None:
            raise ValueError('a fact has no document span')
        if fact['key'] is not None or fact['unit'] is not None:
            raise ValueError('the profile has no key or unit column')
        if citation['source_sha256'] != store_key('source@1', source):
            raise ValueError('the source digest differs from the source bytes')
        if citation['text_sha256'] != document_digest(text):
            raise ValueError('the text digest differs from the reading')
        span = citation['span']
        if not isinstance(span, dict) or set(span) != {'start', 'end'}:
            raise ValueError('a span needs a start and an end')
        try:
            start, end = (len(reading[:span[name]].decode('utf-8')) for name in ('start', 'end'))
        except (TypeError, UnicodeDecodeError) as error:
            raise ValueError('a span is not on character boundaries') from error
        items.append({'field': fact['field'], 'value': fact['value'],
                      'document_digest': 'sha256:' + hashlib.sha256(source).hexdigest(),
                      'text_digest': 'sha256:' + hashlib.sha256(reading).hexdigest(),
                      'span': {'start': start, 'end': end}, 'receipt_digest': citation['receipt_sha256']})
    return items


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

    def put(self, items):
        return put_admitted_facts(self.store, json.dumps(items), source=self.source, reading=self.reading,
                                  source_media_type='text/plain', source_locator=LOCATOR,
                                  producer='example/document-reader/v1', native_scheme='example/admitted-citation/v1',
                                  serving=SERVING, evidence=EVIDENCE)

    def call(self, operation, **request):
        result = self.store.call(operation, request)
        self.assertEqual(result['status'], 'ok', result)
        return result

    def test_each_admitted_fact_reads_back_with_its_citation(self):  # ADP-FACT-1 to ADP-FACT-5
        items = items_from_export(self.export, self.source, self.reading)
        self.assertEqual(sorted(item['field'] for item in items), sorted(EXPECTED))
        serving, evidence = self.put(items)
        context = self.call('open_context', roots=[serving, evidence])['context']
        seen = set()
        for mapping_ref in self.store.descriptor(evidence)['native_mappings']:
            record = self.store.descriptor(self.store.descriptor(mapping_ref)['subject'])
            table, = self.call('query_data', context=context, resources=[record['subject']],
                               metadata=['source_locator'])['items']
            row, = table['rows']
            value, cited, (start, end) = EXPECTED[row['field']]
            seen.add(row['field'])
            self.assertEqual(row['value'], value)
            self.assertIs(type(row['value']), str)
            self.assertEqual(table['metadata'], {'source_locator': LOCATOR})
            citation, = self.call('evidence', context=context, subject=record['subject'])['items']
            self.assertEqual(citation['ref'], self.store.descriptor(mapping_ref)['subject'])
            self.assertEqual(record['ext']['citation'],
                             {'selector_profile': 'aavanam/nfc-text/v1', 'start': start, 'end': end})
            excerpt = self.store.descriptor(record['supports'])
            self.assertEqual(self.store.blob(excerpt['payload']['digest']), cited)
            fact = next(f for f in self.export['facts'] if f['field'] == row['field'])
            self.assertEqual(record['ext']['receipt_digest'], fact['citation']['receipt_sha256'])
        self.assertEqual(seen, set(EXPECTED))

    def test_a_fact_without_a_citation_or_a_changed_field_fails(self):
        fact = self.export['facts'][0]
        citation = fact['citation']
        changes = [
            {**self.export, 'format': 'facts@2'},
            {**self.export, 'extra': None},
            {**self.export, 'facts': [{**fact, 'citation': None}]},
            {**self.export, 'facts': [{k: v for k, v in fact.items() if k != 'citation'}]},
            {**self.export, 'facts': [{**fact, 'extra': None}]},
            {**self.export, 'facts': [{**fact, 'unit': 'pcs'}]},
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
                items_from_export(change, self.source, self.reading)


if __name__ == '__main__':
    unittest.main()
