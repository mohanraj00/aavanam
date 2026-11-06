# Publication

State: draft. Prefix: `PUB`. Profile: `aavanam/base/v1`.

## Scope

This document defines snapshots, candidates, external acceptance receipts and atomic head updates in one catalog. The store does not decide acceptance. An external workflow adapter supplies the receipt.

The store keeps a receipt as opaque content (KND-OPQ-2). An acceptance policy is an application component. The store calls it and records its decision. The store does not implement a policy, and it does not read a receipt payload.

## Topics

| Topic | Subject |
|---|---|
| `SNP` | Snapshots |
| `CND` | Candidates and expected heads |
| `RCP` | Acceptance receipt validation |
| `HED` | Head updates, idempotency and dispositions |
| `GAT` | Gated collections |
| `SEC` | Storage sections |
| `LOG` | Publication history |
| `ACT` | Activations |

## Clauses

### Snapshots

**PUB-SNP-1.** The request of `create_snapshot` has the fields of a `snapshot` descriptor, without `descriptor_profile` and `kind` (KND-SCH-1). The store makes the descriptor from the request, with `descriptor_profile` `aavanam/base/v1` and `kind` `snapshot`. Thus a request field that is not in the schema of `snapshot` gives `unknown_field` (KND-SCH-2).

**PUB-SNP-2.** The store checks the descriptor in the steps of STS-MAL-3, from step 3. For example, a member that is not in the store gives `missing_target`, and two native mappings with one native identity and two subjects give `native_id_conflict` (IDN-NAT-3). The result has one item. STS-ENV-3 rule 6 gives the envelope status.

**PUB-SNP-3.** If the result has status `ok`, the store has the descriptor, and the item has the field `ref`, the revision of the descriptor (IDN-CAN-2). A second call with the same request gives the same `ref` and does not change the store (IDN-CAN-1). If the result is not `ok`, the store does not change (STS-ENV-4).

**PUB-SNP-4.** A snapshot names the members of one collection, its `collection` value, at one revision. `create_snapshot` and `put` do not change a head. Only `publish` makes a snapshot the head of its collection (PUB-HED-3).

### Candidates and expected heads

**PUB-CND-1.** The request of `create_candidate` has these fields:

| Field | Value | Count |
|---|---|---|
| `snapshot` | The reference of a `snapshot` | `1` |
| `checks` | A set of references to `check` records (KND-SCH-5) | `0..n` |
| `policy` | The name of an acceptance policy (PUB-RCP-1), a string | `1` |
| `expected_head` | The reference of the snapshot that the candidate expects as the head of its collection, or null for no head | `1` |

If the request has a different field, a field with a count that the table does not permit, or a value of a different type, the result has status `failed` with reason `unknown_field`, `field_count` or `field_type`. The result has no items.

**PUB-CND-2.** If the request form is correct, the store applies these checks in order. It stops at the first check that fails. The result has one item with the reasons of that check, one reason for each affected value. STS-ENV-3 rule 6 gives the envelope status.

1. Each reference in `snapshot` and `checks` is in the store. If not, the status is `not_found` with reason `unknown_ref`.
2. `snapshot` has kind `snapshot`, and each reference in `checks` has kind `check`. If not, the status is `unsupported` with reason `unsupported_target_kind`.
3. `policy` is the name of a registered policy (PUB-RCP-1). If not, the status is `unsupported` with reason `unsupported_profile`.
4. The `subject` of each check is in the closure of the snapshot (KND-DEP-3). If not, the status is `incompatible` with reason `check_outside_snapshot`.
5. If the catalog has a candidate with the identity of the request (PUB-CND-3), the status is `ok`. The store does not apply step 6.
6. `expected_head` is the current head of the collection of the snapshot (PUB-HED-1). Null agrees only with a collection that has no head. If not, the status is `incompatible` with reason `head_moved`.

**PUB-CND-3.** The identity of a candidate is `sha256:` and the 64 lowercase hexadecimal digits of the SHA-256 digest of one JSON object, encoded with RFC 8785 in UTF-8. The object has `format` with the value `aavanam-candidate/v1`, and the request fields `snapshot`, `checks`, `policy` and `expected_head`. `checks` is the list of the check references in ascending string order. An absent `checks` is an empty list. If the result has status `ok`, the item has the fields `candidate`, the identity, and `collection`, the `collection` value of the snapshot.

