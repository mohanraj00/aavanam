# Statuses and reasons

State: draft. Prefix: `STS`. Profile: `aavanam/base/v1`.

## Scope

This document defines the closed status set, the stable reason codes and the result envelope of each operation. Other documents define the items and the fields of each result. This document is the only registry of statuses and reason codes.

## Topics

| Topic | Subject |
|---|---|
| `SET` | Closed status set |
| `RSN` | Reason codes and affected references |
| `ENV` | Result envelopes and per-item status |
| `MAL` | Typed failures for malformed input |

## Clauses

### Status set

**STS-SET-1.** Each result and each result item MUST have exactly one status from the status table. No other status value is permitted. An extension MUST NOT add a status. Only an envelope can have status `partial`. A result item MUST NOT have status `partial`.

**STS-SET-2.** Only `ok` is success. A consumer MUST treat each other status as not success. An unknown, missing, partial or unsupported result MUST NOT become a value, a default or a success.

**STS-SET-3.** An operation MUST return only the statuses that the operation table gives for it. If an operation has no row, it is not in the base profile, and a call returns `unsupported` with reason `unsupported_operation`.

**STS-SET-4.** `partial` permits research and context use of the `ok` items only. A `partial` result MUST NOT be used as permission for a partial execution or a partial build. `execution_inputs`, `build_inputs` and `export` MUST NOT return `partial`.

The status table:

| Status | Meaning |
|---|---|
| `ok` | The operation completed in its declared scope. Each item is `ok` |
| `partial` | Some items are `ok`. Other items have a different status, or the operation stopped at a declared bound |
| `not_found` | An exact reference, subject or payload is not in the store or not in the read context |
| `unresolved` | A needed input has no exact value in the read context, for example a role with no binding |
| `ambiguous` | More than one value is possible, and no declared precedence selects one |
| `incompatible` | Two exact inputs are present but do not agree, for example a runtime profile or a binding skew |
| `unsupported` | The operation, kind, target kind, profile or selector is not supported |
| `failed` | The operation did not complete, for example malformed input, an integrity error, an expired context or a bound before a result that it can return |

### Reason codes

**STS-RSN-1.** Each result that is not `ok` MUST have one or more reasons. Each item that is not `ok` MUST have one or more reasons. An `ok` result and an `ok` item MUST have an empty reason list.

**STS-RSN-2.** A reason is an object with the fields `code`, `refs` and `capabilities`. `code` MUST be a code in the reason table, or an extension code of the form `ext:<owner>/<name>/v<N>#<code>`. `refs` names each affected reference. `capabilities` names each affected capability or profile. The fields `refs` and `capabilities` can be empty lists. An optional field `detail` holds a text for persons. A consumer MUST NOT parse `detail`.

**STS-RSN-3.** Each reason code has one status in the reason table. A result or an item that has the code MUST have that status. An extension code MUST name one status from the status table.

**STS-RSN-4.** A reason code is stable. Its meaning MUST NOT change. If a code is withdrawn, its row stays with `(withdrawn)`, and the code is never used again.

The reason table:

