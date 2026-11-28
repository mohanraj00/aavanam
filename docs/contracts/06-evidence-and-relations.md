# Evidence and relations

State: normative. Prefix: `REL`. Profile: `aavanam/base/v1`.

## Scope

This document defines evidence links, source selectors, derivation records, retraction, supersession and conflicts. The store reports conflicts. It does not apply an eligibility policy. This document also defines the removal of an object from the Evidence section and the log of the removals.

## Topics

| Topic | Subject |
|---|---|
| `EVD` | Evidence links and subjects |
| `SEL` | Source selectors, coordinate systems and normalization profiles |
| `DRV` | Derivation records |
| `RET` | Retraction and supersession targets |
| `CNF` | Conflict reporting |
| `RMV` | Removal from the Evidence section and its records |

## Clauses

The topics `DRV` and `CNF` have no clauses yet.

### Evidence links

**REL-EVD-1.** The request of `evidence` has the fields `context` (CTX-OPN-3) and `subject`, one reference. If the subject is not in the store, the result has status `not_found` and reason `unknown_ref`. If the subject is in the store but not in the read context, the result has status `not_found` and reason `not_in_context`. In these two cases, the result has no items (STS-ENV-3 rule 1).

**REL-EVD-2.** The result has one support item for each `evidence` record that is a member of a RelationSet in the read context and has the subject as its `subject`. A support item has status `ok`, the field `relation` with the value `support`, and the field `ref`, the reference of the `evidence` record. The `subject_selector` of a record does not limit the result. If an `evidence` record is in no RelationSet of the read context, it is not a support item, also if the store has it.

**REL-EVD-3.** A withdrawal item (REL-RET-3) has status `ok`, the field `relation` with the value `withdrawal`, and the fields `ref`, `target` and `origin`. `ref` is the retraction or supersession, and `target` is the withdrawn record. The store MUST NOT remove the support item of a withdrawn record. A withdrawal that is in the store but in no RelationSet of the read context is not returned (CTX-OPN-4).

**REL-EVD-4.** The order of the items is not specified. A consumer MUST NOT depend on it.

### Selectors

**REL-SEL-1.** A `selector` value is a JSON object with the field `selector_profile`, a string. The other fields depend on the selector profile. If a `selector` value is not such an object, the store MUST reject the write with status `failed` and reason `field_type` (KND-SCH-4). In `operand_selectors`, `null` is also a valid value (KND-SCH-7).

**REL-SEL-2.** If the store does not support a selector profile, `put` keeps the selector as opaque data. If an operation must apply that selector, the affected item MUST have status `unsupported` and reason `unsupported_selector`. The store MUST NOT apply a different selector, and MUST NOT select the whole target in its place.

**REL-SEL-3.** The base profile defines the selector profile `aavanam/bytes/v1`, and each store MUST support it. Its fields are `selector_profile`, `start` and `end`. `start` and `end` are integers, and 0 <= `start` <= `end`. The selector selects the payload bytes from offset `start` up to offset `end`, without the byte at `end`. If a value of this profile has a different field, a missing field or a value outside these rules, the store MUST reject the write with status `failed` and reason `field_type`.

**REL-SEL-4.** A selector of profile `aavanam/bytes/v1` applies only to a target that has a `payload` field. For another target, the affected item has status `unsupported` and reason `unsupported_selector`. The store checks the range when it applies the selector. If `end` is more than the payload size, the affected item has status `failed` and reason `selector_range`.

**REL-SEL-5.** The selector profiles whose names start with `aavanam/unsupported/` are reserved for conformance tests. A store MUST NOT support them. Thus REL-SEL-2 applies to each of them.

### Retraction and supersession

**REL-RET-1.** In the base profile, a `retraction` or `supersession` record MUST target a record of kind `evidence` or `check`. The `replacement` of a supersession MUST have the same kind as its `target`.

