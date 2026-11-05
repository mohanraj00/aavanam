# Lookup and discovery

State: draft. Prefix: `QRY`. Profile: `aavanam/base/v1`.

## Scope

This document defines exact lookup, bounded typed data queries, lexical search, the semantic index adapter envelope and bounded context bundles. Search never supplies a value to an exact lookup.

## Topics

| Topic | Subject |
|---|---|
| `GET` | Exact get by reference and selector |
| `DAT` | Bounded data queries, ordering and pagination |
| `LEX` | Lexical search |
| `SEM` | Semantic index adapter envelope |
| `BDL` | Context bundles and budgets |

## Clauses

The topic `SEM` has no clauses. The semantic index is reserved: `semantic_search` returns `reserved_profile` (RSV-RSP-1). Selectors in `get` wait for document 06.

### Exact get

**QRY-GET-1.** The request of `get` has the fields `context` (CTX-OPN-3) and `refs`, a list of one or more references. The result has one item for each reference, in the order of `refs`. If a reference is in the read context, its item has status `ok` and the field `ref`, equal to the reference.

**QRY-GET-2.** If a reference is not in the store, its item has status `not_found` and reason `unknown_ref`. If it is in the store but not in the read context, its item has status `not_found` and reason `not_in_context` (CTX-OPN-4). STS-ENV-3 gives the envelope status.

### Bounded data queries

**QRY-DAT-1.** The request of `query_data` has the fields `context` (CTX-OPN-3) and `resources`, a list of one or more references. It can also have these fields:

| Field | Value |
|---|---|
| `where` | An object. Each key is a column name. Each value is an I-JSON value |
| `metadata` | A list of one or more metadata keys (BND-OVL-2). No key occurs two times |
| `precedence` | A list of strings, highest first (BND-OVL-3) |
| `schema` | A reference. The schema that the consumer expects |
| `offset` | An integer of 0 or more. The default is 0 |
| `limit` | An integer of 1 or more. If it is absent, the request has no bound |

If a request field does not agree with this table, the result has status `failed` and reason `field_type`, and it has no items (STS-ENV-3 rule 1).

**QRY-DAT-2.** The result has one item for each reference of `resources`, in the order of `resources`. The store checks each reference in these steps, in this order. It stops at the first check that fails, and the item has the reasons of that check only.

1. Reference: QRY-GET-2 gives `unknown_ref` or `not_in_context`.
2. Kind: if the object is not a `data-resource`, the item has status `unsupported` and reason `unsupported_target_kind`.
3. Schema: if the request has `schema`, and the `schema` of the data resource is a different reference, the item has status `incompatible` and reason `schema_mismatch`.
4. Profile: if the store does not support the `data_profile`, DAT-PRF-2 gives `unsupported_profile`.
5. Payload bytes: if the bytes of the payload are not in the store, the item has status `not_found` and reason `payload_missing`.
6. Payload form: DAT-TBL-3 gives `payload_form`.
7. Columns: if a key of `where` is not in `columns`, the item has status `unresolved` and reason `unknown_column`.
8. Metadata: for each key of `metadata`, the candidates are the overlays in the read context that have the resource as subject and that key in `values`. If a key has no candidate, the item has status `unresolved` and reason `no_metadata`. Then, if BND-OVL-3 gives `overlay_conflict` for a key, the item has status `ambiguous` and reason `overlay_conflict`.

The item has one reason for each affected value, for example one `no_metadata` for each key with no candidate. STS-ENV-3 gives the envelope status.

**QRY-DAT-3.** If no check of QRY-DAT-2 fails, the item has status `ok` and the fields `ref`, `rows` and `next`. If the request has `metadata`, the item also has the field `metadata`. An item that is not `ok` has no field `rows`, `next` or `metadata`.

1. A row matches if, for each key of `where`, the value of the row in that column is equal to the value of `where` (DAT-TBL-2). If the request has no `where`, each row matches.
2. `rows` is the list of the matched rows, in the table order, from the position `offset` in that list. If the request has `limit`, `rows` has `limit` rows or fewer. Each row is an object. Its keys are the column names, and its values are the values of the row.
3. `metadata` is an object. Each key is a key of the request `metadata`, and its value is the value that BND-OVL-3 applies.
4. An item that is not `ok` has no `rows` and no `metadata`.

