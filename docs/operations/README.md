# Operations

This page describes release 0.6.0. It has one page for each operation of the base profile and of the profiles in documents 19 and 24.

## How to call an operation

In the Python reference, each operation is one call: `store.call(operation, request)`. The request is a JSON object. The result is an envelope with these fields:

| Field | Value |
|---|---|
| `status` | One status of the closed set below |
| `reasons` | The typed reasons of the status. Each reason has `code`, `refs` and `capabilities` |
| `items` | One item for each result, each with its own `status` and `reasons` |
| `context` | The read context of the call, or null |
| `operation`, `operation_profile` | The operation and its profile |

No raw exception crosses the API. The backend service gives the read operations over HTTP, with `POST /v1/op/<operation>` on the read binding `aavanam-read/v1` (NET-SCP-1 in document 22). An operation that writes or removes content gives HTTP status 404 on that listener (NET-SCP-3). The service has a separate listener for writes (NET-TRN-5).

Each example continues the [quickstart](../../README.md#try-it). Run the quickstart first, in the same Python session. The test [tests/test_operation_docs.py](../../tests/test_operation_docs.py) runs each example and compares its output.

## Statuses

| Status | Meaning |
|---|---|
| `ok` | The operation completed in its declared scope |
| `partial` | Some items are `ok`, or the operation stopped at a declared bound |
| `not_found` | An exact reference is not in the store or not in the read context |
| `unresolved` | A needed input has no exact value in the read context |
| `ambiguous` | More than one value is possible, and no declared precedence selects one |
| `incompatible` | Two exact inputs do not agree |
| `unsupported` | The operation, kind, profile or selector is not supported |
| `failed` | The operation did not complete, for example malformed input |

Unknown, missing and unsupported never become a value. [Document 03](../contracts/03-statuses-and-reasons.md) gives the statuses of each operation and each reason code.

## Typed reasons

Each operation can give these reasons for a malformed request (STS-MAL-1):

- `unknown_field`: a field is not in the request form.
- `field_count`: a field is missing, or it has too many values.
- `field_type`: a value does not agree with its type.
- `context_expired`: the read context ended, or the store has no context with the identity.
- `storage_error`: the storage failed. The status is `failed`.

Each operation page gives the reasons that are specific to it.

## Write a snapshot

| Operation | Purpose |
|---|---|
| [`put`](put.md) | Write descriptors and payload bytes |
| [`create_snapshot`](create_snapshot.md) | Make an immutable snapshot of a collection |
| [`remove`](remove.md) | Remove an object of the Evidence section |
| [`removals`](removals.md) | List the removal records |
| [`gc_collect`](gc_collect.md) | Remove the content that no retained root reaches |

## Read in a context

| Operation | Purpose |
|---|---|
| [`open_context`](open_context.md) | Open a read context on snapshots or heads |
| [`close_context`](close_context.md) | End a read context |
| [`get`](get.md) | Check exact references |
| [`read`](read.md) | Read the descriptor and the bytes of a member |
| [`list_items`](list_items.md) | List the members by kind, logical ID or metadata |
| [`context_bundle`](context_bundle.md) | List the closure of roots with a budget |
| [`query_data`](query_data.md) | Read typed rows |
| [`search`](search.md) | Find a word in the members |
| [`search_text`](search_text.md) | Find a word in text artifacts, with a selector |
| [`semantic_search`](semantic_search.md) | Search through a retrieval index |
| [`evidence`](evidence.md) | Find the evidence of a subject |
| [`observations`](observations.md) | List the observations of a subject |
| [`coverage`](coverage.md) | Read the stated coverage of a subject |
| [`compare`](compare.md) | Compare the selected bytes of two members |
| [`impact`](impact.md) | List the dependents of changed objects |
| [`render`](render.md) | Render a document for people to read |

## Lock the inputs of a run

| Operation | Purpose |
|---|---|
| [`execution_inputs`](execution_inputs.md) | Lock the inputs of a script run |
| [`build_inputs`](build_inputs.md) | Lock the inputs of an emitter run |

## Transfer

| Operation | Purpose |
|---|---|
| [`export`](export.md) | Export the closure of roots as a bundle |
| [`import`](import.md) | Import a bundle |

## Publish

| Operation | Purpose |
|---|---|
| [`create_candidate`](create_candidate.md) | Propose a snapshot as the next head |
| [`publish`](publish.md) | Make a candidate the head |
| [`publications`](publications.md) | List the publication records |
| [`snapshot_changes`](snapshot_changes.md) | List the changes between two snapshots |
| [`item_history`](item_history.md) | List the revisions of one logical ID |

## Producer section

| Operation | Purpose |
|---|---|
| [`producer_put`](producer_put.md) | Store opaque bytes under a key |
| [`producer_get`](producer_get.md) | Read the bytes of a key |
| [`producer_forget`](producer_forget.md) | Remove the bytes of a key |
| [`producer_removals`](producer_removals.md) | List the removal records |
| [`producer_append`](producer_append.md) | Append an entry to a log |
| [`producer_read`](producer_read.md) | Read the entries of a log |

## Reserved operations

These operations are reserved in [document 16](../contracts/16-reserved-profiles.md). Each one gives status `unsupported` with reason `reserved_profile`:

`workspace_open`, `workspace_append`, `workspace_read`, `lease_acquire`, `lease_release`, `open_cross_catalog_context`, `query_build_inputs`, `declare_signed_collection`, `rotate_key_set`, `publish_signed`, `rotate_root`, `mark_compromise`.

```python
out = store.call('lease_acquire', {})
print(out['status'], out['reasons'][0]['code'])
```

Output:

```text
unsupported reserved_profile
```
