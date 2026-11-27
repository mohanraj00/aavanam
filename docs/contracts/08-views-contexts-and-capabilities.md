# Views, contexts and capabilities

State: draft. Prefix: `CTX`. Profile: `aavanam/base/v1`.

## Scope

This document defines views, read contexts, the life of a read context, projected contexts with functional capabilities, composition of independently versioned collections and binding skew. A capability states functional availability. It is not access control.

## Topics

| Topic | Subject |
|---|---|
| `VIW` | Views and membership |
| `OPN` | Opening a read context |
| `LIF` | The life of a read context and its end |
| `PRJ` | Projected contexts and capabilities |
| `CMP` | Composition of collections |
| `SKW` | Skew detection and resolution policy |

## Clauses

### Views and membership

**CTX-VIW-1.** A view is the content of one read context. An object is a member of the view if it is in the closure of a root (CTX-OPN-2) and it is not a removed object (REL-RMV-8). Membership does not depend on how the request names the root, by reference or by head. LYR-PRE-2 gives the members of a layered context.

**CTX-VIW-2.** The request of `open_context` can have the field `heads`, a set of collection names (KND-SCH-5). For each name, the store reads the head of that collection in the catalog (PUB-HED-1). If the catalog has a head, the head snapshot is a root of the context. If the catalog has no head for the name, the item of the name has status `unresolved` with reason `no_head`.

**CTX-VIW-3.** The store reads all heads of the request in one atomic step, when it opens the context. A later `publish` does not change the context (CTX-OPN-2). To read a new head, the application opens a new context.

### Opening a read context

**CTX-OPN-1.** The request of `open_context` can have the field `roots`, a list of references, the field `heads` (CTX-VIW-2), the field `skew_policy` (CTX-SKW-3), the field `capabilities` (CTX-PRJ-2), the field `life` (CTX-LIF-1) and the field `layer` (LYR-OPN-1). Each root MUST have kind `knowledge-unit` or `snapshot`. A store that declares the profile of document 24 also permits a root of kind `retrieval-index` (IDX-DEF-3). The result has one item for each root, in the order of `roots`, and then one item for each name of `heads`, in the order of the request. An `ok` item has the field `ref`, the root or the head snapshot, and the field `removed` (REL-RMV-9). If a root is not in the store, its item has status `not_found` and reason `unknown_ref`. If a root has a different kind, its item has status `unsupported` and reason `unsupported_target_kind`. STS-ENV-3 gives the envelope status.

**CTX-OPN-5.** The store checks the request form of `open_context` before its roots and heads. It applies these checks in order, and stops at the first check that fails. The result has status `failed` with one reason for each affected value, and no items.

1. Each field that is not `roots`, `heads`, `skew_policy`, `capabilities` or `life` gives the reason `unknown_field`. A store that declares the layer profile also permits the field `layer` (LYR-OPN-1).
2. If `roots` is not a list, or `heads` is not a list, the field gives `field_type`. If `skew_policy` is not one of the values of CTX-SKW-3, the field gives `field_type`. If `capabilities` is not a list, the field gives `field_type`. Each value of `capabilities` that is not a string gives `field_type`. If `life` is not an integer of 1 or more, the field gives `field_type`. Each value of `roots` that is not a reference, and each value of `heads` that is not a string, gives `field_type`. A `layer` that does not have the form of LYR-OPN-1 gives `field_type`.
3. If the two fields together have no value, the request gives `field_count`. Each name that occurs again in `heads` or in `capabilities` gives `field_count`. If the request has `layer`, and the two fields together do not have exactly one value, the request gives `field_count` (LYR-OPN-1).

**CTX-OPN-2.** If `open_context` is `ok`, the read context contains the closure of each root (KND-DEP-3) without the removed objects (REL-RMV-8), and no other object. A layered context contains the members of LYR-PRE-2, and no other object. The envelope field `context` is the identity of the read context, a string. The content of a read context MUST NOT change after the store opens it, until the context ends (CTX-LIF-2). A later `put` does not add an object to it. If `open_context` is not `ok`, `context` is null.

**CTX-OPN-3.** An operation that reads in a context has the request field `context`, with the identity from `open_context`. Its envelope field `context` MUST be that identity.

**CTX-OPN-4.** If an object is in the store but not in the read context, an operation in that context MUST NOT return the object or a value from it. Where the operation gives the object an outcome, the outcome is status `not_found` with reason `not_in_context`. In a layered context, the reason is `unknown_ref` (LYR-ISO-2).

### The life of a read context

**CTX-LIF-1.** The request of `open_context` can have the field `life`, an integer of 1 or more. It is the declared life of the read context in seconds. The life starts at the time of the store clock when the store opens the context. A context with no `life` has no time limit.

**CTX-LIF-2.** A read context is open from the `ok` result of `open_context` until it ends. A read context ends at the first of these events, and at no other event of the base profile:

1. The store clock is at, or later than, the start of the life plus `life` seconds (CTX-LIF-1).
2. An `ok` `close_context` names the context (CTX-LIF-3).
3. An `ok` `remove` removes a member of the context (CTX-LIF-4).

A `put`, a `publish`, an `import` (BUN-UPD-3) and a `gc_collect` (LIF-GC-5) do not end a context. The store reads its clock when an operation names the context. An implementation MAY also end each read context when the application closes the store.

**CTX-LIF-3.** The request of `close_context` has the field `context`, the identity of a read context, and no other field. If the request has a different field, does not have `context`, or `context` is not a string, the result has status `failed` with reason `unknown_field`, `field_count` or `field_type`, in this order. If the context is open, the store ends it, and the result is `ok` with no items. If the context is not open, the result is the result of CTX-LIF-5. The envelope field `context` is the identity of the request (CTX-OPN-3). `close_context` changes no object, no bytes and no catalog record.

**CTX-LIF-4.** An `ok` `remove` ends each open read context that has the removed object as a member (REL-RMV-4). The members of a context do not change while it is open (CTX-OPN-2). Thus the store ends the context, and it does not take the object out of it.

**CTX-LIF-5.** After a read context ends, each operation that names its identity in the field `context` MUST give status `failed` with one reason, `context_expired`, and no items. An identity that the store did not give has the same result. The store checks this after the form of the request and before the other checks of the request. An ended context does not open again, and the store MUST NOT give its identity to a different context. To continue, the application opens a new context.

**CTX-LIF-6.** Each member of an open read context is a retained root of `gc_collect` (LIF-GC-2, BUN-RMV-1). Thus `gc_collect` does not remove an object or bytes that an open context reads, and each operation in an open context gives the same result before and after it. After a context ends, it retains no object.

### Projected contexts and capabilities

**CTX-PRJ-1.** A capability is the name of a function that a read context can make available. The capability table gives each capability of the base profile and the operations that need it. An operation that is not in the table does not need a capability, for example `get`. A profile document can add a capability for the stores that declare the profile (IDX-PRF-4).

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
