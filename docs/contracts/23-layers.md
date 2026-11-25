# Layers

State: draft. Prefix: `LYR`. Profile: `aavanam/layer/v1`.

## Scope

This document defines the layer profile `aavanam/layer/v1`. A store declares this profile separately from the base profile (LYR-PRF-1). A layer adds members to one base collection, or replaces members of it, for one scope, for example one tenant. A layered read context has the base snapshot and one layer. A read context with a different layer, or with no layer, does not have the content of that layer.

The application owns the scopes. It decides which scope a caller has and which layer applies to a scope. The store compares a scope as an exact string. A scope is not a credential, and a layer is not access control (constitution, Boundaries). A layer is not a metadata overlay (BND-OVL-1).

## Topics

| Topic | Subject |
|---|---|
| `PRF` | The layer profile and its tables |
| `DEF` | The layer object |
| `PRE` | Precedence and the members of a layered context |
| `REF` | References between a layer and its base |
| `OPN` | Opening a layered context |
| `ISO` | Isolation between layers |
| `LCK` | Locks in a layered context |
| `EXP` | Export and device stores |

## Clauses

### The layer profile and its tables

**LYR-PRF-1.** This document specifies the profile `aavanam/layer/v1`. A store declares it separately from the base profile. The clauses of this document, and each sentence of a different document that names a clause of this document, apply only to a store that declares the profile. A store that does not declare it stays a store of the base profile. For such a store, a descriptor of kind `layer` gives `unknown_kind` (KND-REG-3). The name `layer` in the field `kinds` of a request also gives `unknown_kind` (DSC-LST-1, LIF-CHG-1). A lock descriptor with the field `view` gives `unknown_field` (KND-SCH-2). An `open_context` request with the field `layer` gives `unknown_field` (CTX-OPN-5).

**LYR-PRF-2.** The profile registers the kind `layer`, and it adds the field `view` to the kinds `execution-lock` and `build-lock` (KND-REG-2). The two tables of this clause give the kind and the fields, in the form of the tables of document 02. The kind `layer` is in no target group. For a store that declares the profile, `layer` is a kind in the kind table for the field `kinds` of `list_items` and of `snapshot_changes` (DSC-LST-1, LIF-CHG-1). A descriptor of kind `layer` has the common fields of document 02, and its `descriptor_profile` is `aavanam/base/v1`.

The kind table of the profile:

| Kind | Purpose | Document |
|---|---|---|
| `layer` | The members that one scope adds to a base collection or replaces in it | 23 |

The field table of the profile:

| Kind | Field | Value type | Count | Targets |
|---|---|---|---|---|
| `layer` | `base` | `string` | `1` | - |
| `layer` | `scope` | `string` | `1` | - |
| `layer` | `units` | `ref` | `0..n` | `knowledge-unit` |
| `layer` | `schemas` | `ref` | `0..n` | `schema` |
| `layer` | `relation_sets` | `ref` | `0..n` | `relation-set` |
| `layer` | `overlay_sets` | `ref` | `0..n` | `overlay-set` |
| `layer` | `observations` | `ref` | `0..n` | `observation` |
| `layer` | `native_mappings` | `ref` | `0..n` | `native-mapping` |
| `execution-lock` | `view` | `ref` | `0..n` | `snapshot`, `layer` |
| `build-lock` | `view` | `ref` | `0..n` | `snapshot`, `layer` |

**LYR-PRF-3.** The profile has the reason codes of the reason table of this clause, in the form of the reason table of document 03. STS-RSN-3 and STS-RSN-4 apply to them.

The reason table of the profile:

| Code | Status | Meaning |
|---|---|---|
| `layer_scope_mismatch` | `incompatible` | The scope of the layer of an `open_context` request is not the scope of the request (LYR-OPN-2) |
| `layer_base_mismatch` | `incompatible` | The root of an `open_context` request with a layer is not a snapshot of the base collection of the layer (LYR-OPN-2) |

