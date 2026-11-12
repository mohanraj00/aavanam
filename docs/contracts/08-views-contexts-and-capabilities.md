# Views, contexts and capabilities

State: draft. Prefix: `CTX`. Profile: `aavanam/base/v1`.

## Scope

This document defines views, read contexts, projected contexts with functional capabilities, composition of independently versioned collections and binding skew. A capability states functional availability. It is not access control.

## Topics

| Topic | Subject |
|---|---|
| `VIW` | Views and membership |
| `OPN` | Opening a read context |
| `PRJ` | Projected contexts and capabilities |
| `CMP` | Composition of collections |
| `SKW` | Skew detection and resolution policy |

## Clauses

### Views and membership

**CTX-VIW-1.** A view is the content of one read context. An object is a member of the view if it is in the closure of a root (CTX-OPN-2). Membership does not depend on how the request names the root, by reference or by head.

**CTX-VIW-2.** The request of `open_context` can have the field `heads`, a set of collection names (KND-SCH-5). For each name, the store reads the head of that collection in the catalog (PUB-HED-1). If the catalog has a head, the head snapshot is a root of the context. If the catalog has no head for the name, the item of the name has status `unresolved` with reason `no_head`.

**CTX-VIW-3.** The store reads all heads of the request in one atomic step, when it opens the context. A later `publish` does not change the context (CTX-OPN-2). To read a new head, the application opens a new context.

### Opening a read context

**CTX-OPN-1.** The request of `open_context` can have the field `roots`, a list of references, the field `heads` (CTX-VIW-2), the field `skew_policy` (CTX-SKW-3) and the field `capabilities` (CTX-PRJ-2). Each root MUST have kind `knowledge-unit` or `snapshot`. The result has one item for each root, in the order of `roots`, and then one item for each name of `heads`, in the order of the request. An `ok` item has the field `ref`, the root or the head snapshot. If a root is not in the store, its item has status `not_found` and reason `unknown_ref`. If a root has a different kind, its item has status `unsupported` and reason `unsupported_target_kind`. STS-ENV-3 gives the envelope status.

**CTX-OPN-5.** The store checks the request form of `open_context` before its roots and heads. It applies these checks in order, and stops at the first check that fails. The result has status `failed` with one reason for each affected value, and no items.

1. Each field that is not `roots`, `heads`, `skew_policy` or `capabilities` gives the reason `unknown_field`.
2. If `roots` is not a list, or `heads` is not a list, the field gives `field_type`. If `skew_policy` is not one of the values of CTX-SKW-3, the field gives `field_type`. If `capabilities` is not a list, the field gives `field_type`. Each value of `capabilities` that is not a string gives `field_type`. Each value of `roots` that is not a reference, and each value of `heads` that is not a string, gives `field_type`.
3. If the two fields together have no value, the request gives `field_count`. Each name that occurs again in `heads` or in `capabilities` gives `field_count`.

**CTX-OPN-2.** If `open_context` is `ok`, the read context contains the closure of each root (KND-DEP-3), and no other object. The envelope field `context` is the identity of the read context, a string. The content of a read context MUST NOT change after the store opens it. A later `put` does not add an object to it. If `open_context` is not `ok`, `context` is null.

**CTX-OPN-3.** An operation that reads in a context has the request field `context`, with the identity from `open_context`. Its envelope field `context` MUST be that identity.

**CTX-OPN-4.** If an object is in the store but not in the read context, an operation in that context MUST NOT return the object or a value from it. Where the operation gives the object an outcome, the outcome is status `not_found` with reason `not_in_context`.

### Projected contexts and capabilities

**CTX-PRJ-1.** A capability is the name of a function that a read context can make available. The capability table gives each capability of the base profile and the operations that need it. An operation that is not in the table does not need a capability, for example `get`.

| Capability | Operations |
|---|---|
| `aavanam/data/v1` | `query_data` |
| `aavanam/lexical/v1` | `search` |
| `aavanam/locks/v1` | `execution_inputs`, `build_inputs` |
| `aavanam/export/v1` | `export` |
| `aavanam/text/v1` | `search_text` |

