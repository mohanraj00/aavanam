# `observations`

This page describes release 0.6.0. Contract: [document 07](../contracts/07-observations-and-coverage.md), OBS-SUB-1.

List the observations about a subject: append-only facts in a stated scope.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `subject` | One reference. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
obs = put('observation', subjects=[reading], observation_type='example/checked/v1', scope={'reading': 'whole'},
          values={'checked_by': 'example/editor'})
snap2 = store.call('create_snapshot', {'collection': 'pump-manual', 'units': [unit], 'observations': [obs]})['items'][0]['ref']
ctx = store.call('open_context', {'roots': [snap2]})['context']
out = store.call('observations', {'context': ctx, 'subject': reading})
print(out['status'], [i['ref'] == obs for i in out['items']])
```

Output:

```text
ok [True]
```

## Result

One item for each observation that has the subject, with the field `ref`. An empty list states only that the context has no observation of the subject.

## Typed reasons

- `not_in_context`: The subject is not a member of the context.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
