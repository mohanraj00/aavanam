# Bindings and units

State: draft. Prefix: `BND`. Profile: `aavanam/base/v1`.

## Scope

This document defines BindingSets, RelationSets, OverlaySets and knowledge units. It defines how an opaque application rule companion declares its roles and runtime profile without exposing its body to the store.

The store keeps rule companions, runtime profiles and parameters as opaque content. It checks only the references that they declare. It does not read, evaluate, compile or run a rule. The application owns the rule language, the evaluator and the emitter.

## Topics

| Topic | Subject |
|---|---|
| `SET` | BindingSet, RelationSet and OverlaySet identity |
| `UNT` | Knowledge units |
| `ROL` | Roles, ports and parameters |
| `RUL` | Opaque rule companions and runtime profiles |
| `OVL` | Metadata overlays and precedence |

## Clauses

### Set identity

**BND-SET-1.** A BindingSet is its descriptor. Its identity is its revision (IDN-CAN-2). The canonical form sorts the entries of `bindings` by role name (KND-SCH-6) and the set `runtime_profiles` by digest (KND-SCH-5). Two BindingSets are one object only if their canonical forms are equal (IDN-CAN-1). Thus a change of one binding, runtime profile, parameter, `logical_id` or `ext` value is a new BindingSet.

**BND-SET-2.** A RelationSet or an OverlaySet is the set of its `members`. The order of the members in a request has no effect on the revision (KND-SCH-5). The other fields, for example `logical_id` and `ext`, are part of the canonical form, as for each descriptor (IDN-CAN-1). An empty set is valid.

**BND-SET-3.** A set does not change after the store writes it. To add or remove a member, the application writes a new set. The earlier set stays in the store with its members.

### Knowledge units

**BND-UNT-1.** A knowledge unit has one BindingSet in `binding_set` (KND-SCH-3). It can also have narratives, RelationSets and OverlaySets. The closure of the unit (KND-DEP-3) contains the BindingSet, the bound objects, the narratives, the sets and the members of the sets. Thus a read context with the unit as a root contains each of them (CTX-OPN-2).

**BND-UNT-2.** An object that the closure of a unit does not contain is not in a read context that has only that unit as its root. This applies also to an `overlay` or an `evidence` record whose subject is in the closure, if no set of the unit has the record as a member (CTX-OPN-4).

### Roles, ports and parameters

**BND-ROL-1.** A role is a key of a `ref-map`. A port is a role that an `artifact` declares in `declared_roles`. The value of a declared role is the schema that the bound object must have. The store compares role names only as strings.

**BND-ROL-2.** For `execution_inputs`, the store applies this rule only if no outcome of LCK-EXE-1, LCK-EXE-2, LCK-CAP-2 or LCK-EXE-3 applies. For each declared role of each bound `artifact`, the binding with that role name is the bound object. The bound object agrees with the declared schema if one of these conditions is true:

1. The bound object is a `data-resource` or an `artifact`, and its `schema` is the declared schema.
2. The bound object is the declared schema.

If a bound object does not agree with its declared schema, the result has status `incompatible` and reason `role_schema_mismatch`. The result has a reason for each affected role. The store compares only references. It does not read a payload.

**BND-ROL-3.** `parameters` is opaque JSON (KND-OPQ-3). The store keeps the value in the canonical form and does not check it against a declared role. A parameter is not a binding. Thus a parameter key with the name of a declared role does not bind the role (LCK-EXE-3).

**BND-ROL-4.** A BindingSet can bind a role that no artifact declares, for example the role of the rule companion itself. The store does not reject this binding.

### Opaque rule companions and runtime profiles

**BND-RUL-1.** A rule companion is an `artifact`. Its payload is the rule body, in a language that the application owns. Its `artifact_type` names the application form. The store reads only its descriptor fields, for the schema, reference and identity checks, for example `declared_roles`, `runtime_profile` and `schema` (BND-ROL-2). KND-OPQ-2 applies to the payload: the store MUST NOT parse, load, evaluate, compile or run it.

**BND-RUL-2.** For `execution_inputs`, the store applies this rule only if no outcome of LCK-EXE-1, LCK-EXE-2, LCK-CAP-2, LCK-EXE-3 or BND-ROL-2 applies. If a bound `artifact` has a `runtime_profile` that is not in the `runtime_profiles` of the BindingSet, the result has status `incompatible` and reason `runtime_profile_mismatch`. The result has a reason for each affected artifact.

**BND-RUL-3.** A `runtime-profile` is a declaration of the application. Its `profile_id` names a runtime or a callable profile. The store compares it only as a string. A runtime profile in the store does not show that a runtime exists, or that a runtime supports a contract. The store MUST NOT state such a support.

**BND-RUL-4.** The store MUST NOT get a role, a runtime profile or a dependency from a payload. Only the descriptor fields declare them (KND-DEP-1). If a payload names a role that `declared_roles` does not declare, the store does not require a binding for that role.

### Metadata overlays and precedence

**BND-OVL-1.** An `overlay` refers to its subject, and the subject does not refer to the overlay (KND-DEP-4). Thus an overlay does not change the revision or the closure of its subject.

**BND-OVL-2.** An overlay applies in a read context only if the context contains it, for example through an OverlaySet of a unit (BND-UNT-2). The store keeps `values` as opaque JSON. Each top-level key of `values` is one metadata key.

**BND-OVL-3.** A read that returns overlay values can have the request field `precedence`, a list of strings, highest first. If the request has no `precedence`, the list is empty. For one subject and one metadata key, the candidates are the overlays in the read context that have that subject and key. The store applies these rules in order:

1. If all candidates give the same value, that value applies.
2. Each listed value is one tier. All other candidates, with no `precedence` or with a value that is not in the list, are in one lowest tier. The store keeps only the candidates of the highest tier that has a candidate.
3. If the kept candidates give the same value, that value applies. If not, the outcome is status `ambiguous` with reason `overlay_conflict`.

The store does not give an order to `precedence` values. Only the request list gives the order. `query_data` returns the applied values (QRY-DAT-3).
