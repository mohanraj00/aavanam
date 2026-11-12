"""Create a snapshot, publish it, read one item and print its text and its citation.

Run it from a clean environment that has the aavanam wheel installed:

    python3 examples/quickstart.py

The store is in memory. The example uses only the public operations of Store.call and the
acceptance hook Store.accept. See examples/README.md for the expected output.
"""
from __future__ import annotations

import base64
import hashlib
import json

from aavanam import Store

POLICY = 'example/quickstart-policy'
COLLECTION = 'pump-manual'
QUOTE = b'Replace the seal every 500 hours.'


def put(store: Store, kind: str, text: str | None = None, media_type: str = 'text/plain', **fields) -> str:
    """Put one descriptor, with the bytes of its payload if it has a text. Return its reference."""
    desc, data = {'descriptor_profile': 'aavanam/base/v1', 'kind': kind, **fields}, {}
    if text is not None:
        raw = text.encode('utf-8')
        digest = 'sha256:' + hashlib.sha256(raw).hexdigest()
        desc['payload'] = {'digest': digest, 'media_type': media_type, 'size': len(raw)}
        data = {digest: base64.b64encode(raw).decode()}
    result = store.call('put', {'descriptors': [json.dumps(desc)], 'payloads': data})
    if result['status'] != 'ok':
        raise SystemExit(f'put {kind}: {result["status"]} {result["reasons"]}')
    return result['items'][0]['ref']


def read(store: Store, context: str, ref: str, selector: dict | None = None) -> dict:
    """Read one member of the context with the read operation. Return the item."""
    request = {'context': context, 'ref': ref}
    if selector is not None:
        request['selector'] = selector
    result = store.call('read', request)
    if result['status'] != 'ok':
        raise SystemExit(f'read: {result["status"]} {result["reasons"]}')
    return result['items'][0]


def main() -> None:
    store = Store()

    # The producer: a source, a reading of it, and evidence that the reading supports a binding set.
    source = put(store, 'source', '# Pump P-1\nReplace the seal every 500 hours.\n', 'text/markdown')
    reading = put(store, 'reading', 'Pump P-1. Replace the seal every 500 hours.', source=source,
                  producer='example/reader', coverage_profile='example/coverage/v1')
    bindings = put(store, 'binding-set', bindings={})
    evidence = put(store, 'evidence', subject=bindings, supports=reading, origin='example/editor')
    relations = put(store, 'relation-set', members=[evidence])
    snap = store.call('create_snapshot', {'collection': COLLECTION, 'relation_sets': [relations]})
    print('snapshot', snap['status'])
    snap = snap['items'][0]['ref']

    # The publication. The application owns the acceptance decision (PUB-RCP-2). This one accepts its own receipt.
    receipt = put(store, 'receipt', 'accepted by the example', producer='example/reviewer')
    store.accept(POLICY, lambda candidate, rcpt, activation: rcpt == receipt)
    cand = store.call('create_candidate', {'snapshot': snap, 'policy': POLICY, 'expected_head': None})
    cand = cand['items'][0]['candidate']
    pub = store.call('publish', {'candidate': cand, 'receipt': receipt})['items'][0]
    print('publish', pub['status'], pub['disposition'], 'head is the snapshot', pub['head'] == snap)

    # The consumer: open a context on the published head, find the reading and read the quote.
    context = store.call('open_context', {'roots': [pub['head']]})['context']
    hit = store.call('search', {'context': context, 'query': 'seal'})['items'][0]
    # The first read gives all the payload bytes of the reading (CRD-READ-3). Find the quote in these bytes.
    whole = read(store, context, hit['ref'])
    start = base64.b64decode(whole['data']).index(QUOTE)
    selector = {'selector_profile': 'aavanam/bytes/v1', 'start': start, 'end': start + len(QUOTE)}
    # The second read gives only the selected bytes (CRD-SEL-3).
    quote = read(store, context, hit['ref'], selector)
    print('text', repr(base64.b64decode(quote['data']).decode('utf-8')))

    # The citation names the collection, the snapshot, the item and the byte range of the quote.
    citation = {'collection': COLLECTION, 'snapshot': snap, 'ref': quote['ref'], 'kind': quote['kind'],
                'selector': quote['selector']}
    print('citation', json.dumps(citation, indent=2, sort_keys=True))


if __name__ == '__main__':
    main()
