# Kinds and typed references

State: draft. Prefix: `KND`. Profile: `aavanam/base/v1`.

## Scope

This document defines the kind registry. Each kind has a schema. Each reference field names its permitted target kinds. The dependency set of an object comes only from its reference fields.

This document does not define the meaning of a kind. Other documents do that, for example document 05 for BindingSets and document 06 for evidence. Document 03 defines the status values and reason codes that this document uses.

## Topics

| Topic | Subject |
|---|---|
| `REG` | Kind registry and kind identity |
| `SCH` | Schema for each kind and strict normative fields |
| `REF` | Typed reference fields and permitted target kinds |
| `DEP` | Derived dependency sets |
| `OPQ` | Opaque application payloads and extension data |

## Clauses

### Registry

**KND-REG-1.** Each descriptor MUST have a `kind` field. Its value MUST be a kind in the kind table, or an extension kind of an extension profile that the store supports.

**KND-REG-2.** The profile `aavanam/base/v1` registers the kinds in the kind table, and no other kinds.

**KND-REG-3.** If a descriptor has a kind that is not registered, the store MUST reject the write with status `unsupported` and reason `unknown_kind`. The store does not keep the descriptor.

**KND-REG-4.** An extension kind has the form `ext:<owner>/<name>/v<N>#<kind>`. Its profile MUST give a schema in the form of the field table. An extension MUST NOT add, remove or change a field of a base kind.

**KND-REG-5.** A later minor version of the base profile can add a kind. It MUST NOT remove a kind or change the schema of a kind. A new kind MUST NOT join a target group, because each field that names the group then gets a new target (KND-REF-4). Minor version 1 adds the publication kind `activation` (PUB-ACT-1). It is in no target group. A candidate and a head stay catalog records, not descriptors (PUB-CND-4, PUB-HED-1).

The kind table:

| Kind | Purpose | Document |
|---|---|---|
| `source` | Exact source bytes, for example a document | 01 |
| `reading` | A producer reading of a source, with its producer and coverage profile | 15 |
| `excerpt` | A part of a reading, with its parent ranges | 11 |
| `schema` | A schema document and its schema profile | 04 |
| `data-resource` | A typed table or record set under a data profile | 04 |
| `artifact` | An opaque application artifact, for example a rule source, a narrative or a prompt | 05 |
| `runtime-profile` | A declaration of a runtime or a callable profile | 05 |
| `binding-set` | Exact bindings of roles to resources, with parameters and runtime profiles | 05 |
| `derivation` | A declared derivation from exact operands | 06 |
| `evidence` | A link from a subject to the material that supports it | 06 |
| `receipt` | A receipt that an external producer made | 15 |
| `check` | The outcome of one check on one subject, in a stated scope | 06 |
| `retraction` | A withdrawal of one evidence or check record | 06 |
| `supersession` | A replacement of one evidence or check record with another | 06 |
| `relation-set` | A set of derivation, evidence, check and relation records | 05 |
| `overlay` | A metadata record about a subject, with its origin | 05 |
| `overlay-set` | A set of overlays | 05 |
| `knowledge-unit` | A BindingSet with its narratives, RelationSets and OverlaySets | 05 |
| `snapshot` | The units, schemas, sets, observations and native mappings of one collection at one revision | 08 |
| `observation` | An append-only fact about subjects, in a stated scope | 07 |
| `native-mapping` | A checked mapping from a producer identity to a store descriptor | 01 |
| `execution-lock` | The inputs of one execution: a BindingSet closure and the declared context inputs | 10 |
| `build-lock` | The inputs that one build consumed | 10 |
| `activation` | The record of an application decision that qualifies one candidate before its publication | 13 |

### Schemas

**KND-SCH-1.** Each descriptor MUST have the common fields that have a count of `1`. The schema of a kind is the common fields plus the rows for that kind in the field table.

**KND-SCH-2.** If a descriptor has a top-level field that is not in its schema, the store MUST reject the write with status `failed` and reason `unknown_field`.

**KND-SCH-3.** If a field has fewer or more values than its count permits, the store MUST reject the write with status `failed` and reason `field_count`. A count of `0..1`, `0..n` or `0..n list` permits an absent field. For a field with a count of `0..n` or `0..n list`, an empty list and an absent field are the same value, and the canonical form omits both. This rule applies only to the field itself. A `json` value is one value of any I-JSON type. Thus `[]`, `{}` and `null` are valid values of a `json` field with a count of `1`, and the canonical form keeps them.

