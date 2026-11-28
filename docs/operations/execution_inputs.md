# `execution_inputs`

This page describes release 0.6.0. Contract: [document 10](../contracts/10-consumer-input-locks.md), LCK-EXE-1, LCK-CAP-1.

Lock the exact inputs of one script run: a binding set and its closure.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `binding_set` | A `binding-set` reference. |
| `consumer` | The name of the consumer. |
| `completeness` | The completeness profile, for example `aavanam/closure-only/v1`. |
| `context_inputs` | Optional. More references to lock. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('execution_inputs', {'context': script, 'binding_set': bindings, 'consumer': 'example/service-script',
                                      'completeness': 'aavanam/closure-only/v1'})
print(out['status'], out['lock'].startswith('sha256:'), len(out['items']))
```

Output:

```text
ok True 3
```

## Result

The field `lock` has the reference of the `execution-lock`. One item for each locked object. A lock is complete over its declared references only.

## Typed reasons

- `unbound_role`: A declared role has no binding.
- `role_schema_mismatch`: A bound object does not have the schema of its role.
- `capability_unavailable`: The context does not have a capability that the lock needs.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
