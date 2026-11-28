# `export`

This page describes release 0.6.0. Contract: [document 11](../contracts/11-bundles-and-closure.md), BUN-EXP-1.

Export the closure of roots as a bundle that a different store can import.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `roots` | A list of references. |
| `base_context` | Optional. Export only the content that this context does not have. |
| `filter` | Optional. A metadata filter. |
| `permit` | Optional. The redistribution that the target permits. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('export', {'context': agent, 'roots': [snap]})
print(out['status'], len(out['items']), sorted({i['redistribution'] for i in out['items']}))
```

Output:

```text
ok 9 ['no_payload', 'undeclared']
```

## Result

The field `bundle` has the bundle. One item for each object, with the fields `license`, `redistribution` and `withheld`.

## Typed reasons

- `no_match`: No candidate matches the filter.
- `unmatched_dependency`: The closure of a match has a candidate that does not match the filter.
- `bundle_incomplete`: The store does not have a base root of a delta bundle.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
