# `create_snapshot`

This page describes release 0.6.0. Contract: [document 13](../contracts/13-publication.md), PUB-SNP-1.

Make an immutable snapshot of one collection from units, schemas, sets and observations.

## Request

| Field | Value |
|---|---|
| `collection` | The name of the collection. |
| `units` | Optional. A list of `knowledge-unit` references. |
| `schemas`, `relation_sets`, `overlay_sets`, `observations`, `native_mappings` | Optional. Lists of references of these kinds. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('create_snapshot', {'collection': 'pump-manual', 'units': [unit]})
print(out['status'], out['items'][0]['ref'] == snap)
out = store.call('create_snapshot', {'collection': 'pump-manual', 'units': [table]})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok True
unsupported unsupported_target_kind
```

## Result

One item with the field `ref`, the snapshot reference. The same members give the same snapshot.

## Typed reasons

- `missing_target`: A member is not in the store.
- `unsupported_target_kind`: A member has a kind that its field does not permit.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
