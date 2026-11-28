# `producer_read`

This page describes release 0.6.0. Contract: [document 19](../contracts/19-producer-storage.md), PRD-LOG-3.

Read the entries of a producer log.

## Request

| Field | Value |
|---|---|
| `producer` | The name of the producer. |
| `log` | The name of the log. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
runs = {'producer': 'example/reader', 'log': ['runs']}
store.call('producer_append', {**runs, 'sequence': 1, 'entry': base64.b64encode(b'run 1').decode()})
out = store.call('producer_read', runs)
print(out['status'], [(i['sequence'], base64.b64decode(i['entry'])) for i in out['items']])
```

Output:

```text
ok [(1, b'run 1')]
```

## Result

One item for each entry, with the fields `sequence` and `entry`.

## Typed reasons

- `producer_section_absent`: The store is not a writer store.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
