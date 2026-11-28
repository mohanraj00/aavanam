# `publications`

This page describes release 0.6.0. Contract: [document 13](../contracts/13-publication.md), PUB-LOG-1.

List the publication records of a collection, in the order of the head updates.

## Request

| Field | Value |
|---|---|
| `collection` | The name of the collection. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
store.accept('example/review/v1', lambda candidate, receipt, activation: True)
cand = store.call('create_candidate', {'snapshot': snap, 'checks': [], 'policy': 'example/review/v1',
                                       'expected_head': None})['items'][0]['candidate']
receipt = put('receipt', '{"decision": "accepted"}', 'application/json', producer='example/review-board')
store.call('publish', {'candidate': cand, 'receipt': receipt})
out = store.call('publications', {'collection': 'pump-manual'})
print(out['status'], [(i['head'] == snap, i['previous_head']) for i in out['items']])
```

Output:

```text
ok [(True, None)]
```

## Result

One item for each publication, with the fields `candidate`, `receipt`, `activation`, `previous_head` and `head`. A collection with no publication gives no items.

## Typed reasons

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
