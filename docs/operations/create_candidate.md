# `create_candidate`

This page describes release 0.6.0. Contract: [document 13](../contracts/13-publication.md), PUB-CND-1.

Propose a snapshot as the next head of its collection. The application registers the validator of its policy first, with `store.accept(policy, validate)` in the Python reference.

## Request

| Field | Value |
|---|---|
| `snapshot` | A snapshot reference. |
| `checks` | A list of check references. |
| `policy` | The acceptance policy of the application. |
| `expected_head` | The current head, or null. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
request = {'snapshot': snap, 'checks': [], 'policy': 'example/review/v1', 'expected_head': None}
out = store.call('create_candidate', request)
print(out['status'], out['reasons'][0]['code'])
store.accept('example/review/v1', lambda candidate, receipt, activation: True)
out = store.call('create_candidate', request)
print(out['status'], out['items'][0]['collection'])
```

Output:

```text
unsupported unsupported_profile
ok pump-manual
```

## Result

One item with the fields `candidate` and `collection`.

## Typed reasons

- `unsupported_profile`: The application did not register a validator for the policy.
- `check_outside_snapshot`: The subject of a check is not in the closure of the snapshot.
- `unknown_ref`: The snapshot is not in the store.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
