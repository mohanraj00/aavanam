# `render`

This page describes release 0.6.0. Contract: [document 14](../contracts/14-rendering.md), RND-DOC-1.

Render a document of members for people to read. A gap in the checks or the coverage shows as an entry.

## Request

| Field | Value |
|---|---|
| `context` | The identity of the context. |
| `roots` | A list of references. |
| `render_profile` | Optional. The render profile. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
out = store.call('render', {'context': agent, 'roots': [manual]})
print(out['status'], [i.get('gap') or i['kind'] for i in out['items']])
```

Output:

```text
ok ['source', 'no_check', 'no_coverage']
```

## Result

The fields `document` and `document_id`, and one item for each entry. A gap entry has the field `gap`.

## Typed reasons

- `not_in_context`: A root is not a member of the context.
- `unsupported_profile`: The render profile is not supported.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
