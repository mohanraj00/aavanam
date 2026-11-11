# Discovery

State: draft. Prefix: `DSC`. Profile: `aavanam/base/v1`.

## Scope

This document defines the discovery of the items of a read context by kind and by logical ID. Discovery is exact: it compares kinds and logical IDs as exact strings. It does not search payload bytes, and it does not rank. Lexical search is in document 09.

## Topics

| Topic | Subject |
|---|---|
| `LST` | Item listing in a read context |
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

The store checks the request in this order, and stops at the first check that fails. In each case, the result has no items (STS-ENV-3 rule 1).

1. If the request has a field that is not in this clause, the result has status `failed` and reason `unknown_field`.
2. If the request does not have `context`, or `kinds` or `logical_ids` is an empty list or has a duplicate, the result has status `failed` and reason `field_count`.
3. If a value does not agree with its type, the result has status `failed` and reason `field_type`.
4. If the read context expired, the result has status `failed` and reason `context_expired`.
5. If a name of `kinds` is not a kind in the kind table, the result has status `unsupported`, with one reason `unknown_kind` for each such name.

**DSC-LST-2.** The candidates are the members of the read context (CTX-VIW-1). If the request has `kinds`, only a member whose kind is in `kinds` is a candidate. If the request has `logical_ids`, only a member whose `logical_id` is in `logical_ids` is a candidate. A member with no `logical_id` is then not a candidate. The store compares kinds and logical IDs as exact strings (IDN-LOG-1).

**DSC-LST-3.** The result has one item for each candidate, in the item order of LIF-ITM-3. In one kind and one logical ID, the items are in ascending string order of the reference. Each item has status `ok` and the fields `ref`, `kind` and `logical_id`. `logical_id` is null for a member with no logical ID. The items start at the position `offset` in this list. If the request has `limit`, the result has `limit` items or fewer. If more candidates occur after `offset` than `limit`, a declared bound stopped the operation (STS-ENV-3 rule 2). To read the next items, a consumer sends the same request with `offset` plus `limit`. The context does not change between the two reads (CTX-OPN-2).

**DSC-LST-4.** `list_items` does not need a capability (CTX-PRJ-1). A projection does not hide a member (CTX-PRJ-5). The store does not report an object outside the read context. An empty item list does not state that no such object exists outside the read context (STS-ENV-2).

### Locations, not values

**DSC-LOC-1.** A listing item is a location, not a value. It has no payload bytes and no data value. A consumer MUST read a value with `get` or `query_data`. A consumer MUST NOT use a listing item as a value, or in place of an exact lookup that is not `ok` (STS-SET-2).

**DSC-LOC-2.** More than one item can have one kind and one logical ID, for example in a read context that composes collections (CTX-CMP-3). The store does not select one revision (IDN-LOG-3). Only a head selects a revision.
