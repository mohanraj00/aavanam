# `snapshot_changes`

This page describes release 0.6.0. Contract: [document 17](../contracts/17-lifecycle.md), LIF-CHG-1.

List the items that one snapshot adds, removes or replaces in a later snapshot.

## Request

| Field | Value |
|---|---|
| `before`, `after` | Two snapshot references. |
| `kinds` | Optional. A list of kinds. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
note = put('artifact', 'Check the seal for wear at each service.', artifact_type='example/narrative', logical_id='seal-note')
unit2 = put('knowledge-unit', binding_set=bindings, narratives=[note])
snap2 = store.call('create_snapshot', {'collection': 'pump-manual', 'units': [unit2]})['items'][0]['ref']
out = store.call('snapshot_changes', {'before': snap, 'after': snap2, 'kinds': ['artifact']})
print(out['status'], [(i['change'], i['logical_id']) for i in out['items']])
```

Output:

```text
ok [('added', 'seal-note')]
```

## Result

One item for each change, with the fields `change`, `kind`, `logical_id`, `before` and `after`.

## Typed reasons

- `unknown_ref`: A snapshot is not in the store.
- `unknown_kind`: A kind in `kinds` is not registered.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
