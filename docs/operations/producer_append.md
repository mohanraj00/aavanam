# `producer_append`

This page describes release 0.6.0. Contract: [document 19](../contracts/19-producer-storage.md), PRD-LOG-1, PRD-LOG-2.

Append an entry to a producer log. The request gives the next sequence number, so two writers cannot both append it.

## Request

| Field | Value |
|---|---|
| `producer` | The name of the producer. |
| `log` | The name of the log, a list of strings. |
| `sequence` | The number of entries plus 1. |
| `entry` | The base64 bytes. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
runs = {'producer': 'example/reader', 'log': ['runs']}
out = store.call('producer_append', {**runs, 'sequence': 1, 'entry': base64.b64encode(b'run 1').decode()})
print(out['status'], out['items'][0]['sequence'])
out = store.call('producer_append', {**runs, 'sequence': 1, 'entry': base64.b64encode(b'run 2').decode()})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok 1
incompatible log_moved
```

## Result

One item with the field `sequence`.

## Typed reasons

- `log_moved`: The sequence is not the number of entries plus 1.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