**KND-SCH-4.** If a value does not agree with its value type, the store MUST reject the write with status `failed` and reason `field_type`.

**KND-SCH-5.** A field with a count of `0..n` or `1..n` is a set, unless its count says `list`. The canonical form sorts a set of references by digest. A set MUST NOT contain duplicates. A set with a duplicate has more values than its count permits, so it gives `field_count` (KND-SCH-3). This applies also to a set in a request. A list keeps its order.

**KND-SCH-6.** A `ref-map` field has one JSON object as its value, never a list of objects. Its count is the number of entries in that object. The keys are unique (JSON form, STS-MAL-3). The canonical form sorts the entries by key (document 01). Two keys can have the same `ref` value.

**KND-SCH-7.** If a `derivation` has `operand_selectors`, the list MUST have the same length as `operands`. The selector at position N applies to the operand at position N. A `null` value at a position selects the whole operand. The store applies KND-SCH-3 before this rule. Thus an empty `operand_selectors` list is an absent field, and the length rule does not apply to it. If the lengths are different, the store MUST reject the write with status `failed` and reason `field_count`.

The common fields:

| Field | Value type | Count | Note |
|---|---|---|---|
| `descriptor_profile` | `string` | `1` | For example `aavanam/base/v1` |
| `kind` | `string` | `1` | KND-REG-1 |
| `logical_id` | `string` | `0..1` | Document 01 |
| `ext` | `json` | `0..1` | An object with one key for each extension namespace (KND-OPQ-3) |

The value types:

| Value type | Value |
|---|---|
| `ref` | One descriptor revision: `sha256:` and 64 lowercase hexadecimal digits |
| `ref-map` | A JSON object. Each key is a role name. Each value is a `ref` |
| `payload` | An object with `digest`, `media_type` and `size`, for exact bytes |
| `string` | A JSON string |
| `json` | An I-JSON value. The store does not read references from it |
| `selector` | A selector in a form that document 06 defines. In `operand_selectors` only, `null` selects the whole object (KND-SCH-7) |

The target groups:

| Group | Kinds |
|---|---|
| `content` | `source`, `reading`, `excerpt`, `schema`, `data-resource`, `artifact`, `runtime-profile`, `binding-set`, `knowledge-unit` |
| `record` | `derivation`, `evidence`, `receipt`, `check`, `retraction`, `supersession`, `overlay`, `observation` |

The field table. The targets column applies only to the value types `ref` and `ref-map`.

