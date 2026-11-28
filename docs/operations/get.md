# `get`

This page describes release 0.6.0. Contract: [document 09](../contracts/09-lookup-and-discovery.md), QRY-GET-1.

Check that exact references are members of a read context.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `refs` | A list of references. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('get', {'context': agent, 'refs': [table, 'sha256:' + '0' * 64]})
print(out['status'], [(i['status'], [r['code'] for r in i['reasons']]) for i in out['items']])
```

Output:

```text
not_found [('ok', []), ('not_found', ['unknown_ref'])]
```

## Result

One item for each reference, in the request order.

## Typed reasons

- `unknown_ref`: The reference is not in the store.
- `not_in_context`: The object is in the store, but not in the context.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
