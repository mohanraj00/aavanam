# Observations and coverage

State: draft. Prefix: `OBS`. Profile: `aavanam/base/v1`.

## Scope

This document defines coverage results, explicit omissions, unknown denominators, freshness observations and observation scopes. Observations are append-only and scoped to subjects.

## Topics

| Topic | Subject |
|---|---|
| `COV` | Coverage, omissions and unknown denominators |
| `FRS` | Retrieval attempts and freshness |
| `SCP` | Observation scopes, cutoffs and imported observation sets |
| `SUB` | Observation relevance to subjects |

## Clauses

The topics `FRS` and `SCP` have no clauses yet.

### Coverage, omissions and unknown denominators

**OBS-COV-1.** The base profile defines the observation type `aavanam/coverage/v1`. An observation of this type is the statement of a producer about the coverage of each of its subjects. Its `values` is an object with these fields, and no other field:

| Field | Value |
|---|---|
| `unit` | A string with one or more characters, for example `pages` or `rows`. The store compares it only as a string |
| `covered` | An integer of 0 or more |
| `total` | An integer that is equal to `covered` or more, or null. Null is an unknown denominator |
| `omissions` | A list of objects. Each object has the field `reason`, a string with one or more characters. It can also have the field `selector`, a selector (REL-SEL-1) |

The store reads `values` only for this type. The `values` of each other observation type stays opaque. The store does not apply the `selector` of an omission.

**OBS-COV-2.** The request of `coverage` has the fields `context` (CTX-OPN-3) and `subject`, one reference. It can also have the field `observation_type`, a string. The default is `aavanam/coverage/v1`. The store checks the request in this order:

1. If a request field does not agree with this clause, the result has status `failed` and reason `field_type`.
2. If `observation_type` is not `aavanam/coverage/v1`, the result has status `unsupported` and reason `unsupported_profile`.
3. OBS-SUB-1 gives `unknown_ref` or `not_in_context` for the subject.

In each of these cases, the result has no items (STS-ENV-3 rule 1).

**OBS-COV-3.** The result has one item for each observation of type `aavanam/coverage/v1` in the read context that has the subject in its `subjects` (OBS-SUB-2). If its `values` agree with OBS-COV-1, the item has status `ok` and the fields `ref`, `unit`, `covered`, `total` and `omissions`, with the values of the observation. If not, the item has status `failed` and reason `values_form`, and no other field. The order of the items is not specified (OBS-SUB-4). STS-ENV-3 gives the envelope status.

**OBS-COV-4.** The store MUST NOT compute a ratio, give a value to a null `total`, join two observations or remove an omission. A null `total` does not state that the coverage is complete. If the result has status `ok`, an empty item list states only that the read context has no coverage observation of the subject. It does not state complete coverage or zero coverage (STS-ENV-2).

### Observations of a subject

**OBS-SUB-1.** The request of `observations` has the fields `context` (CTX-OPN-3) and `subject`, one reference. If the subject is not in the store, the result has status `not_found` and reason `unknown_ref`. If the subject is in the store but not in the read context, the result has status `not_found` and reason `not_in_context`. In these two cases, the result has no items (STS-ENV-3 rule 1).

**OBS-SUB-2.** The result has one item for each `observation` in the read context that has the subject in its `subjects`. The item has status `ok` and the field `ref`, the reference of the observation. An observation is in the read context only if it is in the closure of a root (CTX-OPN-2). The store MUST NOT return an observation that is in the store but not in the read context, also if it has the same subject.

**OBS-SUB-3.** Observations are not global to the store. An observation in the snapshot of one collection is not in a read context of a different collection, unless a root of that context has it in its closure.

**OBS-SUB-4.** The order of the items is not specified. A consumer MUST NOT depend on it.