| Kind | Field | Value type | Count | Targets |
|---|---|---|---|---|
| `source` | `payload` | `payload` | `1` | - |
| `reading` | `source` | `ref` | `1` | `source` |
| `reading` | `producer` | `string` | `1` | - |
| `reading` | `producer_config` | `payload` | `0..1` | - |
| `reading` | `coverage_profile` | `string` | `1` | - |
| `reading` | `payload` | `payload` | `1` | - |
| `excerpt` | `parent` | `ref` | `1` | `reading` |
| `excerpt` | `ranges` | `json` | `1` | - |
| `excerpt` | `payload` | `payload` | `1` | - |
| `schema` | `schema_profile` | `string` | `1` | - |
| `schema` | `annotations` | `ref` | `0..1` | `artifact` |
| `schema` | `payload` | `payload` | `1` | - |
| `data-resource` | `data_profile` | `string` | `1` | - |
| `data-resource` | `schema` | `ref` | `1` | `schema` |
| `data-resource` | `payload` | `payload` | `1` | - |
| `artifact` | `artifact_type` | `string` | `1` | - |
| `artifact` | `schema` | `ref` | `0..1` | `schema` |
| `artifact` | `declared_roles` | `ref-map` | `0..n` | `schema` |
| `artifact` | `runtime_profile` | `ref` | `0..1` | `runtime-profile` |
| `artifact` | `payload` | `payload` | `1` | - |
| `runtime-profile` | `profile_id` | `string` | `1` | - |
| `runtime-profile` | `payload` | `payload` | `0..1` | - |
| `binding-set` | `bindings` | `ref-map` | `0..n` | `artifact`, `data-resource`, `schema` |
| `binding-set` | `runtime_profiles` | `ref` | `0..n` | `runtime-profile` |
| `binding-set` | `parameters` | `json` | `0..1` | - |
| `derivation` | `operands` | `ref` | `1..n list` | `data-resource`, `reading`, `excerpt` |
| `derivation` | `operand_selectors` | `selector` | `0..n list` | - |
| `derivation` | `method` | `string` | `1` | - |
| `derivation` | `result` | `json` | `1` | - |
| `evidence` | `subject` | `ref` | `1` | `artifact`, `data-resource`, `binding-set` |
| `evidence` | `subject_selector` | `selector` | `0..1` | - |
| `evidence` | `supports` | `ref` | `1` | `reading`, `excerpt`, `derivation` |
| `evidence` | `support_selector` | `selector` | `0..1` | - |
| `evidence` | `origin` | `string` | `1` | - |
| `receipt` | `producer` | `string` | `1` | - |
| `receipt` | `subjects` | `ref` | `0..n` | `content` |
| `receipt` | `payload` | `payload` | `1` | - |
| `check` | `subject` | `ref` | `1` | `content`, `evidence`, `relation-set` |
| `check` | `check_profile` | `string` | `1` | - |
| `check` | `outcome` | `string` | `1` | - |
| `check` | `scope` | `json` | `1` | - |
| `check` | `receipts` | `ref` | `0..n` | `receipt` |
| `check` | `origin` | `string` | `1` | - |
| `retraction` | `target` | `ref` | `1` | `evidence`, `check` |
| `retraction` | `reason` | `string` | `1` | - |
| `retraction` | `origin` | `string` | `1` | - |
| `supersession` | `target` | `ref` | `1` | `evidence`, `check` |
| `supersession` | `replacement` | `ref` | `1` | `evidence`, `check` |
| `supersession` | `reason` | `string` | `1` | - |
| `supersession` | `origin` | `string` | `1` | - |
| `relation-set` | `members` | `ref` | `0..n` | `derivation`, `evidence`, `check`, `retraction`, `supersession` |
| `overlay` | `subject` | `ref` | `1` | `content` |
| `overlay` | `origin` | `string` | `1` | - |
| `overlay` | `precedence` | `string` | `0..1` | - |
| `overlay` | `values` | `json` | `1` | - |
| `overlay-set` | `members` | `ref` | `0..n` | `overlay` |
| `knowledge-unit` | `binding_set` | `ref` | `1` | `binding-set` |
| `knowledge-unit` | `narratives` | `ref` | `0..n` | `artifact` |
| `knowledge-unit` | `relation_sets` | `ref` | `0..n` | `relation-set` |
| `knowledge-unit` | `overlay_sets` | `ref` | `0..n` | `overlay-set` |
| `snapshot` | `collection` | `string` | `1` | - |
| `snapshot` | `units` | `ref` | `0..n` | `knowledge-unit` |
| `snapshot` | `schemas` | `ref` | `0..n` | `schema` |
| `snapshot` | `relation_sets` | `ref` | `0..n` | `relation-set` |
| `snapshot` | `overlay_sets` | `ref` | `0..n` | `overlay-set` |
| `snapshot` | `observations` | `ref` | `0..n` | `observation` |
| `snapshot` | `native_mappings` | `ref` | `0..n` | `native-mapping` |
| `observation` | `subjects` | `ref` | `1..n` | `content`, `record` |
| `observation` | `observation_type` | `string` | `1` | - |
| `observation` | `scope` | `json` | `1` | - |
| `observation` | `values` | `json` | `1` | - |
| `native-mapping` | `subject` | `ref` | `1` | `content`, `record` |
| `native-mapping` | `native_scheme` | `string` | `1` | - |
| `native-mapping` | `native_id` | `string` | `1` | - |
| `native-mapping` | `checked` | `json` | `1` | - |
| `execution-lock` | `binding_set` | `ref` | `1` | `binding-set` |
| `execution-lock` | `context_inputs` | `ref` | `0..n` | `content`, `record`, `relation-set`, `overlay-set` |
| `execution-lock` | `requires` | `string` | `0..n` | - |
| `execution-lock` | `consumer` | `string` | `1` | - |
| `execution-lock` | `completeness` | `string` | `1` | - |
| `build-lock` | `inputs` | `ref` | `1..n` | `content`, `record`, `relation-set`, `overlay-set`, `snapshot` |
| `build-lock` | `requires` | `string` | `0..n` | - |
| `build-lock` | `consumer` | `string` | `1` | - |
| `build-lock` | `completeness` | `string` | `1` | - |
| `activation` | `candidate` | `string` | `1` | - |
| `activation` | `base` | `ref` | `0..1` | `snapshot` |
| `activation` | `lock` | `ref` | `1` | `execution-lock`, `build-lock` |
| `activation` | `approval` | `ref` | `1` | `receipt` |
| `activation` | `report` | `ref` | `1` | `receipt` |
| `activation` | `partition` | `string` | `1` | - |
| `activation` | `effective_from` | `string` | `1` | - |
| `activation` | `effective_until` | `string` | `0..1` | - |
| `activation` | `request_key` | `string` | `1` | - |
| `activation` | `producer` | `string` | `1` | - |
| `activation` | `runner` | `string` | `1` | - |
| `activation` | `approver` | `string` | `1` | - |
| `activation` | `removed` | `ref` | `0..n` | `knowledge-unit`, `schema`, `relation-set`, `overlay-set`, `observation`, `native-mapping` |

