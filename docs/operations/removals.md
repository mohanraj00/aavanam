# `removals`

This page describes release 0.6.0. Contract: [document 06](../contracts/06-evidence-and-relations.md), REL-RMV-6.

List the removal records of a writer store.

## Request

The request is an empty object.

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
store.call('remove', {'ref': evidence, 'reason': 'example request'})
out = store.call('removals', {})
print(out['status'], [(i['kind'], i['reason']) for i in out['items']])
```

Output:

```text
ok [('evidence', 'example request')]
```

## Result

One item for each removal record, in the order of the removals.

## Typed reasons

- `unsupported_role`: The store is not a writer store.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
