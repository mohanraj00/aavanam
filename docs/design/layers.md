# Design note: a layer for one scope on a base collection

Issue: #44. The normative text is [document 23](../contracts/23-layers.md). This note gives the decisions and their reasons. It is not normative.

## The name

The spec uses the word "overlay" for a metadata record (BND-OVL-1). Thus the new object has the name `layer`. A layer is not a metadata overlay.

## The model

| Subject | Decision | Reason |
|---|---|---|
| Collection | The base stays an ordinary collection with ordinary snapshots. A layer names its base collection in the string field `base` | One layer revision applies to each later snapshot of the base. The base does not know its layers |
| Profile | The layer is the profile `aavanam/layer/v1`, with its own kind, field and reason tables in document 23 | The base tables and the base minor version do not change. A store of the base profile that does not declare the profile stays conformant |
| Layer object | A new kind `layer`, written with `put`. It has `base`, `scope` and the six member fields of a snapshot | A descriptor moves in a bundle, so a device store can open the same view. Store configuration does not move |
| Layer revision | A layer is immutable. A change gives a new revision. The store keeps no head for a layer | The application owns the decision which layer applies to a scope |
| Closure | The closure of an object does not change (KND-DEP-3). The closure of the base snapshot keeps each replaced member | Typed references stay the only dependency source (constitution principle 1) |
| Read context | `open_context` gets the field `layer`, an object with `ref` and `scope`. A layered context has one root, the base snapshot | The caller gives the layer. The store compares the scope as an exact string and applies no policy |
| Lock | The lock kinds get the optional field `view`. In a layered context, the store writes the base snapshot and the layer into it | A lock records the view of the run. A lock of a context with no layer keeps its revision |
| Export | A bundle is the closure of its roots, as before. The roots of a bundle for one scope are the base snapshot and the layer of that scope | No new bundle rule is necessary. A different layer is not in these closures |

## Precedence

- Replace: a layer member replaces each base member of the same kind that has the same `logical_id` and a different revision.
- Add: each other layer member is an added member.
- No delete. A layer cannot remove a base member. Reason: a view with a delete depends on an absence. If a caller then gives no layer or a wrong layer, the deleted member comes back with no signal. A replacement is an object that a reader can see and cite.
- The members of a layered context are the base snapshot, the layer, the kept base members and the layer members. Each object that these members reach is also a member, if the path has no replaced member. Thus a replaced member is not in the view, also if a kept record names it as its subject.

For the example of the issue: the base has 200 units. The layer of T1 replaces one unit and adds two. The context of T1 has 202 units. The context of T2, with the layer of T2 or with no layer, has 200 units.

## Reference rules

- A layer member can refer to each object in the store by a typed reference, as each descriptor can (KND-REF-2, KND-REF-3). A reference to a base object pins that revision. The store does not bind it to a later revision.
- A base object does not refer to a layer object. A record in a layer refers to its subject in the base, and the subject does not change (KND-REF-6).
- A record in a layer applies only in a context that has that layer, for example a metadata overlay about a base table.
- The store keeps no owner for an object. Two layers can reach one object, because equal descriptors are one object (IDN-CAN-3).

## Isolation

Isolation is a property of the read context. The context of scope T2 has the base and the layer that the caller gave for T2, and no other object. The vectors test each path from a request to an object of a different layer:

| Path | Operation |
|---|---|
| Exact reference | `get`, `read`, `query_data`, `context_bundle` |
| Record about a base subject | `evidence`, `observations`, the `metadata` of `query_data`, the `filter` of `list_items` |
| Listing and search | `list_items`, `search`, `search_text` |
| Closure | The items of `execution_inputs`, `build_inputs` and `export` |
| Bundle | The roots and the descriptors of `export`, and a device store with the bundle of one scope |

In a layered context, a reference that is not a member gives `unknown_ref`, also if the store has the object. Thus a reader of one scope cannot use the reason code to learn that an object of a different scope is in the store.

## Limits

- The store does not authenticate a caller, and a scope is not a credential (constitution, Boundaries). The application decides which scope a caller has.
- A writer that knows the revision of an object can refer to it from a new layer. The application controls who writes.
- A layered context has one root. A context that composes a layered base with a second collection is not in this version.
- A layer has no head and no activation gate in this version.