### References

**KND-REF-1.** A reference is the descriptor revision of one object (document 01). A logical ID, a tag or a head is not a reference.

**KND-REF-2.** The target of a reference MUST be in the store when the store writes the descriptor. In a local output store, the target can be in the local output store or in its device store (BUN-IMP-8). If it is not, the store MUST reject the write with status `not_found` and reason `missing_target`.

**KND-REF-3.** The kind of each target MUST be in the targets of its field. A group in the targets column means all kinds in the group. If a target has a different kind, the store MUST reject the write with status `unsupported` and reason `unsupported_target_kind`. The store does not keep the descriptor.

**KND-REF-4.** The targets of a base field are closed for a profile version. An extension MUST NOT add a target to a base field. If an extension needs a different target set, it defines an extension kind. For withdrawal records, REL-RET-5 also applies.

**KND-REF-5.** Only `ref` and `ref-map` values are references. A digest in a `json`, `string` or `selector` value is not a reference. It is not a dependency, and closure does not follow it.

**KND-REF-6.** A record refers to its subject. A subject never refers to a record about it. For example, evidence refers to an artifact. The artifact descriptor does not change when evidence is added.

### Dependencies

**KND-DEP-1.** The reference set of a descriptor, `refs`, is the set of all `ref` and `ref-map` values in its schema fields. The store MUST compute `refs` from the descriptor. The descriptor does not contain `refs`.

**KND-DEP-2.** A descriptor MUST NOT contain a second dependency list. A field named `refs` or `dependencies` is an unknown field (KND-SCH-2). Each dependency index that an implementation keeps MUST be equal to the `refs` of KND-DEP-1.

**KND-DEP-3.** The closure of an object is the object plus the closure of each object in its `refs`. The byte closure is the set of `payload` digests in the closure. Bundles, locks and comparisons MUST use this closure. They MUST NOT use a different list. In a local output store, the closure can have objects of the local output store and of its device store (BUN-IMP-8).

**KND-DEP-4.** The reference graph is acyclic. This follows from KND-REF-2 and from content addressing. If application relations form a cycle, they are records. They are not references between the related objects.

### Opaque payloads

**KND-OPQ-1.** The store MUST check that the bytes of each `payload` agree with its `digest` and `size`. The store MUST NOT parse a payload, unless the profile of its kind requires this (for example a data profile in document 04). This exception applies only to the kinds `schema`, `data-resource`, `reading` and `excerpt`.

**KND-OPQ-2.** The store MUST NOT parse, load, import, interpret, execute, evaluate, compile or render an `artifact`, `runtime-profile` or `receipt` payload. No profile can change this. The value of `artifact_type` is an application name. The store compares it only as a string.

**KND-OPQ-3.** The store MUST keep the `ext` value as opaque data in the canonical encoding. If the store does not support an extension namespace in `ext`, it keeps the value and does not apply its meaning.
