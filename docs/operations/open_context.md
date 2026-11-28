# `open_context`

This page describes release 0.6.0. Contract: [document 08](../contracts/08-views-contexts-and-capabilities.md), CTX-OPN-1, CTX-VIW-2, CTX-LIF-1, CTX-PRJ-2, and [document 24](../contracts/24-retrieval-indexes.md), IDX-DEF-3.

Open a read context on snapshots, knowledge units or published heads. Each read operation names a context.

An index is not in the closure of its snapshot. To use an index with [`semantic_search`](semantic_search.md), add the index as a root.

## Request

| Field | Value |
|---|---|
| `roots` | Optional. A list of `snapshot` or `knowledge-unit` references. If the store declares `aavanam/semantic-index/v1`, a root can also be a `retrieval-index` reference. |
| `heads` | Optional. A list of collection names. The store reads the published head of each one. |
| `life` | Optional. The life of the context in seconds. |
| `capabilities` | Optional. The capabilities that the context permits. |
| `skew_policy`, `layer` | Optional. See document 08 and document 23. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('open_context', {'roots': [snap], 'life': 600})
print(out['status'], out['context'].startswith('ctx:'), out['items'][0]['removed'])
out = store.call('open_context', {'heads': ['pump-manual']})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok True []
unresolved no_head
```

## Result

The field `context` has the identity of the context. Each root gives one item with the field `removed`, the removed objects that the root reaches.

## Typed reasons

- `no_head`: A collection in `heads` has no published head.
- `unsupported_target_kind`: A root is not a snapshot or a knowledge unit. If the store declares `aavanam/semantic-index/v1`, a `retrieval-index` root is also permitted.
- `collection_conflict`: Two roots are different snapshots of one collection.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
