# `build_inputs`

This page describes release 0.6.0. Contract: [document 10](../contracts/10-consumer-input-locks.md), LCK-BLD-1.

Lock the exact inputs of one code emitter run.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `inputs` | A list of references. |
| `consumer` | The name of the consumer. |
| `completeness` | The completeness profile. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('build_inputs', {'context': script, 'inputs': [bindings], 'consumer': 'example/emitter',
                                  'completeness': 'aavanam/closure-only/v1'})
print(out['status'], out['lock'].startswith('sha256:'), len(out['items']))
```

Output:

```text
ok True 3
```

## Result

The field `lock` has the reference of the `build-lock`. One item for each locked object.

## Typed reasons

- `not_in_context`: An input is not a member of the context.
- `capability_unavailable`: The context does not have a capability that the lock needs.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
