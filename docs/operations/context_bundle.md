# `context_bundle`

This page describes release 0.6.0. Contract: [document 09](../contracts/09-lookup-and-discovery.md), QRY-BDL-1.

List the closure of roots in a read context, in a stable order, with a budget.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `roots` | A list of references. |
| `budget` | Optional. A bound on the result. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('context_bundle', {'context': agent, 'roots': [snap]})
print(out['status'], [i['kind'] for i in out['items']][:4])
```

Output:

```text
ok ['snapshot', 'knowledge-unit', 'binding-set', 'relation-set']
```

## Result

One item for each object of the closure, with the fields `ref`, `kind` and `size`.

## Typed reasons

- `bounds_reached`: The result stopped at the budget. The status is `partial`.
- `unsupported_budget`: The budget unit is not supported.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
