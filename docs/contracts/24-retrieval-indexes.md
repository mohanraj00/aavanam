# Retrieval indexes

State: normative. Prefix: `IDX`. Profile: `aavanam/semantic-index/v1`.

## Scope

This document defines the retrieval index profile `aavanam/semantic-index/v1`. Document 16 reserved this profile and its operation `semantic_search`. A store declares the profile separately from the base profile (IDX-PRF-1).

A retrieval index is a projection of one snapshot. It records its snapshot, its method and the parameters of the method. A search through an index gives locations: each result names its item, the revision of the item and the snapshot. The index is never a source of truth. A rebuild from the same snapshot gives the same results.

The profile has one method. The method is deterministic, and it needs no model. This document makes no statement about the retrieval quality of a method. An embedding model is outside the profile. An application can record the name and the parameters of such a method, and the store does not run it.

## Topics

| Topic | Subject |
|---|---|
| `PRF` | The profile and its tables |
| `DEF` | The index object |
| `MTH` | Methods and the method `aavanam/hashed-terms/v1` |
| `SRC` | The search through an index |
| `STL` | A stale index |
| `LCK` | The indexes of a lock |
| `LOC` | Results are locations, not values |

## Clauses

### The profile and its tables

**IDX-PRF-1.** This document specifies the profile `aavanam/semantic-index/v1`. A store declares it separately from the base profile. The clauses of this document apply only to a store that declares the profile. A sentence of a different document that names a clause of this document applies only to such a store. A store that does not declare it stays a store of the base profile. For such a store, `semantic_search` gives `reserved_profile` (RSV-RSP-1). A descriptor of kind `retrieval-index` gives `unknown_kind` (KND-REG-3). The name `retrieval-index` in the field `kinds` of a request also gives `unknown_kind` (DSC-LST-1, LIF-CHG-1). A lock descriptor with the field `indexes` gives `unknown_field` (KND-SCH-2).

**IDX-PRF-2.** The profile registers the kind `retrieval-index`, and it adds the field `indexes` to the kinds `execution-lock` and `build-lock` (KND-REG-2). The two tables of this clause give the kind and the fields, in the form of the tables of document 02. The kind `retrieval-index` is in no target group. For a store that declares the profile, `retrieval-index` is a kind in the kind table for the field `kinds` of `list_items` and of `snapshot_changes` (DSC-LST-1, LIF-CHG-1). A descriptor of kind `retrieval-index` has the common fields of document 02, and its `descriptor_profile` is `aavanam/base/v1`.

The kind table of the profile:

| Kind | Purpose | Document |
|---|---|---|
| `retrieval-index` | A projection of one snapshot for a search: its snapshot, its method and the parameters of the method | 24 |

The field table of the profile:

| Kind | Field | Value type | Count | Targets |
|---|---|---|---|---|
| `retrieval-index` | `snapshot` | `ref` | `1` | `snapshot` |
| `retrieval-index` | `method` | `string` | `1` | - |
| `retrieval-index` | `parameters` | `json` | `1` | - |
| `execution-lock` | `indexes` | `ref` | `0..n` | `retrieval-index` |
| `build-lock` | `indexes` | `ref` | `0..n` | `retrieval-index` |

**IDX-PRF-3.** The profile has the reason code of the reason table of this clause, in the form of the reason table of document 03. STS-RSN-3 and STS-RSN-4 apply to it.

The reason table of the profile:

| Code | Status | Meaning |
|---|---|---|
| `index_stale` | `incompatible` | The snapshot of a retrieval index is not the snapshot of its collection in the read context (IDX-STL-1) |

**IDX-PRF-4.** For a store that declares the profile, `semantic_search` has the row of the operation table of this clause. The table has the form of the operation table of document 03. This row replaces the row of document 03, and RSV-LST-1 and RSV-RSP-1 do not apply to the operation (RSV-LST-4). The capability table of this clause adds one row to the capability table of CTX-PRJ-1. Thus a read context of such a store with no `capabilities` in its request has this capability also (CTX-PRJ-2).

The operation table of the profile:

| Operation | Document | Statuses |
|---|---|---|
| `semantic_search` | 24 | `ok`, `partial`, `not_found`, `unresolved`, `ambiguous`, `incompatible`, `unsupported`, `failed` |

The capability table of the profile:

| Capability | Operations |
|---|---|
| `aavanam/semantic-index/v1` | `semantic_search` |

### The index object

**IDX-DEF-1.** A retrieval index is an object of kind `retrieval-index`. Its field `snapshot` is the snapshot that it is derived from. Its field `method` is the name of its method. Its field `parameters` is the parameters of the method, a JSON value. The application writes it with `put`. The store checks it in the steps of STS-MAL-3, as each other descriptor.

