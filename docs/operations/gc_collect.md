# `gc_collect`

This page describes release 0.6.0. Contract: [document 17](../contracts/17-lifecycle.md), LIF-GC-1, LIF-GC-2.

Remove the objects and payload bytes that no retained root reaches.

## Request

| Field | Value |
|---|---|
| `roots` | A list of references that the store keeps, in addition to its retained roots. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
extra = put('schema', '{"fields": []}', 'application/json', schema_profile='example/schema/v1')
out = store.call('gc_collect', {'roots': [snap]})
print(out['status'], extra in [i['ref'] for i in out['items']], table in [i['ref'] for i in out['items']])
```

Output:

```text
ok True False
```

## Result

One item for each removed object, and the fields `candidates` and `payloads`.

## Typed reasons

- `unsupported_role`: The store is a local output store.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
