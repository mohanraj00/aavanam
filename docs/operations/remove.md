# `remove`

This page describes release 0.6.0. Contract: [document 06](../contracts/06-evidence-and-relations.md), REL-RMV-2.

Remove an object of the Evidence section from a writer store. The store keeps a removal record with no content.

## Request

| Field | Value |
|---|---|
| `ref` | The reference of a `source`, `reading`, `excerpt` or `evidence` object. |
| `reason` | The reason, a string. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('remove', {'ref': evidence, 'reason': 'example request'})
print(out['status'], out['items'][0]['kind'], out['items'][0]['sequence'])
ctx = store.call('open_context', {'roots': [snap]})['context']
print(store.call('read', {'context': ctx, 'ref': evidence})['reasons'][0]['code'])
```

Output:

```text
ok evidence 1
removed
```

## Result

One item, the removal record, with the fields `ref`, `kind`, `reason`, `time` and `sequence`. A context that was open before the removal expires.

## Typed reasons

- `unsupported_role`: The store is not a writer store.
- `unknown_ref`: The object is not in the store.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
