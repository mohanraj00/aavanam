"""Read a synthetic PDF data sheet with groundgate, and keep the admitted facts in aavanam snapshots.

Run it from a clean environment that has the aavanam wheel and the pinned groundgate (examples/pdf_facts/README.md):

    python3 -I examples/pdf_facts/pdf_facts.py

The example does the steps of issue #71:

1. groundgate reads the PDF, admits three values and rejects one value that the PDF does not contain.
2. The admitted-facts adapter keeps the facts@1 export in a serving snapshot and an evidence snapshot
   (ADP-FXT-1 to ADP-FXT-7).
3. The example publishes the serving snapshot. Then it reads each fact back with the `read` operation
   (CRD-READ-1 to CRD-READ-3). It prints the value and its citation: the page, the span, the cited text,
   the source digest and the text digest.
4. Version 2 of the PDF changes one value. The example publishes a second snapshot, and shows that the
   first snapshot gives the same facts as before.

A fact with no citation stops the example with exit status 1.
"""
from __future__ import annotations

import base64
import hashlib
import json
import os
import sys
import tempfile
from pathlib import Path

from aavanam import Store
from aavanam.admitted import put_export_facts
from groundgate import verify
from groundgate.store import FolderStore
from groundgate.store.export import admitted_facts
from groundgate.store.packets import admit_packet, load_packet, put_reading

HERE = Path(__file__).resolve().parent
SERVING = 'sample-datasheet-serving'
EVIDENCE = 'sample-datasheet-evidence'
POLICY = 'example/pdf-facts-policy'
SCHEMA = {'fields': {'flow': {'type': 'integer'}, 'pressure': {'type': 'integer'},
                     'weight': {'type': 'integer'}, 'noise': {'type': 'integer'}}}
# The reader declaration of facts@1 (ADP-FXT-2). It is the declaration of tests/test_groundgate_seam.py.
DECLARATION = {
    'format': 'facts@1', 'identity_member': 'packet_sha256', 'span_unit': 'utf8-bytes',
    'source_digest': {'form': 'aavanam/prefixed-sha256/v1', 'scheme': 'example/source-object/v1',
                      'prefix': 'groundgate-object/1:source@1\0'},
    'text_digest': {'form': 'aavanam/prefixed-jcs-sha256/v1', 'scheme': 'example/document-digest/v1',
                    'prefix': 'groundgate/0.12:document\0', 'member': 'text'},
}
ROW = {'selector_profile': 'aavanam/row/v1', 'index': 0}


def candidates(flow: int) -> list[dict]:
    """The candidate values of a model. The noise value is not in the PDF, so groundgate rejects it."""
    return [
        {'id': 'c1', 'field': 'flow', 'value': flow, 'evidence': {'text': f'Flow rate: {flow} L/min'}},
        {'id': 'c2', 'field': 'pressure', 'value': 6, 'evidence': {'text': 'Maximum pressure: 6 bar'}},
        {'id': 'c3', 'field': 'weight', 'value': 42, 'evidence': {'text': 'Weight: 42 kg'}},
        {'id': 'c4', 'field': 'noise', 'value': 70, 'evidence': {'text': 'Noise: 70 dB'}},
    ]


def page_of(layout, start: int, end: int) -> int:
    """The page of the UTF-8 byte span [start, end) of the reading text.

    groundgate 0.14 gives no page in the facts@1 citation. This function takes the page from the layout of
    the groundgate reading. A later change takes the page from the locator of the groundgate 0.15 export.
    """
    pages = {box.page for box in layout.locate(start, end)}
    if len(pages) != 1:
        raise SystemExit(f'the span {start}-{end} has no single page: {sorted(pages)}')
    return pages.pop()


def run_groundgate(pdf: Path, flow: int, folder: str) -> tuple[dict, bytes, bytes, object]:
    """Read the PDF and admit the candidates. Return the facts@1 export, the PDF bytes, the text bytes and the layout."""
    with FolderStore(os.path.join(folder, 'store')) as store:
        reading, extracted = put_reading(store, pdf)
        packet_key, receipt = admit_packet(store, extracted.text, SCHEMA, candidates(flow), None, reading=reading)
        packet = load_packet(store, packet_key)
        result = verify(packet.receipt, packet.text, packet.schema, packet.candidates, **packet.inputs())
        if not result.ok:
            raise SystemExit(f'groundgate verify failed: {result.problems}')
        for decision in sorted(receipt.to_dict()['decisions'], key=lambda d: d['field']):
            print('groundgate', decision['field'], decision['outcome'])
        export = admitted_facts(store, packet)
    return export, pdf.read_bytes(), extracted.text.encode('utf-8'), extracted.layout


