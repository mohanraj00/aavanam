# `producer_get`

This page describes release 0.6.0. Contract: [document 19](../contracts/19-producer-storage.md), PRD-OBJ-3.

Read the bytes of a producer key.

## Request

| Field | Value |
|---|---|
| `producer` | The name of the producer. |
| `key` | The key. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
store.call('producer_put', {'producer': 'example/reader', 'key': 'page-1', 'data': base64.b64encode(b'raw page').decode()})
out = store.call('producer_get', {'producer': 'example/reader', 'key': 'page-1'})
print(out['status'], base64.b64decode(out['items'][0]['data']))
out = store.call('producer_get', {'producer': 'example/reader', 'key': 'page-2'})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok b'raw page'
not_found unknown_producer_key
```

## Result

One item with the fields `key` and `data`.

## Typed reasons

- `unknown_producer_key`: The producer has no object and no removal record with the key.
- `producer_removed`: The producer removed the object with the key.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
