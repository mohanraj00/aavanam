# `search_text`

This page describes release 0.6.0. Contract: [document 20](../contracts/20-content-reads-and-text-search.md), CRD-TXT-1.

Find a word in the text artifacts of a read context. Each match gives a byte selector that `read` accepts. Evidence material is not a candidate.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `query` | The words to find. |
| `limit`, `offset` | Optional. The page of the result. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
note = put('artifact', 'Check the seal for wear at each service.', artifact_type='example/narrative')
unit2 = put('knowledge-unit', binding_set=bindings, narratives=[note])
snap2 = store.call('create_snapshot', {'collection': 'pump-manual', 'units': [unit2]})['items'][0]['ref']
ctx = store.call('open_context', {'roots': [snap2]})['context']
out = store.call('search_text', {'context': ctx, 'query': 'wear'})
match = out['items'][0]
print(out['status'], match['ref'] == note, match['selector'])
```

Output:

```text
ok True {'end': 23, 'start': 19, 'selector_profile': 'aavanam/bytes/v1'}
```

## Result

One item for each match, with the fields `ref`, `selector` and `snapshots`.

## Typed reasons

- `bounds_reached`: The result stopped at `limit`. The status is `partial`.
- `payload_missing`: The bytes of a candidate are not in the store.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
