# `put`

This page describes release 0.6.0. Contract: [document 02](../contracts/02-kinds-and-typed-references.md), KND-SCH-1, STS-MAL-3, STS-ENV-5.

Write descriptors and their payload bytes to a writer store. A changed byte gives a new object with a new reference.

## Request

| Field | Value |
|---|---|
| `descriptors` | A list of descriptor JSON texts. |
| `payloads` | Optional. A map from a payload digest to the base64 bytes. |

## Example

The example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session.

```python
desc = json.dumps({'descriptor_profile': 'aavanam/base/v1', 'kind': 'schema', 'schema_profile': 'example/schema/v1',
                   'payload': {'digest': 'sha256:' + '0' * 64, 'media_type': 'application/json', 'size': 2}})
out = store.call('put', {'descriptors': [desc]})
print(out['status'], out['reasons'][0]['code'])
out = store.call('put', {'descriptors': [json.dumps({'descriptor_profile': 'aavanam/base/v1', 'kind': 'binding-set',
                                                      'bindings': {'intervals': table}})]})
print(out['status'], out['items'][0]['ref'] == bindings)
```

Output:

```text
not_found payload_missing
ok True
```

## Result

One item for each descriptor, in the request order. An `ok` item has the field `ref`, the digest of the descriptor. A second `put` of the same bytes gives the same reference.

## Typed reasons

- `payload_missing`: A payload digest has no bytes in the request or in the store.
- `missing_target`: A reference field names an object that is not in the store.
- `unknown_kind`: The kind is not registered.
- `unknown_field`: A field is not in the schema of the kind.

Each operation can also give the reasons of a malformed request (STS-MAL-1). See [the index](README.md#typed-reasons).
