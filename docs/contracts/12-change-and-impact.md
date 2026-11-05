# Change and impact

State: draft. Prefix: `CHG`. Profile: `aavanam/base/v1`.

## Scope

This document defines comparison of two contexts, change reports scoped to a selector, and declared and inferred dependents. A source change is not a claim that content is false.

## Topics

| Topic | Subject |
|---|---|
| `CMP` | Context comparison |
| `SEL` | Selector-scoped change reports |
| `IMP` | Declared and inferred dependents |
| `STL` | Stale derived inputs |

## Clauses

The topics `CMP` and `STL` have no clauses yet.

### Selector-scoped change reports

**CHG-SEL-1.** The request of `compare` has the fields `context` (CTX-OPN-3), `before` and `after`, two references, and `selector`, a selector (REL-SEL-1). The result has one item. If `before` or `after` is not in the store, the item has status `not_found` and reason `unknown_ref`. If it is in the store but not in the read context, the item has status `not_found` and reason `not_in_context`. STS-ENV-3 gives the envelope status.

**CHG-SEL-2.** If both references are in the read context, the store applies the selector to `before` and to `after` (REL-SEL-2 to REL-SEL-4). If it can apply the selector to both, the item has status `ok` and the field `change`. The value is `unchanged` if the two selected byte sequences are equal, and `changed` if they are not.

**CHG-SEL-3.** If the store cannot apply the selector to a target, the item has the status and reason of that outcome. If the outcomes of `before` and `after` have different statuses, the item has the first status in the order of rule 6 of STS-ENV-3, with the reasons of that status. The store MUST NOT report `unchanged` for a selector that it did not apply.

**CHG-SEL-4.** `change` describes the selected bytes only. `unchanged` does not state that the content is true, and `changed` does not state that it is false.

### Declared and inferred dependents

**CHG-IMP-1.** The request of `impact` has the fields `context` (CTX-OPN-3) and `changed`, a list of one or more references. It can also have the fields `dependents`, a string with the default `declared`, and `limit`, an integer of 1 or more. The store checks the request in this order:

1. If a request field does not agree with this clause, the result has status `failed` and reason `field_type`.
2. If `dependents` is not `declared`, the result has status `unsupported` and reason `unsupported_dependents`. The base profile has no inferred dependents.

In these two cases, the result has no items (STS-ENV-3 rule 1).

**CHG-IMP-2.** If a reference of `changed` is not in the store or not in the read context, the result has one item for each such reference, in the order of `changed`, and no other item. QRY-GET-2 gives the status and reason of each item. Each item has the field `ref`. STS-ENV-3 gives the envelope status.

**CHG-IMP-3.** A dependent is an object of the read context that is not in `changed` and whose closure (KND-DEP-3) contains a reference of `changed`. Only the typed reference fields give the closure. The store MUST NOT get a dependent from a payload, from a `json` value or from `ext`, and MUST NOT infer one (BND-RUL-4). A record refers to its subject (KND-REF-6). Thus a record in the read context about a changed object is a dependent.

**CHG-IMP-4.** If each reference of `changed` is in the read context, the result has one item for each dependent. Each item has status `ok` and the fields `ref`, `kind` and `distance`. `distance` is the smallest number of reference steps from the dependent to a reference of `changed`. The order of the items is the distance, smallest first, then the reference, in ascending string order. If the request has `limit` and more dependents than `limit`, the result has the first `limit` items, and a declared bound stopped the operation (STS-ENV-3 rule 2).

**CHG-IMP-5.** An impact item states only that the closure of the dependent contains a changed object. It does not state that the dependent is false or stale (CHG-SEL-4). The store does not report an object outside the read context. An empty item list does not state that no dependent exists outside the read context.
