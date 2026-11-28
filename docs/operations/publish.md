# `publish`

This page describes release 0.6.0. Contract: [document 13](../contracts/13-publication.md), PUB-HED-2, PUB-RCP-1.

Make a candidate the head of its collection. The validator of the application decides if it accepts the receipt.

## Request

| Field | Value |
|---|---|
| `candidate` | A candidate reference. |
| `receipt` | A `receipt` reference. |
| `activation` | Optional. An activation, for a gated collection. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
store.accept('example/review/v1', lambda candidate, receipt, activation: True)
cand = store.call('create_candidate', {'snapshot': snap, 'checks': [], 'policy': 'example/review/v1',
                                       'expected_head': None})['items'][0]['candidate']
receipt = put('receipt', '{"decision": "accepted"}', 'application/json', producer='example/review-board')
out = store.call('publish', {'candidate': cand, 'receipt': receipt})
print(out['status'], out['items'][0]['disposition'], out['items'][0]['head'] == snap)
```

Output:

```text
ok published True
```

## Result

One item with the fields `candidate`, `disposition` and `head`. The application registers its validator with `store.accept(policy, validate)` in the Python reference.

## Typed reasons

- `head_moved`: The expected head of the candidate is not the current head.
- `receipt_rejected`: The validator did not accept the receipt.
- `activation_required`: The collection is gated, and the request has no activation.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
