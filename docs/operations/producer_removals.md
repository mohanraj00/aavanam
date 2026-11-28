# `producer_removals`

This page describes release 0.6.0. Contract: [document 19](../contracts/19-producer-storage.md), PRD-RMV-4.

List the removal records of one producer.

## Request

| Field | Value |
|---|---|
| `producer` | The name of the producer. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
store.call('producer_put', {'producer': 'example/reader', 'key': 'page-1', 'data': base64.b64encode(b'raw page').decode()})
store.call('producer_forget', {'producer': 'example/reader', 'key': 'page-1', 'reason': 'example request'})
out = store.call('producer_removals', {'producer': 'example/reader'})
print(out['status'], [(i['key'], i['reason']) for i in out['items']])
```

Output:

```text
ok [('page-1', 'example request')]
```

## Result

One item for each removal record of the producer.

## Typed reasons

- `producer_section_absent`: The store is not a writer store.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