**CTX-PRJ-2.** The request of `open_context` can have the field `capabilities`, a set of capability names (KND-SCH-5). If the request has no `capabilities`, the context has each capability of the table. If the request has `capabilities`, the context is a projected context. It has only the capabilities of the request. An empty list gives a projected context with no capability.

**CTX-PRJ-3.** After the checks of CTX-OPN-5, and before the roots and the heads, the store checks each name of `capabilities`. If a name is not in the capability table, the result has status `unsupported` with one reason `unsupported_profile` for each such name. The `capabilities` of the reason name it. The result has no items, and `context` is null.

**CTX-PRJ-4.** A projection does not change the members of the context (CTX-VIW-1). An operation in a projected context that needs a capability that the context does not have gives status `unsupported` with one reason `capability_unavailable`. The `capabilities` of the reason name the capability. The store checks this after the request form (STS-MAL-1) and the context (CTX-OPN-3), and before the other checks of the request. The result has no items, no lock and no bundle (STS-ENV-4).

**CTX-PRJ-5.** A capability states functional availability. It is not access control or authentication. A projection does not hide an object: an operation that needs no capability reads each member of the context. An application MUST NOT use a projection to protect data.

### Composition of collections

**CTX-CMP-1.** A read context composes collections if it has two or more snapshot roots. Each snapshot gives its collection, the value of its field `collection`. A read context has at most one snapshot of each collection. If two different snapshot roots have the same collection, the item of each of these roots has status `ambiguous` with reason `collection_conflict`. If one snapshot is a root two times, for example by reference and by head, it is one root.

**CTX-CMP-2.** An object in the closure of more than one root is one member of the context. Its identity is its revision (IDN-CAN-2). The store does not copy it.

**CTX-CMP-3.** Composition does not select a revision for a logical ID (IDN-LOG-3). Two collections can contain different revisions of one logical ID. Topic `SKW` gives the outcome when the bindings of two collections disagree.

### Skew detection and resolution policy

**CTX-SKW-1.** A pin of a snapshot root is an object with a `logical_id` that a BindingSet in the closure of the root binds. The BindingSet binds it as a value of `bindings` or as a value of `runtime_profiles`. The pins of a root for a logical ID are the revisions of its pins with that logical ID. An object with no `logical_id` is not a pin. A role name does not make two objects one input.

**CTX-SKW-2.** A logical ID has binding skew in a read context that composes collections (CTX-CMP-1) if two or more snapshot roots have pins for it, and these roots do not all have the same pins for it. Thus two collections that pin one revision do not have skew. The pins of one root alone do not give skew. A root that is not a snapshot does not take part.

**CTX-SKW-3.** The field `skew_policy` of the request declares the outcome of binding skew. It is a value from a closed set: `reject` or `permit`. If the request has no `skew_policy`, the policy is `reject`. The policy is a value, not a rule. The store does not evaluate application rules to apply it.

**CTX-SKW-4.** The store checks skew when it opens the context, only if each item is `ok` after CTX-OPN-1, CTX-VIW-2 and CTX-CMP-1. Under `reject`, the item of each snapshot root with pins for a logical ID with skew has status `incompatible`. The item has one reason `binding_skew` for each such logical ID. The `refs` of the reason name the pins of the root for that logical ID. If one snapshot is a root two times (CTX-CMP-1), each of its items has these reasons. STS-ENV-3 gives the envelope status, and `context` is null (CTX-OPN-2).

**CTX-SKW-5.** Under `permit`, skew does not change an item. The context opens only if each item is `ok` (CTX-OPN-2). Thus `permit` does not change an outcome of CTX-OPN-1, CTX-VIW-2 or CTX-CMP-1. Each pin of each root is a member (CTX-CMP-2). The store does not select one revision for the logical ID (CTX-CMP-3, IDN-LOG-3). A consumer reads each revision by its reference.
