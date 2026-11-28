# `item_history`

This page describes release 0.6.0. Contract: [document 17](../contracts/17-lifecycle.md), LIF-HST-1.

List the revisions of one logical ID over the publications of a collection.

## Request

| Field | Value |
|---|---|
| `collection` | The name of the collection. |
| `logical_id` | The logical ID. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
store.accept('example/review/v1', lambda candidate, receipt, activation: True)
cand = store.call('create_candidate', {'snapshot': snap, 'checks': [], 'policy': 'example/review/v1',
                                       'expected_head': None})['items'][0]['candidate']
receipt = put('receipt', '{"decision": "accepted"}', 'application/json', producer='example/review-board')
store.call('publish', {'candidate': cand, 'receipt': receipt})
out = store.call('item_history', {'collection': 'pump-manual', 'logical_id': 'seal-note'})
print(out['status'], [(i['change'], i['head'] == snap) for i in out['items']])
```

Output:

```text
ok [('absent', True)]
```

## Result

One item for each publication of the collection, with the fields `change`, `revisions`, `head`, `previous_head` and `candidate`.

## Typed reasons

- `field_count`: The request does not have `collection` or `logical_id`.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
