# `list_items`

This page describes release 0.6.0. Contract: [document 18](../contracts/18-discovery.md), DSC-LST-1.

List the members of a read context, by kind, logical ID or metadata.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `kinds` | Optional. A list of kinds. |
| `logical_ids` | Optional. A list of logical IDs. |
| `filter` | Optional. A metadata filter. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('list_items', {'context': agent, 'kinds': ['data-resource', 'schema']})
print(out['status'], [i['kind'] for i in out['items']])
out = store.call('list_items', {'context': agent, 'kinds': ['pump']})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok ['data-resource', 'schema']
unsupported unknown_kind
```

## Result

One item for each member that matches, with the fields `ref`, `kind` and `logical_id`.

## Typed reasons

- `unknown_kind`: A kind in `kinds` is not registered.
- `no_metadata`: No overlay gives a value for a filtered key.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
