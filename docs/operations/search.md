# `search`

This page describes release 0.6.0. Contract: [document 09](../contracts/09-lookup-and-discovery.md), QRY-LEX-1.

Find the members whose text has a word of the query. The match is lexical and exact. It is not a semantic search.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `query` | The words to find. |
| `limit`, `offset` | Optional. The page of the result. |
| `search_profile` | Optional. The search profile. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('search', {'context': agent, 'query': 'seal'})
print(out['status'], [(i['kind'], i['count']) for i in out['items']])
```

Output:

```text
ok [('reading', 1)]
```

## Result

One item for each match, with the fields `ref`, `kind` and `count`. A result with no items states only that the context has no match.

## Typed reasons

- `bounds_reached`: The result stopped at `limit`. The status is `partial`.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
