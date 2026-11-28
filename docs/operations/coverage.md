# `coverage`

This page describes release 0.6.0. Contract: [document 07](../contracts/07-observations-and-coverage.md), OBS-COV-1, OBS-COV-2.

Read the coverage that a producer stated for a subject. The store does not compute a ratio or fill a missing total.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `subject` | One reference. |
| `observation_type` | Optional. The default is `aavanam/coverage/v1`. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
obs = put('observation', subjects=[reading], observation_type='aavanam/coverage/v1', scope={'reading': 'whole'},
          values={'unit': 'pages', 'covered': 1, 'total': None, 'omissions': []})
snap2 = store.call('create_snapshot', {'collection': 'pump-manual', 'units': [unit], 'observations': [obs]})['items'][0]['ref']
ctx = store.call('open_context', {'roots': [snap2]})['context']
item = store.call('coverage', {'context': ctx, 'subject': reading})['items'][0]
print(item['status'], item['unit'], item['covered'], item['total'])
```

Output:

```text
ok pages 1 None
```

## Result

One item for each coverage observation, with the fields `ref`, `unit`, `covered`, `total` and `omissions`. A `total` of null is an unknown denominator.

## Typed reasons

- `values_form`: The values of the observation do not agree with the type.
- `not_in_context`: The subject is not a member of the context.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