def call(store: Store, operation: str, request: dict) -> dict:
    result = store.call(operation, request)
    if result['status'] != 'ok':
        raise SystemExit(f'{operation}: {result["status"]} {result["reasons"]}')
    return result


def read(store: Store, context: str, ref: str, selector: dict | None = None) -> dict:
    """Read one member of the context with the read operation (CRD-READ-3). Return the item."""
    request = {'context': context, 'ref': ref}
    if selector is not None:
        request['selector'] = selector
    return call(store, 'read', request)['items'][0]


def publish(store: Store, serving: str, head: str | None) -> str:
    """Publish the serving snapshot. The application owns the acceptance decision (PUB-RCP-2)."""
    note = f'accepted by the example: {serving}'.encode('utf-8')
    digest = 'sha256:' + hashlib.sha256(note).hexdigest()
    receipt = call(store, 'put', {'descriptors': [json.dumps({
        'descriptor_profile': 'aavanam/base/v1', 'kind': 'receipt', 'producer': 'example/reviewer',
        'payload': {'digest': digest, 'media_type': 'text/plain', 'size': len(note)}})],
        'payloads': {digest: base64.b64encode(note).decode()}})['items'][0]['ref']
    candidate = call(store, 'create_candidate', {'snapshot': serving, 'policy': POLICY, 'expected_head': head})
    published = call(store, 'publish', {'candidate': candidate['items'][0]['candidate'], 'receipt': receipt})
    return published['items'][0]['head']


def facts(store: Store, serving: str, evidence: str, layout) -> list[dict]:
    """Read each fact of the two snapshots with the read operation, and give its value and its citation."""
    context = call(store, 'open_context', {'roots': [serving, evidence]})['context']
    root = read(store, context, evidence)['descriptor']
    natives = {}
    for ref in root['native_mappings']:
        mapping = read(store, context, ref)['descriptor']
        natives[mapping['subject']] = mapping['native_id']
    out = []
    for relations in root['relation_sets']:
        for ref in read(store, context, relations)['descriptor']['members']:
            record = read(store, context, ref)['descriptor']
            row = read(store, context, record['subject'], ROW)['row']
            excerpt = read(store, context, record['supports'])
            cited = base64.b64decode(excerpt['data']).decode('utf-8')
            reading = excerpt['descriptor']['parent']
            source = read(store, context, reading)['descriptor']['source']
            span = record['ext']['citation']
            out.append({'field': row['field'], 'value': row['value'], 'text': cited,
                        'page': page_of(layout, span['start'], span['end']),
                        'span': [span['start'], span['end']], 'source': natives.get(source),
                        'text_digest': natives.get(reading)})
    out.sort(key=lambda fact: fact['field'])
    for fact in out:
        if not (fact['text'] and fact['page'] and fact['span'] and fact['source'] and fact['text_digest']):
            raise SystemExit(f'the fact {fact["field"]} has no citation')
    return out


def show(label: str, found: list[dict]) -> None:
    for fact in found:
        print(label, fact['field'], repr(fact['value']), json.dumps(
            {key: fact[key] for key in ('page', 'span', 'text', 'source', 'text_digest')}, sort_keys=True))


def main() -> None:
    store = Store()
    store.section(SERVING, 'serving')
    store.section(EVIDENCE, 'evidence')
    store.accept(POLICY, lambda candidate, receipt, activation: True)
    versions = []
    head = None
    for name, flow in (('datasheet-v1.pdf', 120), ('datasheet-v2.pdf', 135)):
        with tempfile.TemporaryDirectory() as folder:
            export, source, text, layout = run_groundgate(HERE / name, flow, folder)
        serving, evidence = put_export_facts(
            store, json.dumps(export), DECLARATION, source=source, reading=text, source_media_type='application/pdf',
            source_locator='example/' + name, producer='example/pdf-reader/v1',
            native_scheme='example/admitted-citation/v1', serving=SERVING, evidence=EVIDENCE)
        head = publish(store, serving, head)
        print('published', name, 'head is the new snapshot', head == serving)
        found = facts(store, head, evidence, layout)
        show('fact', found)
        versions.append((serving, evidence, layout, found))

    first_serving, first_evidence, first_layout, first_found = versions[0]
    again = facts(store, first_serving, first_evidence, first_layout)
    print('first snapshot unchanged', again == first_found)
    changed = [fact['field'] for fact, new in zip(first_found, versions[1][3]) if fact['value'] != new['value']]
    print('changed in the second snapshot', changed)


if __name__ == '__main__':
    try:
        main()
    except SystemExit as stop:
        print(stop, file=sys.stderr)
        sys.exit(1)