**QRY-DAT-4.** If an `ok` item has more matched rows after `offset` than `limit`, `next` is the sum of `offset` and `limit`, and a declared bound stopped the operation (STS-ENV-3 rule 2). If not, `next` is null. To read the next rows, a consumer sends the same request with `offset` equal to `next`. The context does not change between the two reads (CTX-OPN-2).

### Lexical search

**QRY-LEX-1.** The request of `search` has the fields `context` (CTX-OPN-3) and `query`, a string with one or more characters. It can also have these fields:

| Field | Value |
|---|---|
| `search_profile` | A string. The default is `aavanam/lexical/v1` |
| `offset` | An integer of 0 or more. The default is 0 |
| `limit` | An integer of 1 or more. If it is absent, the request has no bound |

If a request field does not agree with this table, the result has status `failed` and reason `field_type`, and it has no items. Then, if `search_profile` is not `aavanam/lexical/v1`, the result has status `unsupported` and reason `unsupported_profile`, and it has no items (STS-ENV-3 rule 1).

**QRY-LEX-2.** Under `aavanam/lexical/v1`, the candidates are each `reading` and each `excerpt` in the read context whose payload bytes are in the store. A candidate matches if the UTF-8 bytes of `query` occur in its payload bytes. The count of a match is the number of occurrences that do not overlap, from the start of the payload. The store does not change the case, does not normalize Unicode and does not divide the text into words.

**QRY-LEX-3.** The result has one item for each match, in this order: the count, highest first, then the reference, in ascending string order. The items start at the position `offset` in this list. If the request has `limit`, the result has `limit` items or fewer. Each item has status `ok` and the fields `ref`, `kind` and `count`. If more matches occur after `offset` than `limit`, a declared bound stopped the operation (STS-ENV-3 rule 2).

**QRY-LEX-4.** A search item is a location, not a value. It has no payload bytes and no data value. A consumer MUST read a value with `get` or `query_data`. A consumer MUST NOT use a search result as a value, or in place of an exact lookup that is not `ok` (STS-SET-2). A lexical search is not a semantic search (RSV-RSP-2).

### Context bundles and budgets

**QRY-BDL-1.** The request of `context_bundle` has the fields `context` (CTX-OPN-3) and `roots`, a list of one or more references. It can also have the field `budget`, an object with the fields `unit`, a string, and `max`, an integer of 0 or more. If a request field does not agree with this, the result has status `failed` and reason `field_type`, and it has no items. Then, if the `unit` is not `bytes`, the result has status `unsupported` and reason `unsupported_budget`, and it has no items (STS-ENV-3 rule 1).

**QRY-BDL-2.** If a root is not in the store or not in the read context, the result has one item for each such root, in the order of `roots`, and no other item. QRY-GET-2 gives the status and reason of each item. Each item has the field `ref`. STS-ENV-3 gives the envelope status.

**QRY-BDL-3.** If each root is in the read context, the candidates are the objects of the closures of the roots (KND-DEP-3). Each object is one candidate. The distance of a candidate is the smallest number of reference steps from a root to it. The order of the candidates is the distance, smallest first, then the reference, in ascending string order. Each item has status `ok` and the fields `ref`, `kind` and `size`. `size` is the sum of the `size` values of the payload fields of the descriptor.

**QRY-BDL-4.** If the request has no `budget`, the result has one item for each candidate. If the unit is `bytes`, the store adds the candidates in their order while the sum of their `size` values is `max` or less. At the first candidate that makes the sum more than `max`, the store stops. Then a declared bound stopped the operation: STS-ENV-3 rule 2 gives `partial` with `bounds_reached`, or rule 3 gives `failed` with `incomplete_result` if no candidate was added.

**QRY-BDL-5.** A context bundle is a list of references in the read context. It has no payload bytes. A consumer reads the objects with `get`. A `partial` bundle permits research and context use only (STS-SET-4).
