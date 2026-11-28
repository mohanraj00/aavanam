# `import`

This page describes release 0.6.0. Contract: [document 11](../contracts/11-bundles-and-closure.md), BUN-IMP-1.

Import a bundle into a store. A second import adds only the new content.

## Request

| Field | Value |
|---|---|
| `bundle` | A bundle from `export`. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
bundle = store.call('export', {'context': agent, 'roots': [snap]})['bundle']
device = Store(role='device')
out = device.call('import', {'bundle': bundle})
print(out['status'], len(out['items']))
ctx = device.call('open_context', {'roots': [snap]})['context']
print(device.call('read', {'context': ctx, 'ref': manual})['status'])
```

Output:

```text
ok 9
ok
```

## Result

One item for each imported object.

## Typed reasons

- `bundle_incomplete`: The bundle does not have a reference target or payload bytes that the store does not have.
- `excerpt_mismatch`: The bytes of an excerpt do not agree with its parent payload.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