| Code | Status | Meaning |
|---|---|---|
| `bounds_reached` | `partial` | The operation stopped at a declared bound after one or more `ok` items, for example a count or a budget |
| `items_not_ok` | `partial` | One or more items have a status other than `ok` |
| `missing_target` | `not_found` | A reference target is not in the store (KND-REF-2) |
| `unknown_ref` | `not_found` | An exact reference in the request is not in the store |
| `unknown_candidate` | `not_found` | The catalog has no candidate with the identity in the request (PUB-HED-3) |
| `not_in_context` | `not_found` | The object is in the store but not in the read context |
| `payload_missing` | `not_found` | The bytes of a payload are not in the store |
| `unbound_role` | `unresolved` | A declared role has no binding in the BindingSet |
| `no_head` | `unresolved` | A logical ID has no head in the read context, or a collection has no head in the catalog (CTX-VIW-2) |
| `unknown_column` | `unresolved` | A column in the request is not a column of the table (QRY-DAT-2) |
| `no_metadata` | `unresolved` | No overlay in the read context gives a value for a requested metadata key (QRY-DAT-2) |
| `multiple_heads` | `ambiguous` | A logical ID has more than one head in the read context |
| `overlay_conflict` | `ambiguous` | Two overlays give different values, and no precedence selects one |
| `collection_conflict` | `ambiguous` | A read context has two different snapshots of one collection (CTX-CMP-1) |
| `descriptor_profile_mismatch` | `incompatible` | Two descriptors use descriptor profiles that the operation cannot join |
| `runtime_profile_mismatch` | `incompatible` | A target profile does not agree with the declared runtime profile |
| `role_schema_mismatch` | `incompatible` | A bound object does not have the schema that its declared role gives (BND-ROL-2) |
| `schema_mismatch` | `incompatible` | A data resource does not have the schema that the request gives (QRY-DAT-2) |
| `binding_skew` | `incompatible` | Two collections pin different revisions of one shared input, and the skew policy rejects this (CTX-SKW-4) |
| `head_moved` | `incompatible` | An expected head is not the current head |
| `check_outside_snapshot` | `incompatible` | The subject of a check is not in the closure of the snapshot of the candidate (PUB-CND-2) |
| `receipt_rejected` | `incompatible` | The validator of the policy did not accept the receipt for the candidate (PUB-RCP-2) |
| `activation_required` | `incompatible` | The collection of the candidate is gated, and the `publish` request has no activation (PUB-GAT-3) |
| `activation_mismatch` | `incompatible` | The `candidate`, `base` or `partition` of the activation does not agree with the candidate of the `publish` request (PUB-ACT-3) |
| `lock_has_snapshot` | `incompatible` | The covered set of the lock of the activation has a snapshot (PUB-ACT-3) |
| `lock_outside_snapshot` | `incompatible` | An object in the covered set of the lock of the activation is not in the closure of the snapshot of the candidate (PUB-ACT-3) |
| `change_outside_lock` | `incompatible` | A member that the candidate adds or replaces is not in the covered set of the lock of the activation (PUB-ACT-3) |
| `removal_refused` | `incompatible` | The members that the candidate removes are not exactly the `removed` of the activation, or a gated collection has a removal (PUB-ACT-3) |
| `unknown_kind` | `unsupported` | The kind is not registered (KND-REG-3) |
| `unsupported_target_kind` | `unsupported` | A reference target has a kind that its field does not permit (KND-REF-3) |
| `unsupported_operation` | `unsupported` | The operation is not in the supported profile |
| `unsupported_profile` | `unsupported` | A named profile is not supported |
| `unsupported_selector` | `unsupported` | The selector form is not supported for this target |
| `unsupported_budget` | `unsupported` | The budget unit is not supported (QRY-BDL-1) |
| `unsupported_dependents` | `unsupported` | The kind of dependents is not supported (CHG-IMP-1) |
| `capability_unavailable` | `unsupported` | The read context does not have a capability that the operation or the lock needs (CTX-PRJ-4, LCK-CAP-2) |
| `read_only_store` | `unsupported` | The store is a device store, and the operation writes to it (BUN-IMP-6) |
| `reserved_profile` | `unsupported` | The feature is reserved in document 16 and not specified |
| `malformed_json` | `failed` | The input is not valid JSON |
| `duplicate_key` | `failed` | A JSON object has a key more than one time |
| `non_ijson_value` | `failed` | A value is outside I-JSON (RFC 7493) |
| `unknown_field` | `failed` | A field is not in the schema of the kind (KND-SCH-2) |
| `field_count` | `failed` | A field has fewer or more values than its count permits (KND-SCH-3) |
| `field_type` | `failed` | A value does not agree with its value type (KND-SCH-4) |
| `selector_range` | `failed` | A selector selects outside its target (REL-SEL-4, ADP-SEL-4) |
| `excerpt_mismatch` | `failed` | The bytes of an excerpt do not agree with its ranges in the parent payload (BUN-EXC-3) |
| `payload_form` | `failed` | A payload does not agree with the form that its data or selector profile gives (DAT-TBL-3, ADP-SEL-4) |
| `values_form` | `failed` | The values of an observation do not agree with the form that its observation type gives (OBS-COV-3) |
| `bundle_incomplete` | `failed` | A bundle does not have a reference target or payload bytes that the store does not have (BUN-IMP-2) |
| `native_id_conflict` | `failed` | One native identity has more than one subject in a snapshot (IDN-NAT-3) |
| `section_violation` | `failed` | The closure of a snapshot in a `serving` collection has a descriptor of a kind that the section does not permit (PUB-SEC-3) |
| `replacement_kind` | `failed` | A supersession replacement has a different kind than its target (REL-RET-2) |
| `digest_mismatch` | `failed` | Bytes do not agree with their digest |
| `size_mismatch` | `failed` | Bytes do not agree with their size |
| `context_expired` | `failed` | The read context expired before the operation started |
| `deadline_exceeded` | `failed` | The operation did not complete before its deadline |
| `storage_error` | `failed` | The storage layer returned an error |
| `incomplete_result` | `failed` | A declared bound stopped the operation, and the operation cannot return `partial` (STS-ENV-3) |
| `batch_aborted` | `failed` | The descriptor passed its checks, but the `put` did not commit (STS-ENV-5) |

