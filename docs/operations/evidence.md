# `evidence`

This page describes release 0.6.0. Contract: [document 06](../contracts/06-evidence-and-relations.md), REL-EVD-1.

Find the evidence that supports or contradicts a subject.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `subject` | One reference. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('evidence', {'context': agent, 'subject': table})
print(out['status'], [(i['ref'] == evidence, i['relation']) for i in out['items']])
```

Output:

```text
ok [(True, 'support')]
```

## Result

One item for each evidence object, with the fields `ref` and `relation`.

## Typed reasons

- `not_in_context`: The subject is not a member of the context.
- `removed`: A `remove` removed the evidence object.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
