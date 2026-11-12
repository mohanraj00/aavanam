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

    # The consumer: open a context on the published head, find the reading and read its bytes.
    context = store.call('open_context', {'roots': [pub['head']]})['context']
    hit = store.call('search', {'context': context, 'query': 'seal'})['items'][0]
    # The export bundle carries the exact descriptors and payload bytes of the closure (BUN-EXP-2).
    bundle = store.call('export', {'context': context, 'roots': [hit['ref']]})['bundle']
    desc = next(json.loads(t) for t in bundle['descriptors'] if json.loads(t)['kind'] == 'reading')
    data = base64.b64decode(bundle['payloads'][desc['payload']['digest']])
    start = data.index(QUOTE)
    end = start + len(QUOTE)
    print('text', repr(data[start:end].decode('utf-8')))

    # The citation names the collection, the snapshot, the item and the byte range of the quote.
    citation = {'collection': COLLECTION, 'snapshot': snap, 'ref': hit['ref'], 'kind': hit['kind'],
                'selector': {'selector_profile': 'aavanam/bytes/v1', 'start': start, 'end': end}}
    print('citation', json.dumps(citation, indent=2, sort_keys=True))


if __name__ == '__main__':
    main()
