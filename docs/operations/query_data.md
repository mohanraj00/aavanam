# `query_data`

This page describes release 0.6.0. Contract: [document 09](../contracts/09-lookup-and-discovery.md), QRY-DAT-1.

Read typed rows from table resources, with an exact filter.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `resources` | A list of `data-resource` references. |
| `where` | Optional. Exact column values. |
| `metadata` | Optional. Metadata keys to read from overlays. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('query_data', {'context': script, 'resources': [table], 'where': {'part': 'bearing'}})
print(out['status'], out['items'][0]['rows'])
out = store.call('query_data', {'context': script, 'resources': [table], 'where': {'colour': 'red'}})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
ok [{'interval_h': 2000, 'part': 'bearing'}]
unresolved unknown_column
```

## Result

One item for each resource, with the fields `rows` and `next`.

## Typed reasons

- `unknown_column`: A column in the request is not a column of the table.
- `missing_value`: A value that a range clause compares is missing.
- `value_type`: A compared value is not of the declared type.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
