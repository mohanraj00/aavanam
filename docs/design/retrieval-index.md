# Design note: a retrieval index derived from a snapshot

Issue: #45. The normative text is [document 24](../contracts/24-retrieval-indexes.md). This note gives the decisions and their reasons. It is not normative.

## The model

| Subject | Decision | Reason |
|---|---|---|
| Profile | The index is the profile `aavanam/semantic-index/v1`, which document 16 reserved. It has its own kind, field, reason, operation and capability tables in document 24 | The base tables and the base minor version do not change. A store that does not declare the profile keeps the response `reserved_profile` |
| Index object | A new kind `retrieval-index`, written with `put`. It has `snapshot`, `method` and `parameters` | The descriptor is the full record of the index. It is immutable and citable, and it moves in a bundle |
| Projection | The store keeps no index content as an object. Derived data, for example a table of terms, is not an object, and a result does not depend on it | The index is never a source of truth. A rebuild from the same snapshot gives the same items |
| Method | The profile has one method, `aavanam/hashed-terms/v1`, with the parameter `dimensions` | The method is deterministic and needs no model account, so CI can run it. It uses integers only |
| External method | An index can name a different method, with opaque parameters. A search through it gives `unsupported_profile` | An embedding model is outside the profile. The store records the name and the parameters, and it does not run the method |
| Read context | The index MUST be a member of the read context. An index can be a root of `open_context` | One context gives each consumer the same exact inputs. The index is an input of the answer |
| Result | Each item has `ref`, `kind`, `logical_id`, `snapshot`, `index` and `score` | A result is a location that a consumer can cite. The consumer reads the value with an exact operation |
| Stale index | An index is stale in a context that has a different snapshot of its collection as a root. The outcome is `index_stale` | The store does not give a result of a different snapshot, and it does not select a different index |
| Lock | The lock kinds get the optional field `indexes` | A lock records the index that a run used. A lock with no index keeps its revision |
| Filter | `semantic_search` permits the `filter` of document 18 | The filter reads only declared metadata. A candidate with unknown metadata gives an item that is not `ok` |

## The method `aavanam/hashed-terms/v1`

- A term is the folded form of a word, as in the text search of document 20.
- The bucket of a term is the first four bytes of its SHA-256 digest, as an integer, modulo `dimensions`.
- The items are the units of the snapshot. The texts of an item are the text artifacts in its closure.
- The score of an item is the number of buckets that the item and the query share.
- The order is the score, then the logical ID, then the reference.

The method is a method of shared terms. It does not use the meaning of a word. The profile makes no statement about its retrieval quality, and no measured quality exists for it. Its purpose is a search path that each implementation can make again with the same result.

## The example of the issue

A reader opens a context with the snapshot S7 and the index of S7. The reader calls `semantic_search` with a query and `limit` 5. The result has five items. Each item has the revision of a unit, its logical ID and the snapshot S7. If more than five items match, the status is `partial` with the reason `bounds_reached`, because the limit is a declared bound.

## Limits

- A layered context (document 23) has no index. An index has only the items of its snapshot, so a search in a layered context gives `unknown_ref`.
- The store does not make an index for a new snapshot. The application writes the index descriptor for the new snapshot.
- The store does not check that a consumer made a search through an index that its lock records.
- The Go implementations do not declare the profile. They keep the reserved response until #132.
