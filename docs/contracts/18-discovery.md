# Discovery

State: normative. Prefix: `DSC`. Profile: `aavanam/base/v1`.

## Scope

This document defines the discovery of the items of a read context by kind and by logical ID. Discovery is exact: it compares kinds and logical IDs as exact strings. It does not search payload bytes, and it does not rank. Lexical search is in document 09.

This document also defines the filter. A filter selects the items whose declared metadata matches exact values and half-open intervals. A listing and an export apply the same filter (BUN-FLT-1). The application owns the applicability policy. The store applies only exact equality and the range match of document 04.

## Topics

| Topic | Subject |
|---|---|
| `LST` | Item listing in a read context |
| `FLT` | The filter on declared metadata |
| `LOC` | Listing results are locations, not values |

## Clauses

### Item listing

**DSC-LST-1.** The request of `list_items` has the field `context` (CTX-OPN-3). It can also have these fields:

| Field | Value |
|---|---|
| `kinds` | A set of one or more kind names (KND-SCH-5) |
| `logical_ids` | A set of one or more logical IDs, each a string with one or more characters (IDN-LOG-1) |
| `offset` | An integer of 0 or more. The default is 0 |
| `limit` | An integer of 1 or more. If it is absent, the request has no bound |
| `filter` | A filter (DSC-FLT-1) |

The store checks the request in this order, and stops at the first check that fails. In each case, the result has no items (STS-ENV-3 rule 1).

1. If the request has a field that is not in this clause, the result has status `failed` and reason `unknown_field`.
2. If the request does not have `context`, or `kinds` or `logical_ids` is an empty list or has a duplicate, the result has status `failed` and reason `field_count`.
3. If a value does not agree with its type, the result has status `failed` and reason `field_type`. A `filter` that does not agree with DSC-FLT-1 does not agree with its type.
4. If the read context expired, the result has status `failed` and reason `context_expired`.
5. If a name of `kinds` is not a kind in the kind table, the result has status `unsupported`, with one reason `unknown_kind` for each such name.

**DSC-LST-2.** The candidates are the members of the read context (CTX-VIW-1). If the request has `kinds`, only a member whose kind is in `kinds` is a candidate. If the request has `logical_ids`, only a member whose `logical_id` is in `logical_ids` is a candidate. A member with no `logical_id` is then not a candidate. The store compares kinds and logical IDs as exact strings (IDN-LOG-1).

**DSC-LST-3.** If the request has no `filter`, the result has one item for each candidate, in the item order of LIF-ITM-3. If the request has `filter`, DSC-FLT-5 gives the candidates that have an item. In one kind and one logical ID, the items are in ascending string order of the reference. Each item has status `ok` and the fields `ref`, `kind` and `logical_id`. `logical_id` is null for a member with no logical ID. The items start at the position `offset` in this list. If the request has `limit`, the result has `limit` items or fewer. If more candidates occur after `offset` than `limit`, a declared bound stopped the operation (STS-ENV-3 rule 2). To read the next items, a consumer sends the same request with `offset` plus `limit`. The context does not change between the two reads (CTX-OPN-2).

**DSC-LST-4.** `list_items` does not need a capability (CTX-PRJ-1). A projection does not hide a member (CTX-PRJ-5). The store does not report an object outside the read context. An empty item list does not state that no such object exists outside the read context (STS-ENV-2).

### Filter on declared metadata

**DSC-FLT-1.** A filter is a JSON object with one or more of these fields, and no other field. It has `equals`, `ranges` or both.

| Field | Value |
|---|---|
| `equals` | An object with one or more keys. Each key is a metadata key (BND-OVL-2). Each value is an I-JSON value other than `null` |
| `ranges` | A list of one or more range clauses (DAT-RNG-1). The `lower` and the `upper` of a clause are metadata keys |
| `precedence` | A list of strings, highest first (BND-OVL-3) |

The filter keys are the keys of `equals`, and the `lower` and the `upper` of each clause of `ranges`.

**DSC-FLT-2.** The filter reads the declared metadata of an object, and no other value. For one object and one filter key, the candidates are the overlays in the read context that have the object as subject and the key in `values` (BND-OVL-2). The applied value is the value that BND-OVL-3 gives with the `precedence` of the filter. The store MUST NOT read a filter value from a field of the object, from a payload or from an `activation`. Thus the fields `effective_from` and `effective_until` of an activation are not filter values (PUB-ACT-5). The declared values go on each item. In a gated collection, a changed OverlaySet of the snapshot is in the covered set of the lock of the activation (PUB-ACT-3 check 5). Thus the activation confirms the values, and it does not carry them. One head can then hold items for many intervals and many regions.

**DSC-FLT-3.** The store checks one object against a filter in these steps, in this order. It stops at the first check that fails, and the outcome has the reasons of that check only. The `refs` of each reason name the object.

1. Metadata: if a filter key has no candidate, the outcome has status `unresolved` and reason `no_metadata`, with one reason for each such key.
2. Conflict: if BND-OVL-3 gives `overlay_conflict` for a filter key, the outcome has status `ambiguous` and reason `overlay_conflict`, with one reason for each such key.
3. Missing values: if the applied value of a filter key is `null`, the outcome has status `unresolved` and reason `missing_value` (DAT-MIS-2), with one reason for each such key.
4. Value types: if the applied value of the `lower` or the `upper` of a clause is not a value of the declared type of the clause, the outcome has status `incompatible` and reason `value_type` (DAT-RNG-5), with one reason for each such value of each clause.

An object with unknown or missing metadata does not match, and it does not fail to match. It has one of these outcomes. The store MUST NOT use a default value for it.

**DSC-FLT-4.** If no check of DSC-FLT-3 fails, the object matches the filter if both of these are true:

1. For each key of `equals`, the applied value is equal to the value of `equals` (DAT-TBL-2).
2. Each clause of `ranges` matches the range of the applied values of its `lower` and its `upper` (DAT-RNG-2, DAT-RNG-3, DAT-RNG-4).

The store does not interpret a value. It applies no hierarchy between two keys, no match of a part of a text and no membership in a list. For example, the applied value `["north", "south"]` is equal only to the same list, and it is not equal to `"north"`. An application that has levels declares one key for each level.

**DSC-FLT-5.** If the request of `list_items` has `filter`, the store checks each candidate of DSC-LST-2 against the filter.

1. A candidate that matches gives the item of DSC-LST-3, with status `ok`.
2. A candidate that does not match gives no item.
3. A candidate with a failed check gives one item with the status and the reasons of DSC-FLT-3. This item has no other field.

The items are in the order of DSC-LST-3, and `offset` and `limit` apply to this list. STS-ENV-3 gives the envelope status. Thus a listing with `ok` items and an item that is not `ok` has status `partial` and reason `items_not_ok`.

**DSC-FLT-6.** An item that is not `ok` shows that the store cannot tell if the object applies. A consumer MUST NOT use that object as content that applies, and MUST NOT conclude that it does not apply (STS-SET-2). The rules of this clause apply to the consumer. A store vector does not test them.

### Locations, not values

**DSC-LOC-1.** A listing item is a location, not a value. It has no payload bytes and no data value. A consumer MUST read a value with `get` or `query_data`. A consumer MUST NOT use a listing item as a value, or in place of an exact lookup that is not `ok` (STS-SET-2).

**DSC-LOC-2.** More than one item can have one kind and one logical ID, for example in a read context that composes collections (CTX-CMP-3). The store does not select one revision (IDN-LOG-3). Only a head selects a revision.