**REL-RET-2.** If a `retraction` or `supersession` has a target of a different kind, the store MUST reject the write with status `unsupported` and reason `unsupported_target_kind` (KND-REF-3). The store does not keep the record. If the target and the replacement both have permitted kinds, but the replacement kind is not the target kind, the store MUST reject the write with status `failed` and reason `replacement_kind`. The store checks `unsupported_target_kind` before `replacement_kind` (STS-MAL-3).

**REL-RET-3.** `evidence()` MUST return each reachable retraction or supersession of a returned record as a conflicting item, with its origin. A record is reachable if a RelationSet in the read context contains it. The store applies no eligibility policy. The application decides which origins can defeat support.

**REL-RET-4.** A correction of a resource is a new resource revision and a changed BindingSet. A defect notice about a resource is an `observation`. Neither one is a retraction or a supersession.

**REL-RET-5.** An extension profile can define a withdrawal kind with more target kinds only if it specifies how each consumer operation shows the withdrawal. If it does not, the store MUST reject that kind (KND-REG-3).

**REL-RET-6.** Omission of a record from a newer RelationSet is not a withdrawal. Only these records withdraw a record: a `retraction`, a `supersession`, or a record of an extension withdrawal kind that REL-RET-5 permits and that the store supports.

### Removal from the Evidence section

**REL-RMV-1.** `remove` removes one object of the Evidence section from a writer store, and `removals` gives the log of the removals. Only a writer store has sections (PUB-SEC-1). On a device store and on a local output store, the two operations MUST give status `unsupported` with one reason, `unsupported_role`. The store checks the role after the operation (STS-MAL-4) and before the request. The result has no items, and the store does not change.

**REL-RMV-2.** The request of `remove` has the fields `ref`, a reference, and `reason`, a string of 1 to 1024 characters that agrees with I-JSON (KND-SCH-4). If the request has a different field, the result is `failed` with `unknown_field`. If a field is absent, the result is `failed` with `field_count`. If a field has a value of a different form, the result is `failed` with `field_type`. The store checks these in this order. The result has no items, and the store does not change (STS-MAL-2).

**REL-RMV-3.** If the request form is correct, the store checks `ref` in these steps, in this order. It stops at the first step that gives an outcome. The result has the status and one reason of that step, with `ref` in the `refs` of the reason. It has no items, and the store does not change.

1. If the store does not have the object, and no removal record has the reference, the result is `not_found` with `unknown_ref`.
2. If the store does not have the object, and a removal record has the reference, the result is `not_found` with `removed`.
3. If the kind of the object is not `source`, `reading`, `excerpt` or `evidence`, the result is `unsupported` with `unsupported_target_kind`. These kinds are the kinds that a `serving` collection does not permit (PUB-SEC-3).

**REL-RMV-4.** If no step of REL-RMV-3 gives an outcome, the store does these actions in one transaction:

1. It removes the descriptor of the object.
2. It removes the bytes of each payload digest that the descriptor names, unless a different object of the store names the same digest.
3. It removes each deferred check record of the object (BUN-IMP-10).
4. It adds one removal record (REL-RMV-5).
5. It ends each read context that has the object as a member (CTX-LIF-4).

The result is `ok` with one item that has the fields of the removal record. The store removes no other object. Thus the application calls `remove` for each object that holds the content, for example a source, its readings and its excerpts.

**REL-RMV-5.** A removal record has the fields `sequence`, `ref`, `kind`, `time` and `reason`. `sequence` counts the removal records of the store from 1. `ref` and `kind` are the reference and the kind of the removed object. `time` is the UTC time of the store clock in the form `YYYY-MM-DDTHH:MM:SSZ` (RFC 3339). `reason` is the value of the request. A removal record MUST NOT have a different field. It holds no other field of the removed descriptor, no payload digest and no bytes. The application MUST NOT put removed content in `reason`. The store does not check this.

**REL-RMV-6.** The request of `removals` has no field. A field gives `failed` with `unknown_field` and no items. The result is `ok`, with the removal records of the store in the order of their sequence numbers. Each record is one `ok` item with its fields. No operation changes or removes a removal record. `removals` reads the store, not a read context, so its envelope field `context` is null.

