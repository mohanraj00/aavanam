# `search`

This page describes release 0.6.0. Contract: [document 09](../contracts/09-lookup-and-discovery.md), QRY-LEX-1.

Find the readings and the excerpts whose payload bytes contain the exact UTF-8 bytes of the query. The store does not change the case, does not normalize Unicode and does not divide the text into words. It is not a semantic search.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `query` | The exact text to find. |
| `limit`, `offset` | Optional. The page of the result. |
| `search_profile` | Optional. The search profile. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
for query in ('seal', 'eal', 'Seal', 'seal bearing'):
    out = store.call('search', {'context': agent, 'query': query})
    print(query, out['status'], [(i['kind'], i['count']) for i in out['items']])
```

Output:

```text
seal ok [('reading', 1)]
eal ok [('reading', 1)]
Seal ok []
seal bearing ok []
```

## Result

One item for each match, with the fields `ref`, `kind` and `count`. `count` is the number of occurrences that do not overlap. A result with no items states only that the context has no match.

## Typed reasons

- `bounds_reached`: The result stopped at `limit`. The status is `partial`.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
