"""The seam test of the chained-log adapter with a released groundgate (PRD-PRO-1 to PRD-PRO-3, issue #7).

The test needs groundgate 0.11.0, the pinned release. CI installs it with a hash from
`tests/requirements-seam.txt` and sets AAVANAM_SEAM=1, so an absent package or a different version fails there.
Without AAVANAM_SEAM=1, an absent package skips this test, because the reference implementation uses only the
standard library (constitution principle 9).
"""
import os
import unittest
from importlib import metadata
from types import SimpleNamespace

from support import Store

from aavanam.chained import ChainedLogStore

PINNED = '0.11.0'
try:
    from groundgate import verify
    from groundgate.canonical import jcs
    from groundgate.store import LOGS, NotFound, StoreError, check_bytes, contract, key, reviewer_key
    from groundgate.store.packets import InputGone, admit_packet, load_packet
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


class SeamTest(unittest.TestCase):
    def setUp(self):
        if verify is None:
            if os.environ.get('AAVANAM_SEAM') == '1':
                self.fail('AAVANAM_SEAM=1 needs groundgate ' + PINNED)
            self.skipTest('groundgate is not installed')
        self.assertEqual(metadata.version('groundgate'), PINNED)

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


if __name__ == '__main__':
    unittest.main()
