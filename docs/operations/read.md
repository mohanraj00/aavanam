# `read`

This page describes release 0.6.0. Contract: [document 20](../contracts/20-content-reads-and-text-search.md), CRD-READ-1.

Read the descriptor and the payload bytes of one member. A selector reads a part of the bytes.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `ref` | One reference. |
| `selector` | Optional. For example `aavanam/bytes/v1` with `start` and `end`. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('read', {'context': agent, 'ref': manual})
item = out['items'][0]
print(out['status'], item['kind'], base64.b64decode(item['data']).decode().splitlines()[0])
out = store.call('read', {'context': agent, 'ref': manual, 'selector': {'selector_profile': 'aavanam/bytes/v1', 'start': 0, 'end': 999}})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok source # Pump P-1
failed selector_range
```

## Result

One item with the fields `kind`, `descriptor`, `data` (base64), `selector` and `row`.

## Typed reasons

- `not_in_context`: The object is not a member of the context.
- `selector_range`: The selector selects outside the payload.
- `unsupported_selector`: The selector form is not supported for the target.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