### Envelopes

**STS-ENV-1.** Each result MUST have an envelope with the fields `status`, `reasons`, `operation`, `operation_profile` and `context`. `context` is the identity of the read context, or null for an operation that does not use a context. Other documents add fields to the envelope of each operation.

**STS-ENV-2.** If a result has items, each item MUST have its own `status` and `reasons`. An empty item list does not show that no such item exists. It shows only that the operation found no item in its declared scope.

**STS-ENV-3.** The status of an envelope MUST be the status of the first rule that applies. An operation permits `partial` only if its row in the operation table has `partial`.

1. If the operation did not complete as a whole, the status is the status of that outcome, with its reasons. Examples are an unsupported operation (STS-MAL-4), a malformed request (STS-MAL-1), `context_expired`, `deadline_exceeded` and `storage_error`. The items do not change this status.
2. If a declared bound stopped the operation, the operation permits `partial`, and one or more items are `ok`, the status is `partial` with reason `bounds_reached`. If one or more items are not `ok`, the envelope also has reason `items_not_ok`.
3. If a declared bound stopped the operation, and rule 2 does not apply, the status is `failed` with reason `incomplete_result`.
4. If each item is `ok`, or the item list is empty, the status is `ok`.
5. If one or more items are `ok`, and the operation permits `partial`, the status is `partial` with reason `items_not_ok`.
6. In all other cases, the status is the first status of the items that are not `ok`, in this order: `failed`, `unsupported`, `incompatible`, `ambiguous`, `unresolved`, `not_found`. The envelope reasons are the reasons of the items that have that status.

**STS-ENV-4.** An operation that does not permit `partial` is a complete operation, for example the operations of STS-SET-4 and `put`. A complete operation MUST return a lock, a bundle or a change to the store only with status `ok`. With each other status, the result MUST NOT have a lock or a bundle, and the store MUST NOT change (STS-MAL-2).

**STS-ENV-5.** If a `put` is not `ok`, each descriptor that passed its checks MUST have status `failed` and reason `batch_aborted`. Rule 6 of STS-ENV-3 uses only the items that have a reason other than `batch_aborted`. If each descriptor passed its checks, rule 1 or rule 3 of STS-ENV-3 gives the envelope status, for example with `storage_error` or `incomplete_result`.

The operation table:

| Operation | Document | Statuses |
|---|---|---|
| `put` | 02 | `ok`, `not_found`, `unsupported`, `failed` |
| `open_context` | 08 | `ok`, `not_found`, `unresolved`, `ambiguous`, `incompatible`, `unsupported`, `failed` |
| `get` | 09 | `ok`, `not_found`, `unresolved`, `unsupported`, `failed` |
| `query_data` | 09 | `ok`, `partial`, `not_found`, `unresolved`, `ambiguous`, `incompatible`, `unsupported`, `failed` |
| `search` | 09 | `ok`, `partial`, `unsupported`, `failed` |
| `context_bundle` | 09 | `ok`, `partial`, `not_found`, `unsupported`, `failed` |
| `evidence` | 06 | `ok`, `partial`, `not_found`, `unsupported`, `failed` |
| `coverage` | 07 | `ok`, `partial`, `not_found`, `unsupported`, `failed` |
| `observations` | 07 | `ok`, `partial`, `not_found`, `unsupported`, `failed` |
| `execution_inputs` | 10 | `ok`, `not_found`, `unresolved`, `ambiguous`, `incompatible`, `unsupported`, `failed` |
| `build_inputs` | 10 | `ok`, `not_found`, `unresolved`, `ambiguous`, `incompatible`, `unsupported`, `failed` |
| `render` | 14 | `ok`, `partial`, `not_found`, `unsupported`, `failed` |
| `compare` | 12 | `ok`, `partial`, `not_found`, `incompatible`, `unsupported`, `failed` |
| `impact` | 12 | `ok`, `partial`, `not_found`, `unsupported`, `failed` |
| `create_snapshot` | 13 | `ok`, `not_found`, `unsupported`, `failed` |
| `create_candidate` | 13 | `ok`, `not_found`, `incompatible`, `unsupported`, `failed` |
| `publish` | 13 | `ok`, `not_found`, `incompatible`, `unsupported`, `failed` |
| `publications` | 13 | `ok`, `failed` |
| `export` | 11 | `ok`, `not_found`, `unsupported`, `failed` |
| `import` | 11 | `ok`, `incompatible`, `unsupported`, `failed` |
| `workspace_open` | 16 | `unsupported` |
| `workspace_append` | 16 | `unsupported` |
| `workspace_read` | 16 | `unsupported` |
| `gc_collect` | 16 | `unsupported` |
| `lease_acquire` | 16 | `unsupported` |
| `lease_release` | 16 | `unsupported` |
| `open_cross_catalog_context` | 16 | `unsupported` |
| `query_build_inputs` | 16 | `unsupported` |
| `semantic_search` | 16 | `unsupported` |
| `declare_signed_collection` | 16 | `unsupported` |
| `rotate_key_set` | 16 | `unsupported` |
| `publish_signed` | 16 | `unsupported` |
| `rotate_root` | 16 | `unsupported` |
| `mark_compromise` | 16 | `unsupported` |

Conflicting evidence is not `ambiguous`. `evidence` returns each conflict as an item with its origin (REL-RET-3).

### Malformed input

**STS-MAL-1.** If the input of an operation is malformed, the result MUST be an envelope with status `failed` and a reason code from the reason table. An implementation MUST NOT let a raw exception, a stack trace or a storage error message cross the API.

**STS-MAL-2.** A write that fails MUST NOT change the store. A `put` of many descriptors writes all of them or none of them.

**STS-MAL-3.** The store MUST check a descriptor in these steps, in this order. Each step gives its checks in order.

1. JSON form: `malformed_json`, `duplicate_key`, `non_ijson_value`.
2. Kind: `unknown_kind`.
3. Schema: `unknown_field`, `field_count`, `field_type`.
4. References: `missing_target`, `unsupported_target_kind`, `replacement_kind`.
5. Payload bytes: `payload_missing`, `digest_mismatch`, `size_mismatch`.
6. Content: `excerpt_mismatch` (BUN-EXC-3), `native_id_conflict` (IDN-NAT-3), `section_violation` (PUB-SEC-3).

In step 1, a number is outside I-JSON if it is not finite as an IEEE 754 double, or if its canonical form (IDN-CAN-1) has a different value (RFC 7493 section 2.2). The notation has no effect. Thus `9007199254740993` and `9007199254740993e0` give `non_ijson_value`, but `100000000000000000000` and `1e20` do not. Each number that the store keeps has a canonical form that the store accepts again, so an exported bundle imports again (BUN-IMP-3).

The store stops at the first check that fails. The result gives the reasons of that check only, with one reason for each affected value. Thus each descriptor has one status, and vectors can expect one result for each input.

**STS-MAL-4.** The store MUST check the operation before its input. If the operation has no row in the operation table, the result is `unsupported` with reason `unsupported_operation`. If its row gives only `unsupported`, the result is `unsupported` with reason `reserved_profile`. In these two cases, the store does not check the input, and STS-MAL-1 does not apply. Next, a device store checks its role (BUN-IMP-6). If it gives `read_only_store`, the store does not check the input either.