**PUB-CND-4.** A candidate is a catalog record, not a descriptor (KND-REG-5). It does not change after the store writes it. A change of the snapshot, a check, the policy or the expected head gives a new candidate. A second call with the same request gives the same identity and does not change the store, also after a `publish` changed the head (step 5 of PUB-CND-2). If the result is not `ok`, the store does not change (STS-ENV-4).

**PUB-CND-5.** The store does not run a check, and it does not read the `outcome` of a check. Thus a candidate can have a check with any outcome. The policy decides if it accepts the candidate (PUB-RCP-2).

### Acceptance receipt validation

**PUB-RCP-1.** An acceptance policy is a validator that the application owns. The application registers it with the store under its name before a call. The store does not supply a validator, and the base profile registers no policy. The store compares policy names only as strings.

**PUB-RCP-2.** The input of a validator is the identity of a candidate, the reference of one `receipt` and the reference of the `activation` of the request. The activation is null if the request has no activation. Its output is accepted or not accepted. The application reads the receipt and the activation and decides. The store MUST NOT parse or evaluate the receipt payload, or the payloads of `approval` and `report` (KND-OPQ-2). The store MUST NOT accept a candidate without the decision of the validator of its policy. A request field, a check outcome, a receipt value or an activation does not replace the decision.

**PUB-RCP-3.** The store does not keep the head update open while the validator runs. After the validator accepts, the store applies step 5 of PUB-HED-3 as one atomic step. That step applies step 3 and the activation checks (PUB-ACT-3) again before it compares the head. Thus a competing `publish` that changes the head during the validation gives `head_moved`. A competing `publish` of the same candidate gives the outcome of step 3 (PUB-HED-5).

### Head updates, idempotency and dispositions

**PUB-HED-1.** The base profile has one catalog for each store. The catalog has zero or one head for each collection. A head is the reference of one snapshot of that collection. An empty store has no head. Only `publish` changes a head.

**PUB-HED-2.** The request of `publish` has the fields `candidate`, a candidate identity (PUB-CND-3), and `receipt`, a reference. It can also have the field `activation`, a reference. If the request has a different field, does not have `candidate` or `receipt`, or has a value of a different type, the result has status `failed` with reason `unknown_field`, `field_count` or `field_type`. The result has no items.

**PUB-HED-3.** If the request form is correct, the store applies these steps in order. It stops at the first step that gives an outcome. The result has one item with the field `candidate`. STS-ENV-3 rule 6 gives the envelope status.

1. If the catalog has no candidate with the identity, the status is `not_found` with reason `unknown_candidate`. If `receipt` or `activation` is not in the store, the status is `not_found` with reason `unknown_ref`, one reason for each reference that is not in the store. If more than one applies, the item has each reason.
2. If `receipt` does not have kind `receipt`, or `activation` does not have kind `activation`, the status is `unsupported` with reason `unsupported_target_kind`, one reason for each such reference.
3. If the store published the candidate before, the status is `ok` (PUB-HED-5). The store does not call the validator. If the store did not publish the candidate before, the store applies the activation checks (PUB-ACT-3) before step 4.
4. If the validator of the policy of the candidate does not accept the receipt and the activation for the candidate (PUB-RCP-2), the status is `incompatible` with reason `receipt_rejected`.
5. The store applies these actions in order, as one atomic step (PUB-RCP-3). It stops at the first action that gives an outcome.
   1. If the store published the candidate after step 3, the outcome of step 3 applies. If not, the store applies the activation checks again (PUB-ACT-3).
   2. If the current head of the collection is not the expected head of the candidate, the status is `incompatible` with reason `head_moved`.
   3. The store makes the snapshot of the candidate the head of its collection, and it records the publication of the candidate with the receipt and the activation (PUB-LOG-2). The status is `ok`.

**PUB-HED-4.** An `ok` item has the fields `disposition` and `head`. `head` is the head of the collection after the call. `disposition` has one of these values:

| Value | Step | Meaning |
|---|---|---|
| `published` | 5.3 | This call published the candidate |
| `already_published` | 3 or 5.1 | An earlier call published the candidate, and its snapshot is still the head |
| `superseded` | 3 or 5.1 | An earlier call published the candidate, and a later publication changed the head |

An item with reason `head_moved` also has the field `head`, the current head.

**PUB-HED-5.** The store publishes a candidate one time only. A later `publish` of the same candidate, with the same or a different receipt, does not record a second publication, does not call the validator and does not change the store. It MUST NOT move a head back to an earlier snapshot.

**PUB-HED-6.** `publish` is a complete operation. If the result is not `ok`, the store does not change (STS-ENV-4). A head of a collection in a different catalog is not in the scope of `publish`. Cross-catalog contexts are reserved (document 16).

