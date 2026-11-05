# Evidence and relations

State: draft. Prefix: `REL`. Profile: `aavanam/base/v1`.

## Scope

This document defines evidence links, source selectors, derivation records, retraction, supersession and conflicts. The store reports conflicts. It does not apply an eligibility policy.

## Topics

| Topic | Subject |
|---|---|
| `EVD` | Evidence links and subjects |
| `SEL` | Source selectors, coordinate systems and normalization profiles |
| `DRV` | Derivation records |
| `RET` | Retraction and supersession targets |
| `CNF` | Conflict reporting |

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