**IDX-DEF-2.** An index descriptor is the full record of the index. Two index descriptors with the same snapshot, the same method and the same parameters are one object (IDN-CAN-3). The store keeps no index content as an object. An implementation MAY keep derived data for an index, for example a table of terms. Derived data is not an object. It has no revision, and it is not in a bundle. An implementation MUST be able to make it again from the snapshot of the index. A result MUST NOT depend on the presence of derived data.

**IDX-DEF-3.** An index refers to its snapshot, and the snapshot does not refer to the index (KND-REF-6). Thus the index is not in the closure of its snapshot. A root of `open_context` can have kind `retrieval-index` (CTX-OPN-1). The read context then contains the closure of the index: the index, its snapshot and the closure of the snapshot. An index root is not a snapshot root. It does not take part in CTX-CMP-1 or in the skew check (CTX-SKW-2). An index that is a member can be a root of an `export` (BUN-EXP-1).

**IDX-DEF-4.** An index has only the items of its snapshot. A layered context (document 23) has one root, the base snapshot, so an index is not a member of it. Thus `semantic_search` in a layered context gives `unknown_ref` (LYR-ISO-2). This version has no index for the view of a layer.

### Methods and the method `aavanam/hashed-terms/v1`

**IDX-MTH-1.** The profile defines one method, `aavanam/hashed-terms/v1`. Each store that declares the profile MUST support it. The `parameters` of an index with this method MUST be an object with the field `dimensions` and no other field. `dimensions` is an integer from 1 to 65536. If not, the store MUST reject the descriptor with status `failed` and reason `field_type` (STS-MAL-3 step 3).

**IDX-MTH-2.** The store compares each other method name only as a string, and it keeps its `parameters` as an opaque value. An application can thus record an index of an external method, for example a method with an embedding model. The store does not load or run an external method. `semantic_search` with such an index gives status `unsupported` with reason `unsupported_profile`. The `capabilities` of the reason name the method. The `refs` of the reason name the index. This profile defines no search for an external method.

**IDX-MTH-3.** The items of an index are the references in the field `units` of its snapshot. The texts of an item are the payloads of each `artifact` in the closure of the item that has a text media type (CRD-TXT-2). The store reads these payloads as bytes. It does not decode or parse them (CRD-TXT-7).

**IDX-MTH-4.** Under `aavanam/hashed-terms/v1`, a term is the folded form of a word (CRD-TXT-3). The bucket of a term is a number. The store takes the SHA-256 digest of the bytes of the term. The first four bytes of the digest are an unsigned integer, with the most significant byte first. The bucket is this integer modulo `dimensions`. The bucket set of an item is the set of the buckets of the terms of its texts. The bucket set of a query is the set of the buckets of the terms of the UTF-8 bytes of the query.

**IDX-MTH-5.** The score of an item for a query is a count of buckets. A bucket counts if it is in the bucket set of the item and in the bucket set of the query. A score is an integer. An item with the score 0 does not match. The method uses no floating point value, no time and no random value.

### The search through an index

**IDX-SRC-1.** The request of `semantic_search` has the fields `context` (CTX-OPN-3), `index`, a reference, and `query`, a string of 1 to 1024 Unicode scalar values. It can also have these fields:

| Field | Value |
|---|---|
| `offset` | An integer of 0 or more. The default is 0 |
| `limit` | An integer of 1 or more. If it is absent, the request has no bound |
| `filter` | A filter (DSC-FLT-1) |

The store checks the request in this order, and stops at the first check that fails. In each case, the result has no items (STS-ENV-3 rule 1).

1. If the request has a field that is not in this clause, the result has status `failed` and reason `unknown_field`.
2. If the request does not have `context`, `index` or `query`, the result has status `failed` and reason `field_count`.
3. If a value does not agree with its type, the result has status `failed` and reason `field_type`. A query with no word, or with more than 32 words, does not agree with its type (CRD-TXT-1 step 3).
4. If the read context expired, the result has status `failed` and reason `context_expired`.
5. If the context does not have the capability `aavanam/semantic-index/v1`, CTX-PRJ-4 gives `capability_unavailable`.

**IDX-SRC-2.** If the request passes the checks of IDX-SRC-1, the store checks the index in these steps, in this order. It stops at the first check that fails. The result has the status and the reason of that check, and it has no items. The `refs` of the reason name the index. For step 3, they name the index and then the root (IDX-STL-1).

1. Reference: QRY-GET-2 gives `unknown_ref` or `not_in_context`. Thus the index MUST be a member of the read context.
2. Kind: if the object is not a `retrieval-index`, the result has status `unsupported` and reason `unsupported_target_kind`.
3. Stale index: IDX-STL-1 gives `index_stale`.
4. Method: IDX-MTH-2 gives `unsupported_profile`.

**IDX-SRC-3.** If no check of IDX-SRC-2 fails, the candidates are the items of the index that match the query (IDX-MTH-5). Each candidate gives one item with status `ok` and these fields:

