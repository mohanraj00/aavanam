# `impact`

This page describes release 0.6.0. Contract: [document 12](../contracts/12-change-and-impact.md), CHG-IMP-1.

List the members that depend on changed objects, through the typed reference fields.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `changed` | A list of references. |
| `dependents` | Optional. The kind of dependents. |
| `limit` | Optional. A bound on the result. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('impact', {'context': agent, 'changed': [schema]})
print(out['status'], [(i['kind'], i['distance']) for i in out['items']])
```

Output:

```text
ok [('data-resource', 1), ('binding-set', 2), ('evidence', 2), ('relation-set', 3), ('knowledge-unit', 3), ('snapshot', 4)]
```

## Result

One item for each dependent, with the fields `ref`, `kind` and `distance`.

## Typed reasons

- `unsupported_dependents`: The kind of dependents is not supported.
- `bounds_reached`: The result stopped at `limit`. The status is `partial`.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
