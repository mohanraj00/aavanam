"""One update from a source document to the head of a gated collection, with groundgate, entelos and aavanam.

Run it from a clean environment that has the aavanam wheel and the pinned groundgate (examples/library_hours/README.md):

    python3 -I examples/library_hours/library_hours.py

The example does the steps of issue #83:

1. groundgate admits two facts from a synthetic source document: the opening hours of two library branches.
2. The admitted-facts adapter writes the facts to the store (ADP-FXT-1 to ADP-FXT-7). The application declares the
   branch and the dates of each fact in overlays (DSC-FLT-2), and makes the candidate snapshot.
3. A publish to the gated collection with no activation is refused (PUB-GAT-3). The head does not move.
4. The activation record approves the update. The publish with the activation moves the head (PUB-ACT-3).
5. A filtered read of the head for one branch and one date (DSC-FLT-1 to DSC-FLT-5, DAT-RNG-2) gives one fact.

The activation record is a STAND-IN (activation_record_stand_in.json). No release writes the proposed activation
record at commit 4d9d66e yet. Only the function `activation_of` reads the layout of the record.

An unexpected outcome stops the example with exit status 1.
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
SOURCE = HERE / 'hours.txt'
STAND_IN = HERE / 'activation_record_stand_in.json'
SERVING = 'sample-library-hours'
EVIDENCE = 'sample-library-evidence'
POLICY = 'example/library-hours-policy'
BASE = 'aavanam/base/v1'
ORIGIN = 'example/library-policy'
SCHEMA = {'fields': {'north_winter': {'type': 'string'}, 'south_summer': {'type': 'string'}}}
CANDIDATES = [
    {'id': 'c1', 'field': 'north_winter', 'value': '09:00 to 17:00',
     'evidence': {'text': 'North branch, winter season, 2026-01-01 to 2026-04-01: open 09:00 to 17:00'}},
    {'id': 'c2', 'field': 'south_summer', 'value': '10:00 to 20:00',
     'evidence': {'text': 'South branch, summer season, 2026-06-01 to 2026-09-01: open 10:00 to 20:00'}},
]
# The applicability of each fact. The application owns these values. The store compares them, and does not
# interpret them (DSC-FLT-4). The intervals are half-open (DAT-RNG-2).
APPLICABILITY = {
    'north_winter': {'branch': 'north', 'valid_from': '2026-01-01', 'valid_until': '2026-04-01'},
    'south_summer': {'branch': 'south', 'valid_from': '2026-06-01', 'valid_until': '2026-09-01'},
}
# The filtered read: the north branch on one date.
FILTER = {'equals': {'branch': 'north'},
          'ranges': [{'lower': 'valid_from', 'upper': 'valid_until', 'type': 'date', 'match': 'contains',
                      'value': '2026-02-10'}]}
# The reader declaration of facts@1 (ADP-FXT-2). It is the declaration of tests/test_groundgate_seam.py.
DECLARATION = {
    'format': 'facts@1', 'identity_member': 'packet_sha256', 'span_unit': 'utf8-bytes',
    'source_digest': {'form': 'aavanam/prefixed-sha256/v1', 'scheme': 'example/source-object/v1',
                      'prefix': 'groundgate-object/1:source@1\0'},
    'text_digest': {'form': 'aavanam/prefixed-jcs-sha256/v1', 'scheme': 'example/document-digest/v1',
                    'prefix': 'groundgate/0.12:document\0', 'member': 'text'},
}
ROW = {'selector_profile': 'aavanam/row/v1', 'index': 0}


def run_groundgate(folder: str) -> tuple[dict, bytes]:
    """Read the source and admit the candidates. Return the facts@1 export and the reading bytes."""
    with FolderStore(os.path.join(folder, 'store')) as store:
        reading, extracted = put_reading(store, SOURCE)
        packet_key, receipt = admit_packet(store, extracted.text, SCHEMA, CANDIDATES, None, reading=reading)
        packet = load_packet(store, packet_key)
        result = verify(packet.receipt, packet.text, packet.schema, packet.candidates, **packet.inputs())
        if not result.ok:
            raise SystemExit(f'groundgate verify failed: {result.problems}')
        for decision in sorted(receipt.to_dict()['decisions'], key=lambda d: d['field']):
            print('groundgate', decision['field'], decision['outcome'])
        export = admitted_facts(store, packet)
    return export, extracted.text.encode('utf-8')


def call(store: Store, operation: str, request: dict) -> dict:
    result = store.call(operation, request)
    if result['status'] != 'ok':
        raise SystemExit(f'{operation}: {result["status"]} {result["reasons"]}')
    return result


def put(store: Store, descriptor: dict, data: bytes | None = None, media_type: str = 'text/plain') -> str:
    """Write one descriptor, and its payload if it has one. Return the reference."""
    payloads = {}
    if data is not None:
        digest = 'sha256:' + hashlib.sha256(data).hexdigest()
        descriptor = {**descriptor, 'payload': {'digest': digest, 'media_type': media_type, 'size': len(data)}}
        payloads[digest] = base64.b64encode(data).decode('ascii')
    return call(store, 'put', {'descriptors': [json.dumps(descriptor)], 'payloads': payloads})['items'][0]['ref']


def receipt(store: Store, producer: str, text: str) -> str:
    return put(store, {'descriptor_profile': BASE, 'kind': 'receipt', 'producer': producer}, text.encode('utf-8'))


def read(store: Store, context: str, ref: str, selector: dict | None = None) -> dict:
    """Read one member of the context with the read operation (CRD-READ-3). Return the item."""
    request = {'context': context, 'ref': ref}
    if selector is not None:
        request['selector'] = selector
    return call(store, 'read', request)['items'][0]


def facts_of(store: Store, snapshot: str) -> dict[str, str]:
    """The data resource of each admitted fact of the adapter snapshot, by field name."""
    context = call(store, 'open_context', {'roots': [snapshot]})['context']
    out = {}
    for unit in read(store, context, snapshot)['descriptor']['units']:
        bindings = read(store, context, read(store, context, unit)['descriptor']['binding_set'])['descriptor']
        for ref in bindings['bindings'].values():
            out[read(store, context, ref, ROW)['row']['field']] = ref
    return out


def candidate_snapshot(store: Store, adapter_snapshot: str) -> tuple[str, list[str]]:
    """Add the applicability overlays to the adapter snapshot. Return the new snapshot and its members."""
    context = call(store, 'open_context', {'roots': [adapter_snapshot]})['context']
    adapter = read(store, context, adapter_snapshot)['descriptor']
    overlays = sorted(put(store, {'descriptor_profile': BASE, 'kind': 'overlay', 'subject': ref, 'origin': ORIGIN,
                                  'values': APPLICABILITY[field]})
                      for field, ref in facts_of(store, adapter_snapshot).items())
    applicability = put(store, {'descriptor_profile': BASE, 'kind': 'overlay-set', 'members': overlays})
    overlay_sets = sorted(adapter['overlay_sets'] + [applicability])
    snapshot = call(store, 'create_snapshot', {'collection': SERVING, 'units': adapter['units'],
                                               'overlay_sets': overlay_sets})['items'][0]['ref']
    return snapshot, adapter['units'] + overlay_sets


def bind_value(value: str, bound: dict[str, str]) -> str:
    return bound[value] if value.startswith('$') else value


def activation_of(store: Store, record: dict, bound: dict[str, str], lock: str) -> str:
    """Write the receipts and the `activation` descriptor of one record (PUB-ACT-1). Return the activation.

    This function is the mapping from the layout of the record. The store keeps the interval, the request key and the
    names as strings. It does not evaluate them (PUB-ACT-5).

    The example derives lock, base, approval and report. PUB-ACT-3 checks 3 to 5 run on the derived lock;
    check 2 runs on the derived base. The receipt checks at put run on derived approval and report receipts
    with decision_ref and report_ref as payload text (PUB-ACT-1). Store-side gate behavior is still proved
    for each check. This example does not prove that the record declares these values. The producer record
    does not yet give them. These are gaps of this PR, guarded by tests/test_activation_seam.py.
    """
    # Derived receipts contain the native references as opaque payloads. They are not fetched receipt content.
    approval = receipt(store, 'sample-reference-adapter', record['decision_ref'])
    report = receipt(store, 'sample-reference-adapter', record['report_ref'])
    # Derived profile and kind name the store descriptor. The record format is not a descriptor profile.
    # Derived lock covers the changed members of the store snapshot. No record field is an input lock.
    # The candidate binding is a test value. It proves no equality between native identity schemes.
    descriptor = {'descriptor_profile': BASE, 'kind': 'activation', 'candidate': bind_value(record['candidate']['ref'], bound),
                  'lock': lock, 'approval': approval, 'report': report,
                  'partition': record['partition'], 'effective_from': record['from'],
                  'effective_until': record['until'], 'request_key': record['request_key'],
                  'producer': record['producer'], 'runner': record['runner'], 'approver': record['approver']}
    # Derived base is the bound expected store head. An empty record base gives no descriptor base.
    if record['base']:
        descriptor['base'] = bind_value(record['base'], bound)
    # Derived removed is absent: this first update removes no member (PUB-ACT-2).
    return put(store, descriptor)


def head(store: Store) -> str | None:
    """The head of the gated collection, or None (CTX-VIW-2)."""
    entry = store.call('open_context', {'heads': [SERVING]})['items'][0]
    return entry['ref'] if entry['status'] == 'ok' else None


def validate(candidate: str, acceptance: str, activation: str | None) -> bool:
    """The validator of the application (PUB-RCP-2). It accepts a candidate only with an activation."""
    return activation is not None


def main() -> None:
    store = Store()
    store.section(SERVING, 'serving')
    store.section(EVIDENCE, 'evidence')
    store.gate(SERVING)  # PUB-GAT-1: before the first call
    store.accept(POLICY, validate)

    # Steps 1 and 2: groundgate admits the facts, and the adapter writes them.
    with tempfile.TemporaryDirectory() as folder:
        export, reading = run_groundgate(folder)
    adapter_snapshot, _ = put_export_facts(
        store, json.dumps(export), DECLARATION, source=SOURCE.read_bytes(), reading=reading,
        source_media_type='text/plain', source_locator='example/hours.txt', producer='example/document-reader/v1',
        native_scheme='example/admitted-citation/v1', serving=SERVING, evidence=EVIDENCE)
    snapshot, members = candidate_snapshot(store, adapter_snapshot)
    candidate = call(store, 'create_candidate', {'snapshot': snapshot, 'policy': POLICY,
                                                 'expected_head': None})['items'][0]['candidate']
    print('adapter wrote', len(facts_of(store, snapshot)), 'facts to', SERVING)
    acceptance = receipt(store, 'example/reviewer', 'accepted by the example')

    # Step 3: no activation. The gate refuses the publish, and the head does not move.
    refused = store.call('publish', {'candidate': candidate, 'receipt': acceptance})
    item = refused['items'][0]
    print('publish with no activation:', item['status'], [r['code'] for r in item['reasons']], 'head', head(store))
    if item['status'] == 'ok' or head(store) is not None:
        raise SystemExit('the gate did not refuse the publish')

    # Step 4: the activation record approves the update. The lock covers each changed member (PUB-ACT-3 check 5).
    record = json.loads(STAND_IN.read_text(encoding='utf-8'))['records']['first']
    lock = put(store, {'descriptor_profile': BASE, 'kind': 'build-lock', 'inputs': sorted(members),
                       'consumer': 'example/library-reader', 'completeness': 'example/declared/v1'})
    activation = activation_of(store, record, {'$candidate': candidate}, lock)
    published = call(store, 'publish', {'candidate': candidate, 'receipt': acceptance,
                                        'activation': activation})['items'][0]
    print('publish with the activation:', published['status'], published['disposition'],
          'head is the candidate snapshot', head(store) == snapshot)

    # Step 5: a filtered read of the head for one branch and one date.
    context = call(store, 'open_context', {'heads': [SERVING]})['context']
    listing = call(store, 'list_items', {'context': context, 'kinds': ['data-resource'], 'filter': FILTER})
    when = FILTER['ranges'][0]['value']
    print('filter branch', FILTER['equals']['branch'], 'date', when, 'gives', len(listing['items']), 'of',
          len(facts_of(store, snapshot)), 'facts')
    for entry in listing['items']:
        row = read(store, context, entry['ref'], ROW)['row']
        print('fact', row['field'], repr(row['value']))


if __name__ == '__main__':
    try:
        main()
    except SystemExit as stop:
        print(stop, file=sys.stderr)
        sys.exit(1)
