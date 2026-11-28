# `producer_put`

This page describes release 0.6.0. Contract: [document 19](../contracts/19-producer-storage.md), PRD-OBJ-1, PRD-OBJ-2.

Store opaque bytes under a key, for one producer. Only a writer store has a producer section.

## Request

| Field | Value |
|---|---|
| `producer` | The name of the producer. |
| `key` | The key. |
| `data` | The base64 bytes. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('producer_put', {'producer': 'example/reader', 'key': 'page-1', 'data': base64.b64encode(b'raw page').decode()})
print(out['status'], out['items'][0]['size'])
out = store.call('producer_put', {'producer': 'example/reader', 'key': 'page-1', 'data': base64.b64encode(b'new').decode()})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok 8
incompatible producer_key_conflict
```

## Result

One item with the fields `key` and `size`. A second `put` of the same bytes is `ok`.

## Typed reasons

- `producer_key_conflict`: The key has different bytes.
- `producer_section_absent`: The store is not a writer store.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
