# `producer_forget`

This page describes release 0.6.0. Contract: [document 19](../contracts/19-producer-storage.md), PRD-RMV-1.

Remove the bytes of a producer key, and keep a removal record.

## Request

| Field | Value |
|---|---|
| `producer` | The name of the producer. |
| `key` | The key. |
| `reason` | The reason, a string. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
store.call('producer_put', {'producer': 'example/reader', 'key': 'page-1', 'data': base64.b64encode(b'raw page').decode()})
out = store.call('producer_forget', {'producer': 'example/reader', 'key': 'page-1', 'reason': 'example request'})
print(out['status'], out['items'][0]['sequence'])
out = store.call('producer_get', {'producer': 'example/reader', 'key': 'page-1'})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok 1
not_found producer_removed
```

## Result

One item, the removal record, with the fields `key`, `reason`, `time` and `sequence`.

## Typed reasons

- `unknown_producer_key`: The producer has no object with the key.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