### The layer object

**LYR-DEF-1.** A layer is an object of kind `layer`. Its field `base` is the name of its base collection. This name is the `collection` value of the snapshots of that collection (PUB-SNP-4). Its field `scope` is the name of its scope. A layer has the member fields of a snapshot: `units`, `schemas`, `relation_sets`, `overlay_sets`, `observations` and `native_mappings`. A member of a layer is a reference in one of these fields. An object in the closure of a member is a part of that member, not a member (PUB-ACT-2).

**LYR-DEF-2.** A scope is the name that the application gives to the readers of a layer. The store compares `base` and `scope` as exact strings. It does not change case or Unicode normalization. It applies no hierarchy, no pattern and no default to a scope. The store does not decide which scope a caller has.

**LYR-DEF-3.** The application writes a layer with `put`. The store checks it in the steps of STS-MAL-3, as each other descriptor. Thus a member that is not in the store gives `missing_target`, and a member of a different kind gives `unsupported_target_kind`. A layer does not change after the store writes it. A different member list gives a different revision (IDN-CAN-2). A layer is not a snapshot. The catalog has no head for a layer (PUB-HED-1), and `create_candidate` and `publish` do not accept it. The application keeps the reference of the layer that applies to a scope.

**LYR-DEF-4.** In one layer, one native identity MUST have one subject, as in one snapshot (IDN-NAT-3). Two `native_mappings` of a layer can have the same `native_scheme` and `native_id` and different subjects. The store MUST then reject the layer with status `failed` and reason `native_id_conflict` (STS-MAL-3 step 6). The store does not compare the native mappings of a layer with the native mappings of a base snapshot.

**LYR-DEF-5.** The `base` of a layer can have the section `serving`. The closure of the layer (KND-DEP-3) MUST then have no descriptor of kind `source`, `reading`, `excerpt` or `evidence`. The check and its result are those of PUB-SEC-3. The item has status `failed`, with one reason `section_violation` for each such descriptor. The store MUST refuse a first section declaration for a collection that is the `base` of a layer in the store. PUB-SEC-2 gives the same rule for a collection that has a snapshot. Thus a layered context whose root is a snapshot of a `serving` collection has no member that is Evidence material (CRD-SRV-1).

### Precedence and the members of a layered context

**LYR-PRE-1.** These terms apply to a layer and a snapshot of its base collection, the base snapshot. A base member is a member of the base snapshot (PUB-ACT-2).

- A layer member replaces a base member if the two have the same kind, the same `logical_id` and different references.
- A replaced member is a base member that is not a layer member and that one or more layer members replace.
- A kept member is a base member that is not a replaced member.
- An added member is a layer member that replaces no base member.

A member without a `logical_id` replaces no member, and no member replaces it. The store compares logical IDs as exact strings (IDN-LOG-1). The layer has precedence over the base snapshot, and no other precedence applies. A layer has no field that removes a base member. Thus each base member is a kept member or a replaced member.

**LYR-PRE-2.** A layered context is a read context that the store opens with a base snapshot and a layer (LYR-OPN-1). Its members (CTX-VIW-1) are these objects, and no other object:

1. The base snapshot and the layer.
2. Each kept member and each layer member.
3. Each object that a reference path (KND-DEP-1) from a kept member or a layer member reaches, if the path has no replaced member.

Thus a replaced member is not a member of the context, also if a kept record names it as its subject. An object that only a replaced member reaches is not a member. An object that a replaced member and a different member reach is a member.

**LYR-PRE-3.** The store does not select one revision among the layer members (IDN-LOG-3). Two layer members can have one kind and one `logical_id`. Each of them is then a member of the context. Each of them replaces each base member with that kind and that `logical_id` (DSC-LOC-2).

