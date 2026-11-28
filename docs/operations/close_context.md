# `close_context`

This page describes release 0.6.0. Contract: [document 08](../contracts/08-views-contexts-and-capabilities.md), CTX-LIF-3.

End a read context. A later call with the context gives `context_expired`.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('close_context', {'context': agent})
print(out['status'])
out = store.call('read', {'context': agent, 'ref': manual})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok
failed context_expired
```

## Result

No items. The status is `ok` or `failed`.

## Typed reasons

- `context_expired`: The context ended, or the store has no context with the identity.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