| Field | Value |
|---|---|
| `ref` | The revision of the item |
| `kind` | The kind of the item, `knowledge-unit` |
| `logical_id` | The `logical_id` of the item, or null |
| `snapshot` | The snapshot of the index |
| `index` | The index of the request |
| `score` | The score of the item (IDX-MTH-5) |

**IDX-SRC-4.** If the bytes of a text of an item are not in the store, the item has no score. It gives one item with status `not_found` and reason `payload_missing`. The `refs` of the reason name the item. The store does not skip it, and it does not give it a score from its other texts.

**IDX-SRC-5.** If the request has `filter`, the store checks each candidate against the filter (DSC-FLT-3), with the overlays of the read context. A candidate that matches gives the item of IDX-SRC-3. A candidate that does not match gives no item. A candidate with a failed check gives one item with the status and the reasons of DSC-FLT-3, and no other field (DSC-FLT-5). The filter does not change a score.

**IDX-SRC-6.** The candidates are in this order: first the score, highest first. In one score, first the candidates with a logical ID, in ascending string order of the logical ID. Then the reference, in ascending string order. The item of a candidate has the position of the candidate, also if the item is not `ok` (IDX-SRC-5). The items of IDX-SRC-4 come after these items, in ascending string order of the reference of the item. The items start at the position `offset` in this list. If the request has `limit`, the result has `limit` items or fewer. If more items occur after `offset` than `limit`, a declared bound stopped the operation. STS-ENV-3 gives the envelope status.

**IDX-SRC-7.** The items of a search depend only on these inputs: the index descriptor, the closure of its snapshot, the payload bytes, the overlays of the read context and the request. They do not depend on the time of the call, on the order of earlier calls or on derived data (IDX-DEF-2). Thus a rebuild of the index from the same snapshot gives the same items. A device store has an `ok` import of a bundle with an index as a root. A read context on the device store with that root gives the same items as the writer store, if the bundle withholds no bytes (BUN-CLS-3). `semantic_search` does not change the store.

### A stale index

**IDX-STL-1.** An index is stale in a read context if both of these are true:

1. The read context has a snapshot root whose `collection` is the `collection` of the snapshot of the index.
2. That root is not the snapshot of the index.

An operation that uses a stale index gives status `incompatible` with reason `index_stale`. The `refs` of the reason name the index and that root. The store MUST NOT give a result of a stale index, and it MUST NOT use a different index in its place.

**IDX-STL-2.** The store does not make an index for a new snapshot. After a `publish`, the application writes an index descriptor for the new head snapshot. An index of an earlier snapshot stays an object. It is not stale in a read context that has no different snapshot of its collection as a root.

### The indexes of a lock

**IDX-LCK-1.** The request of `execution_inputs` or `build_inputs` can have the field `indexes`, a set of references (KND-SCH-5). A value that is not a list of references gives `field_type`. A reference that occurs again gives `field_count`. An empty list gives a lock with no `indexes` (KND-SCH-3).

**IDX-LCK-2.** The store checks each reference of `indexes` with the other references of the request, in these steps (LCK-EXE-1, LCK-EXE-2, LCK-BLD-1, LCK-BLD-2). If an index is not in the store or not in the read context, the result has status `not_found`, with reason `unknown_ref` or `not_in_context`. If the object is not a `retrieval-index`, the result has status `unsupported` and reason `unsupported_target_kind`. If no outcome of these steps and of LCK-CAP-2 applies, and an index is stale, the result has status `incompatible` and reason `index_stale` (IDX-STL-1). The store checks this before LCK-EXE-3. The result has one reason for each affected index. The store does not check the method of an index for a lock.

**IDX-LCK-3.** If the result is `ok`, the lock has the references in its field `indexes` (LCK-EXE-4, LCK-BLD-3). `indexes` records the indexes that the consumer declares it used. The store does not check that the consumer made a search. The items of the result stay the closure of LCK-EXE-4 or LCK-BLD-3, and the covered set of the lock does not have the indexes (PUB-ACT-2). A device store writes the same lock to its local output store (BUN-IMP-7).

### Results are locations, not values

**IDX-LOC-1.** A search item is a location, not a value. It has no payload bytes and no data value. A consumer MUST read a value with `get`, `read` or `query_data`. A consumer MUST NOT use a search item as a value, or in place of an exact lookup that is not `ok` (STS-SET-2). An empty item list does not state that no item of the snapshot applies to the query (STS-ENV-2). The rules of this clause apply to the consumer. A store vector does not test them.

**IDX-LOC-2.** A score is a count of the method, not a measure of relevance. The profile makes no statement about the retrieval quality of `aavanam/hashed-terms/v1` or of an external method. A consumer MUST NOT use a score from one index as a value for a different index, a different method or different parameters.
