# `compare`

This page describes release 0.6.0. Contract: [document 12](../contracts/12-change-and-impact.md), CHG-SEL-1.

Compare the selected bytes of two members.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `before`, `after` | Two references. |
| `selector` | The part of the bytes to compare. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
cut = {'selector_profile': 'aavanam/bytes/v1', 'start': 10, 'end': 43}
out = store.call('compare', {'context': agent, 'before': manual, 'after': reading, 'selector': cut})
print(out['status'], out['items'][0]['change'])
```

Output:

```text
ok changed
```

## Result

One item with the field `change`.

## Typed reasons

- `selector_range`: The selector selects outside a payload.
- `unsupported_selector`: The selector form is not supported for a target.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