**LYR-PRE-4.** Some rules name the members of a read context, the objects in a read context or the overlays of a read context. In a layered context, each such rule uses the members of LYR-PRE-2. For example, the candidates of `list_items` (DSC-LST-2), of `search` (QRY-LEX-2) and of `search_text` (CRD-TXT-2) are members. An overlay gives a metadata value only if it is a member (BND-OVL-2). The candidates of `context_bundle` are the members in the closures of the roots, and a distance counts only a reference path of members (QRY-BDL-3). An item of `search_text` for a member that is not in the closure of the base snapshot has an empty `snapshots` list (CRD-TXT-6).

**LYR-PRE-5.** A layer does not change a closure (KND-DEP-3). Thus the closure of the base snapshot has each replaced member. `export`, `execution_inputs` and `build_inputs` are complete operations over the closure of their references (BUN-EXP-2, LCK-EXE-5, LCK-BLD-3). In a layered context, their items and the bundle of an `export` have each object of that closure. This applies also to an object that LYR-PRE-2 does not make a member. Such an object is in the closure of a member. It is never an object that only a different layer reaches (LYR-ISO-1).

### References between a layer and its base

**LYR-REF-1.** A member of a layer, and each object in its closure, can refer to each object in the store. KND-REF-2 and KND-REF-3 apply, as for each other descriptor. A reference from a layer to an object of the base is an exact revision (KND-REF-1). The store MUST NOT change it to a different revision of the same `logical_id`, for example when a later base snapshot has a different revision. The target stays in the closure of the layer. Thus it is a member of each layered context with that layer, unless it is a replaced member (LYR-PRE-2).

**LYR-REF-2.** An object in the closure of a base snapshot does not refer to an object that only a layer reaches. A record in a layer refers to its subject in the base, and the subject does not refer to the record (KND-REF-6). Thus a layer does not change the revision or the closure of a base object. A read context that does not have the layer does not have the record.

**LYR-REF-3.** The store keeps no owner for an object. Two descriptors with the same canonical form are one object (IDN-CAN-3). Thus one object can be in the closure of two layers with different scopes. It is then a member of a layered context with each of these layers.

### Opening a layered context

**LYR-OPN-1.** The request of `open_context` can have the field `layer`. Its value is an object with the fields `ref` and `scope`, and no other field. `ref` is a reference. `scope` is a string with one or more characters. A value of a different form gives `field_type` in step 2 of CTX-OPN-5. If the request has `layer`, the fields `roots` and `heads` together MUST have exactly one value. If not, the request gives `field_count` in step 3 of CTX-OPN-5.

**LYR-OPN-2.** If the request has `layer`, the result has two items. The first item is the item of the root or of the head (CTX-OPN-1, CTX-VIW-2). The second item is the item of the layer. The store checks the layer in these steps, in this order. It stops at the first check that fails, and the layer item has the reason of that check only. The `refs` of the reason name the `ref` of `layer`.

1. Reference: if `ref` is not in the store, the item has status `not_found` and reason `unknown_ref`.
2. Kind: if the object is not a `layer`, the item has status `unsupported` and reason `unsupported_target_kind`.
3. Scope: if the `scope` of the layer is not equal to the `scope` of the request, the item has status `incompatible` and reason `layer_scope_mismatch`.
4. Base: this step applies only if the first item is `ok`. The `ref` of the first item MUST be a snapshot whose `collection` is equal to the `base` of the layer. If not, the item has status `incompatible` and reason `layer_base_mismatch`. Thus a root of kind `knowledge-unit` gives this reason.

If no check fails, the layer item has status `ok` and the field `ref`, the `ref` of `layer`. STS-ENV-3 gives the envelope status.

**LYR-OPN-3.** If each item is `ok`, the store opens a layered context with the members of LYR-PRE-2. CTX-OPN-2 and CTX-OPN-3 apply to its identity and to its content. Thus a later `put` of a different layer with the same scope does not change the context. The capabilities of a layered context are those of CTX-PRJ-2.

