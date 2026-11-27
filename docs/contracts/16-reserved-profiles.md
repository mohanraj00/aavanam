# Reserved profiles

State: draft. Prefix: `RSV`. Profile: `aavanam/base/v1`.

## Scope

This document lists features that the specification names but does not yet define. Each one has a defined unsupported response and reason code.

## Topics

| Topic | Subject |
|---|---|
| `LST` | Reserved profile list |
| `RSP` | Unsupported responses |

## Clauses

### Reserved profile list

**RSV-LST-1.** Each row of the reserved profile table is one reserved feature. The row gives the profile that will specify the feature and its reserved operations. A reserved profile is not part of `aavanam/base/v1`. A store that conforms to `aavanam/base/v1` MUST NOT support a reserved operation.

**RSV-LST-2.** Each reserved operation has a row in the operation table of document 03, with document 16 and only the status `unsupported`. Each operation with such a row is in the reserved profile table one time.

**RSV-LST-3.** The names in the reserved profile table are reserved. An extension MUST NOT add an operation or a profile with one of these names. Only a profile document that specifies the feature can use them.

**RSV-LST-4.** Document 24 specifies the profile `aavanam/semantic-index/v1`. The profile is not part of `aavanam/base/v1`, so its row stays in the reserved profile table. A store that does not declare the profile gives the response of RSV-RSP-1 for `semantic_search`. For a store that declares the profile, RSV-LST-1 and RSV-RSP-1 do not apply to `semantic_search`. That store uses the operation row of document 24 (IDX-PRF-4).

**RSV-LST-5.** Earlier minor versions of the base profile reserved the operation `gc_collect` for the profile `aavanam/gc/v1`. From minor version 13, the base profile specifies the operation (LIF-GC-1), the removal from the Evidence section (REL-RMV-1) and the life of a read context (CTX-LIF-1). Thus the operation has no row in the reserved profile table, and RSV-RSP-1 does not apply to it. The name `aavanam/gc/v1` stays reserved (RSV-LST-3). No profile and no extension uses it.

The reserved profile table:

| Profile | Feature | Operations |
|---|---|---|
| `aavanam/workspace/v1` | A writable workspace that a consumer opens, appends to and reads before a snapshot | `workspace_open`, `workspace_append`, `workspace_read` |
| `aavanam/lease/v1` | Leases: a hold that keeps a root and its closure from `gc_collect` (LIF-GC-1) for a declared period | `lease_acquire`, `lease_release` |
| `aavanam/cross-catalog/v1` | Cross-catalog contexts: a read context with roots from more than one catalog | `open_cross_catalog_context` |
| `aavanam/query-build/v1` | Query-stable builds: a build lock whose inputs are the result of a declared data query | `query_build_inputs` |
| `aavanam/semantic-index/v1` | Semantic indexes: search through a semantic index adapter (topic `SEM` of document 09) | `semantic_search` |
| `aavanam/signed-activation/v1` | Signed activations: a check of the signatures of an activation against a registered key set for each signed collection, before a head update | `declare_signed_collection`, `rotate_key_set`, `publish_signed`, `rotate_root`, `mark_compromise` |

### Unsupported responses

**RSV-RSP-1.** A call to a reserved operation MUST return status `unsupported` with one reason, `reserved_profile` (STS-MAL-4). The `capabilities` of the reason MUST be a list with only the profile of the operation in the reserved profile table. Its `refs` MUST be an empty list. The store does not read the input, and the store MUST NOT change.

**RSV-RSP-2.** A consumer MUST treat `reserved_profile` as not success (STS-SET-2). The consumer MUST NOT replace a reserved operation with a base operation and use the result as the result of the feature. For example, a lexical `search` is not a semantic search, and a context of one catalog is not a cross-catalog context.
