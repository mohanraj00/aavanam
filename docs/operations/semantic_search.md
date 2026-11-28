# `semantic_search`

This page describes release 0.6.0. Contract: [document 24](../contracts/24-retrieval-indexes.md), IDX-SRC-1, IDX-MTH-1.

Search the units of one snapshot through a retrieval index. The one method, `aavanam/hashed-terms/v1`, is deterministic and needs no model.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. Its roots include the index. |
| `index` | A `retrieval-index` reference. |
| `query` | The words to find. |
| `filter` | Optional. A metadata filter. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
note = put('artifact', 'Check the seal for wear at each service.', artifact_type='example/narrative')
unit2 = put('knowledge-unit', binding_set=bindings, narratives=[note], logical_id='pump-p1')
snap2 = store.call('create_snapshot', {'collection': 'pump-manual', 'units': [unit2]})['items'][0]['ref']
index = put('retrieval-index', snapshot=snap2, method='aavanam/hashed-terms/v1', parameters={'dimensions': 64})
ctx = store.call('open_context', {'roots': [snap2, index]})['context']
out = store.call('semantic_search', {'context': ctx, 'index': index, 'query': 'seal wear'})
print(out['status'], [(i['logical_id'], i['score']) for i in out['items']])
```

Output:

```text
ok [('pump-p1', 2)]
```

## Result

One item for each unit that matches, with the fields `ref`, `kind`, `logical_id`, `snapshot`, `index` and `score`.

## Typed reasons

- `index_stale`: The index is of a different snapshot of the collection.
- `reserved_profile`: The store does not declare `aavanam/semantic-index/v1`.
- `unsupported_profile`: The index has an external method.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
