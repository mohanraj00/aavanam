# Lifecycle

State: draft. Prefix: `LIF`. Profile: `aavanam/base/v1`.

## Scope

This document defines the lifecycle of an item across snapshots. An item is the set of revisions of one logical ID and one kind. The store reports which items two snapshots add, remove or replace, and the history of one logical ID across the publications of a collection. A lifecycle result states only that revisions are different. It does not state that a revision is true, false, stale or authoritative.

## Topics

| Topic | Subject |
|---|---|
| `ITM` | Items and their revisions in a snapshot |
| `CHG` | Changes between two snapshots |
| `HST` | History of one logical ID in a collection |

## Clauses

### Items

**LIF-ITM-1.** The content of a snapshot is each object of the closure of the snapshot (KND-DEP-3), except the snapshot. Only the typed reference fields give this closure. The store MUST NOT get the content from a payload, from a `json` value or from `ext`.

**LIF-ITM-2.** The item key of an object with a `logical_id` is its kind and its `logical_id`. The item key of an object with no `logical_id` is its kind and its reference. The revisions of an item key in a snapshot are the references of the objects of the content with that key. An item key can have more than one revision in one snapshot. The store does not select one (IDN-LOG-3).

**LIF-ITM-3.** The order of the items of this document is the item order: first the kind, in ascending string order. In one kind, first the items with a logical ID, in ascending string order of the logical ID. Then the items with no logical ID, in ascending string order of the reference. A list of revisions is in ascending string order.

**LIF-ITM-4.** `snapshot_changes` and `item_history` read the objects and the catalog of the store, not a read context. Their requests have no field `context`, and their envelope field `context` MUST be null (STS-ENV-1). Snapshots do not change (PUB-SNP-3), and no operation changes a publication record (PUB-LOG-2). Thus a second call with the same request gives the same items, unless a later `publish` adds a publication. A read context cannot have two snapshots of one collection (CTX-CMP-1), so these operations do not use one.

### Changes between two snapshots

**LIF-CHG-1.** The request of `snapshot_changes` has the fields `before` and `after`, two references. It can also have these fields:

| Field | Value |
|---|---|
| `kinds` | A set of one or more kind names (KND-SCH-5) |
| `offset` | An integer of 0 or more. The default is 0 |
| `limit` | An integer of 1 or more. If it is absent, the request has no bound |

The store checks the request in this order, and stops at the first check that fails. In each case, the result has no items (STS-ENV-3 rule 1).

1. If the request has a field that is not in this clause, the result has status `failed` and reason `unknown_field`.
2. If the request does not have `before` or `after`, or `kinds` is an empty list or has a duplicate, the result has status `failed` and reason `field_count`.
3. If a value does not agree with its type, the result has status `failed` and reason `field_type`.
4. If a name of `kinds` is not a kind in the kind table, the result has status `unsupported`, with one reason `unknown_kind` for each such name.

**LIF-CHG-2.** If the request form is correct, the store checks `before` and then `after`. If a reference is not in the store, its item has status `not_found` and reason `unknown_ref`. If its object is not a `snapshot`, its item has status `unsupported` and reason `unsupported_target_kind`. Each such item has the field `ref`, and the reason names the reference. If one or both references fail a check, the result has these items only, in the order `before`, `after`. STS-ENV-3 gives the envelope status.

**LIF-CHG-3.** If both references are snapshots, the candidates are the item keys with one or more revisions in `before` or in `after` (LIF-ITM-2). The store compares the two lists of revisions of each candidate:

| `before` revisions | `after` revisions | `change` |
|---|---|---|
| None | One or more | `added` |
| One or more | None | `removed` |
| One or more | One or more, and the lists are not equal | `replaced` |
| One or more | The same list | No item |

If the request has `kinds`, only a candidate whose kind is in `kinds` gives an item.

**LIF-CHG-4.** The result has one item for each changed candidate, in the item order (LIF-ITM-3). Each item has status `ok` and the fields `kind`, `logical_id`, `change`, `before` and `after`. `logical_id` is null for an object with no logical ID. `before` and `after` are the lists of revisions in each snapshot. A list with no revision is an empty list. The items start at the position `offset` in this list. If the request has `limit`, the result has `limit` items or fewer. If more items occur after `offset` than `limit`, a declared bound stopped the operation (STS-ENV-3 rule 2).

**LIF-CHG-5.** An object with no logical ID has one revision for each key. Thus it gives only `added` or `removed`. A `replaced` item does not state that the `after` revisions are a correction of the `before` revisions. An empty item list states only that the two contents have the same item keys and the same revisions. The two snapshots can be of different collections. The store does not compare the `collection` values.

### History of one logical ID

**LIF-HST-1.** The request of `item_history` has the fields `collection`, a string, and `logical_id`, a string with one or more characters. If the request has a different field, the result has status `failed` with reason `unknown_field`. If it does not have `collection` or `logical_id`, the reason is `field_count`. If a value does not agree with its type, the reason is `field_type`. The store applies these checks in this order. The result has no items.

**LIF-HST-2.** If the request form is correct, the result has status `ok`, and one item for each recorded publication of the collection (PUB-LOG-2), in the order of the head updates (PUB-LOG-3). Each item has status `ok` and the fields `candidate`, `previous_head`, `head`, `revisions` and `change`. `candidate`, `previous_head` and `head` have the values of the publication record.

**LIF-HST-3.** `revisions` is the list of the objects of the content of `head` (LIF-ITM-1) whose `logical_id` is the `logical_id` of the request. Each value is an object with the fields `ref` and `kind`. The list is in ascending string order of `ref`. The previous revisions are the same list for `previous_head`. If `previous_head` is null, the previous revisions are an empty list.

**LIF-HST-4.** `change` compares the references of the previous revisions with the references of `revisions`:

| Previous revisions | `revisions` | `change` |
|---|---|---|
| None | None | `absent` |
| None | One or more | `added` |
| One or more | None | `removed` |
| One or more | One or more, and the references are not equal | `replaced` |
| One or more | The same references | `unchanged` |

**LIF-HST-5.** A collection with no recorded publication gives status `ok` and no items. A head of a different collection, a snapshot that `publish` did not make a head, and a publication in a different store are not in the result. `item_history` does not change the store. It reads the catalog of the store, not a read context, so it does not need a capability (CTX-PRJ-1). The history states which revisions each head contains. It does not state when a revision became effective or which revision is authoritative.