**REL-RMV-7.** After an `ok` `remove`, no operation of the store gives the descriptor of the removed object, and no operation gives the bytes that the store removed. This applies to each read context and to each later open of the store. This topic does not require the removal of the bytes from the storage media, a journal, a backup, a bundle or a store that imported the object. A reference names a removed object if a removal record has the reference and the store does not have the object. For such a reference, an outcome that has the reason `unknown_ref` or `not_in_context` by a different clause has the reason `removed` in its place. The status stays `not_found`. Two exceptions apply. In a layered context, the reason stays `unknown_ref` (LYR-ISO-2). A `put` of a descriptor with such a reference gives `missing_target` (KND-REF-2).

**REL-RMV-8.** A removal does not change a different object. The store MUST keep the `refs` of the removed object (KND-DEP-1) as a list of references, and it uses them only to follow a closure. Thus the closure of a root (KND-DEP-3) has the same references before and after a removal. A removed object is in a closure only as a reference. It is not a member of a view (CTX-VIW-1), and it is not in the content of a snapshot (LIF-ITM-1). Thus an operation that reads the members of a read context gives no item for it. A reference path in a read context (QRY-BDL-3, CHG-IMP-4) does not go through a removed object, because the object is not a member. An object that a root reaches only through a removed object stays a member. A snapshot that names a removed object does not change, and a read context with that snapshot as a root opens. Each other member reads as before the removal.

**REL-RMV-9.** Each `ok` item of `open_context` has the field `removed`: the list of the removed objects in the closure of the root of the item, in ascending string order. The list is empty if the closure has no removed object. Thus a consumer that opens a read context knows each object that a removal took from its view. A read context that the store opened before the removal ends (REL-RMV-4), so a consumer cannot read a view with an unreported removal.

**REL-RMV-10.** `export`, `execution_inputs` and `build_inputs` are complete operations (STS-ENV-4). If the closure of a bundle root has a removed object, `export` MUST give status `not_found`, with one reason `removed` for each such object. If the closure of the covered set of a lock has a removed object, `execution_inputs` and `build_inputs` MUST give the same outcome. The `refs` of each reason name the removed object. The result has no items, no bundle and no lock, and the store does not change. The store applies this check after each other check of the operation. To export or to lock such content, the application writes a snapshot that does not name the removed object.

**REL-RMV-11.** A later `put` or `import` of the descriptor of a removed object adds the object again (IDN-CAN-3). The removal record stays. After that write, the reference does not name a removed object (REL-RMV-7), and the object is a member of each read context that the store opens later with a root that reaches it. The store does not refuse this write. It applies no removal policy. An application that must prevent the write reads `removals` first. The write does not change a read context that is open at that time (REL-RMV-13).

**REL-RMV-12.** A removal is not a withdrawal (REL-RET-6). A `retraction` or a `supersession` is a record that stays in the store with its target. A removal takes the object from the store. `gc_collect` removes the objects that no retained root reaches (LIF-GC-1). It adds no removal record, and its result reports each removed object. A removal from the producer section is the operation `producer_forget` (PRD-RMV-1).

**REL-RMV-13.** A `put` or an `import` that adds a removed object again (REL-RMV-11) MUST NOT change an open read context (CTX-OPN-2). An open read context that reported the object in `removed` (REL-RMV-9) does not have the object as a member after the write. In that context, the object is in the store but not in the read context (CTX-OPN-4). Thus an operation in that context gives no item for the object and no value from it. For example, `evidence` gives no support item for it (REL-EVD-2). A reference path in that context does not go through the object (REL-RMV-8). An outcome for its reference has status `not_found` with the reason `not_in_context`. In a layered context, the reason is `unknown_ref` (LYR-ISO-2). The check of REL-RMV-10 applies to the object in that context, with this reason in the place of `removed`. Thus `export`, `execution_inputs` and `build_inputs` in that context give no bundle and no lock for a closure that has the object. The object stays out of that context until the context ends (CTX-LIF-2). To read the object, the application opens a new read context.