**LYR-OPN-4.** The `scope` of the request is the scope that the application reads for. The check of LYR-OPN-2 step 3 makes sure that the layer of the request is a layer of that scope. It does not prove that the caller has the scope (LYR-DEF-2).

### Isolation between layers

**LYR-ISO-1.** An operation in a read context reads only the members of the context (CTX-OPN-4, BUN-CLS-1). An object that only a layer reaches is not a member of a read context that does not have that layer. An operation in such a context MUST NOT return that object, a value from it or a match in it. It MUST NOT give an item for the object. It MUST NOT give a reason that names the object, unless the request names it. This applies to a layered context with a different layer, and to a read context with no layer.

**LYR-ISO-2.** In a layered context, a reference of a request that is not a member gives status `not_found` with reason `unknown_ref`. This applies also if the store has the object. The store MUST NOT give `not_in_context` in a layered context. Thus the reason does not show that an object of a different scope is in the store. A read context with no layer keeps the reasons of CTX-OPN-4. An application that reads for a scope with no content can use a layer with no members.

**LYR-ISO-3.** A record applies in a read context only if it is a member (LYR-PRE-4). An `overlay`, an `observation` or an `evidence` record can be a record that only a layer reaches. In a read context that does not have that layer, the record gives no value and no item. This applies also if its subject is a member (BND-OVL-2, OBS-SUB-2, REL-EVD-2).

**LYR-ISO-4.** The base profile has no operation that lists the layers of a base collection or the scopes of a store. In a layered context, the only member of kind `layer` is the layer of the context.

### Locks in a layered context

**LYR-LCK-1.** Under this profile, the kinds `execution-lock` and `build-lock` have the field `view`, a set of references to objects of kind `snapshot` or `layer` (LYR-PRF-2). In a layered context, `execution_inputs` and `build_inputs` write a lock with `view` (LCK-EXE-4, LCK-BLD-3). Its values are the base snapshot and the layer of the context, and no other value. In a read context with no layer, the lock has no `view`. The request has no field for `view`: the store takes the values from the read context.

**LYR-LCK-2.** `view` records the view that the consumer read through. It is not an input of the consumer. The items of the result stay the closure of LCK-EXE-4 or LCK-BLD-3. The covered set of the lock does not have the values of `view` (PUB-ACT-2). Two requests with equal fields give different locks if the base snapshots or the layers of their contexts are different.

### Export and device stores

**LYR-EXP-1.** In a layered context, the base snapshot and the layer are members, so each can be a root of an `export` (BUN-EXP-1). The bundle has the closure of its roots (BUN-EXP-2, LYR-PRE-5). Thus a bundle with the roots base snapshot and layer has the base and that layer. It MUST NOT have a descriptor or bytes that only a different layer reaches. A root that is a different layer is not a member. The result then has status `not_found` (LYR-ISO-2), and it has no `bundle`.

**LYR-EXP-2.** In a layered context, a candidate of a filtered export MUST be a member of the context. Thus the candidates are the members of kind `knowledge-unit` in the closure of the request roots (BUN-FLT-1). A replaced unit is not a candidate, so a filtered listing and a filtered export give the same content (BUN-FLT-5). Rule 3 of BUN-FLT-2 applies to each object of kind `knowledge-unit` in the closure of the candidates that match. This includes a replaced unit. Thus the units in the bundle are exactly its roots (BUN-FLT-3).

**LYR-EXP-3.** A device store has an `ok` import of a bundle whose roots are a base snapshot and a layer. A layered context on the device store has that base snapshot as its root and that layer. It has the same members as the layered context on the writer store (LYR-PRE-2). The rules of BUN-CLS-3 and BUN-CLS-4 then apply to the two contexts. A lock that the device context writes has the same `view` and the same revision as the lock of the writer context. A device store with only bundles of one scope does not have an object that only a layer of a different scope reaches.