### Gated collections

**PUB-GAT-1.** A gated collection is a collection name that the application declares as gated. The application makes the declaration when it opens the store, before the first call of that open. The declaration is store configuration. It is not a request field, a descriptor or a catalog record. The base profile declares no gated collection. A declaration can name a collection that has no snapshot or no head.

**PUB-GAT-2.** The store MUST keep each declaration for the life of the store. A later open of the store keeps it, also if the application does not declare it again. The base profile has no operation that removes a declaration. A gate is on a collection, not on a policy, so a candidate with a different policy does not avoid it.

**PUB-GAT-3.** The gate check of `publish` is check 1 of the activation checks (PUB-ACT-3): if the collection of the snapshot of the candidate is gated and the request has no activation, the item has status `incompatible` and reason `activation_required`. The item has no other reason, and the store does not change (STS-ENV-4). The store applies the activation checks after step 3 of PUB-HED-3 and before step 4. Thus a repeat `publish` of a published candidate gives the outcome of step 3 without the gate check, and the store does not call the validator if the gate check fails. An open can declare a gate while a `publish` of a different open runs its validator. Action 1 of step 5 applies the activation checks again in the atomic step (PUB-RCP-3). Thus that `publish` gives the outcome of the checks in the gated collection, and the store does not change if a check fails.

### Storage sections

**PUB-SEC-1.** A storage section states the purpose of the data of a collection. The application can declare one section for a collection when it opens the store, before the first call of that open. The declaration is store configuration. It is not a request field, a descriptor or a catalog record. The base profile has two sections: `serving`, for the content that consumers read live, and `evidence`, for the material that supports that content. A collection with no declaration has no section, and this topic does not apply to it.

**PUB-SEC-2.** The store MUST keep each declaration for the life of the store. A later open of the store keeps it, also if the application does not declare it again. The base profile has no operation that removes a declaration. A repeat of a declaration has no effect. The store MUST refuse a declaration with a different section for the collection. The store MUST also refuse a first declaration for a collection that has a snapshot in the store, because the store did not check that snapshot (PUB-SEC-3). The declaration fails, and the store does not change. A declaration and a snapshot write from two opens of one store MUST NOT both succeed if the result breaks PUB-SEC-3. Thus the store checks the section again in the transaction of the write. A device store and its local output store have one set of declarations and snapshots for this topic (BUN-IMP-8).

**PUB-SEC-3.** If the `collection` of a snapshot has the section `serving`, the closure of the snapshot (KND-DEP-3) MUST NOT have a descriptor of kind `source`, `reading`, `excerpt` or `evidence`. This check is in step 6 of STS-MAL-3, so it applies to each operation that checks a descriptor in these steps, for example `create_snapshot` and `put`. The item has status `failed`, with one reason `section_violation` for each such descriptor. The `refs` of each reason list these descriptors.

**PUB-SEC-4.** Thus a bundle of a root in a `serving` collection has no descriptor of these kinds and no payload bytes of these descriptors (BUN-EXP-2). The `export` operation has no check for a section. An `overlay` on the content of a `serving` collection can keep a source locator in its `values`, for example a source URL. The store does not interpret it (KND-OPQ-3).

**PUB-SEC-5.** An `evidence` collection has no restriction on the kinds in its closure. A read context with a root in a `serving` collection and a root in an `evidence` collection reads the evidence of a subject with `evidence`. The result gives the evidence records (REL-EVD-2). The application follows `supports` to read the excerpt.

**PUB-SEC-6.** A section changes no identity, reference, closure, status or reason, except by PUB-SEC-3. All the sections are in one logical store, so a reference from one section to a different section resolves (KND-REF-2). An implementation MAY keep each section in a different backend. This topic sets no time limit for a read of an `evidence` collection.

### Publication history

**PUB-LOG-1.** The request of `publications` has the field `collection`, a string. If the request has a different field, does not have `collection`, or has a value of a different type, the result has status `failed` with reason `unknown_field`, `field_count` or `field_type`. The result has no items.

**PUB-LOG-2.** Action 5.3 of PUB-HED-3 records one publication in the catalog. The record has the candidate, the receipt of the request, the activation of the request, the head of the collection before the action and the head after the action. The activation is null if the request has no activation. The head before the action is null if the collection had no head. Only action 5.3 records a publication. Thus a repeat `publish` records no publication (PUB-HED-5). No operation changes or removes a record.

**PUB-LOG-3.** If the request form is correct, the result has status `ok`, and one item for each recorded publication of the collection, in the order of the head updates. Each item has status `ok` and the fields `candidate`, `receipt`, `activation`, `previous_head` and `head`, with the values of PUB-LOG-2. A collection with no recorded publication gives status `ok` and no items. A publication of a different collection is not in the result. `publications` does not change the store.

**PUB-LOG-4.** `publications` reads the catalog of the store (PUB-HED-1), not a read context. Thus it does not need a capability (CTX-PRJ-1). The result has only the publications that `publish` recorded in this store.

### Activations

**PUB-ACT-1.** An `activation` is a descriptor (document 02). It records the decision of an application that qualifies one candidate before its publication. The application writes it with `put`. At `put`, the store applies the checks of each descriptor, for example the schema (KND-SCH-1 to KND-SCH-5) and the references (KND-REF-2, KND-REF-3). The store does not compare `candidate`, `base` or `partition` with a candidate at `put`, and it does not check `lock` or `removed` against a snapshot. The store does not read the payload of `approval` or `report` (KND-OPQ-2). It does not evaluate `effective_from`, `effective_until`, `request_key`, `producer`, `runner` or `approver`. It keeps them as strings.

**PUB-ACT-2.** These terms apply to the activation checks:

- The covered set of an `execution-lock` is the closure (KND-DEP-3) of its `binding_set` and of each of its `context_inputs` (LCK-EXE-4, LCK-EXE-5). The covered set of a `build-lock` is the closure of each of its `inputs` (LCK-BLD-3). The lock descriptor is not in its covered set.
- A member of a snapshot is a reference in its fields `units`, `schemas`, `relation_sets`, `overlay_sets`, `observations` and `native_mappings`. An object in the closure of a member is a part of that member, not a member.
- The base members are the members of the snapshot `base` of the activation. If the activation has no `base`, there are no base members.
- A changed member is a member of the snapshot of the candidate that is not a base member.
- A member of the candidate replaces a base member if the two have the same kind, the same `logical_id` and different references. A removed member is a base member that is not a member of the snapshot of the candidate, and that no member of the candidate replaces. A member without a `logical_id` replaces no member.

**PUB-ACT-3.** The activation checks of `publish`. The store applies them in this order. It stops at the first check that fails. If a check fails, the item has status `incompatible` and the reasons of that check, and the store does not change (STS-ENV-4). If the request has no activation, the store applies only check 1.

| Order | Check | Reason if it fails |
|---|---|---|
| 1 | If the collection of the snapshot of the candidate is gated (PUB-GAT-1), the request has `activation` (PUB-GAT-3) | `activation_required` |
| 2 | The `candidate` of the activation is the `candidate` of the request. The `base` of the activation is the `expected_head` of the candidate, and an absent `base` agrees only with a null `expected_head`. The `partition` of the activation is the `collection` of the snapshot of the candidate. One reason for each field that does not agree, with the activation in `refs` | `activation_mismatch` |
| 3 | The covered set of `lock` has no `snapshot`. One reason for each snapshot in the covered set | `lock_has_snapshot` |
| 4 | Each object in the covered set of `lock` is in the closure of the snapshot of the candidate. One reason for each object that is not in it | `lock_outside_snapshot` |
| 5 | Each changed member is in the covered set of `lock`. A member in `native_mappings` is not in the scope of this check. One reason for each changed member that is not in it | `change_outside_lock` |
| 6 | The removed members are exactly the references in `removed`. In a gated collection, `removed` is also empty. One reason for each removed member that is not in `removed`, and for each reference in `removed` that is not a removed member or that is in `removed` in a gated collection | `removal_refused` |

Each reason of checks 3 to 6 has the affected reference in `refs`. Each reason has an empty `capabilities`. Checks 2 to 5 and the exact rule of check 6 read only immutable objects. Thus in action 5.1 of PUB-HED-3, the store can use their results from step 3. It applies check 1 and the empty rule of check 6 again, because a gate can start during the validation (PUB-GAT-3).

**PUB-ACT-4.** The store applies checks 2 to 6 also in a collection that is not gated, if the request has an activation. Check 1 and the empty rule of check 6 apply only in a gated collection. A member of `native_mappings` stays in the snapshot checks of PUB-SNP-2.

**PUB-ACT-5.** The activation checks compare references and strings only. They do not prove that a change is correct, that a lock is complete or that an approval is valid. The store does not evaluate `effective_from` or `effective_until`, and it does not verify `approver`, `runner` or a signature (PUB-ACT-1). The validator decides (PUB-RCP-2).
