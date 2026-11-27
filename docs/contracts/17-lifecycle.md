# Lifecycle

State: draft. Prefix: `LIF`. Profile: `aavanam/base/v1`.

## Scope

This document defines the lifecycle of an item across snapshots. An item is the set of revisions of one logical ID and one kind. The store reports which items two snapshots add, remove or replace, and the history of one logical ID across the publications of a collection. A lifecycle result states only that revisions are different. It does not state that a revision is true, false, stale or authoritative. This document also defines the end of the life of stored content: `gc_collect` removes the content that no retained root reaches.

## Topics

| Topic | Subject |
|---|---|
| `ITM` | Items and their revisions in a snapshot |
| `CHG` | Changes between two snapshots |
| `HST` | History of one logical ID in a collection |
| `GC` | The retained roots and the removal of the content that no retained root reaches |

## Clauses

### Items

**LIF-ITM-1.** The content of a snapshot is each object of the closure of the snapshot (KND-DEP-3), except the snapshot and each removed object (REL-RMV-8). Only the typed reference fields give this closure. The store MUST NOT get the content from a payload, from a `json` value or from `ext`.

**LIF-ITM-2.** The item key of an object with a `logical_id` is its kind and its `logical_id`. The item key of an object with no `logical_id` is its kind and its reference. The revisions of an item key in a snapshot are the references of the objects of the content with that key. An item key can have more than one revision in one snapshot. The store does not select one (IDN-LOG-3).

**LIF-ITM-3.** The order of the items of this document is the item order: first the kind, in ascending string order. In one kind, first the items with a logical ID, in ascending string order of the logical ID. Then the items with no logical ID, in ascending string order of the reference. A list of revisions is in ascending string order.

**LIF-ITM-4.** `snapshot_changes` and `item_history` read the objects and the catalog of the store, not a read context. Their requests have no field `context`, and their envelope field `context` MUST be null (STS-ENV-1). Snapshots do not change (PUB-SNP-3), and no operation changes a publication record (PUB-LOG-2). Thus a second call with the same request gives the same items, unless a later `publish` adds a publication, or a later `remove` or `gc_collect` removes an object that the request reads (REL-RMV-8, LIF-HST-6). A read context cannot have two snapshots of one collection (CTX-CMP-1), so these operations do not use one.

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

**LIF-HST-2.** If the request form is correct, the result has one item for each recorded publication of the collection (PUB-LOG-2), in the order of the head updates (PUB-LOG-3). Each item has the fields `candidate`, `previous_head` and `head`, with the values of the publication record. An `ok` item also has the fields `revisions` and `change`. Each item is `ok`, unless LIF-HST-6 applies to it. STS-ENV-3 gives the envelope status.

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

**LIF-HST-6.** A `gc_collect` can remove a snapshot that a publication record names (LIF-GC-5). The record stays (PUB-LOG-2). If the store does not have the snapshot `head` of a record, or it does not have a `previous_head` that is not null, the item of the record has status `not_found`, with one reason `unknown_ref` for each such snapshot. The `refs` of the reason name the snapshot. The item has no `revisions` and no `change`. The store MUST NOT give an empty list of revisions for a snapshot that it does not have.

### Retained roots and removal

**LIF-GC-1.** `gc_collect` removes the objects and the payload bytes that no retained root reaches. A writer store and a device store support it. On a local output store, `gc_collect` MUST give status `unsupported` with one reason, `unsupported_role`. The store checks the role after the operation (STS-MAL-4) and before the request. The request can have the field `roots`, a set of references (KND-SCH-5), with zero or more values. If the request has a different field, the result is `failed` with `unknown_field`. If a value occurs again in `roots`, the result is `failed` with `field_count`. If `roots` is not a list of references, the result is `failed` with `field_type`. The store checks these in this order, as in PUB-CND-1. The result has no items, and the store does not change.

**LIF-GC-2.** The retained roots of a writer store are these objects, and no other object:

1. The head of each collection (PUB-HED-1).
2. The receipt and the activation of the newest publication record of each collection (PUB-LOG-2).
3. The snapshot of each candidate that has no publication record (PUB-CND-4).
4. Each member of each open read context (CTX-LIF-6).
5. Each value of `roots`.

BUN-RMV-1 gives the retained roots of a device store. The retained set is the closure of the retained roots (KND-DEP-3). The closure follows the kept `refs` of a removed object (REL-RMV-8).

**LIF-GC-3.** A snapshot that a later head replaced is not a retained root. A lock, a layer (LYR-DEF-3), a retrieval index (IDX-DEF-1) and a descriptor that no snapshot names yet are not retained roots either. The application names each such object in `roots` to keep it, or it keeps a read context open on it. A lease that keeps a root for a declared period stays reserved (RSV-LST-1). An application SHOULD NOT call `gc_collect` while a writer prepares a snapshot. If it does, the next write of that writer gives `missing_target` or `unknown_ref` for a removed object, and the writer writes the object again.

**LIF-GC-4.** If the request form is correct, the store checks each value of `roots`. If the store does not have the object, the result has status `not_found`, with one reason for each such value. The reason is `unknown_ref`, or `removed` for a removed object (REL-RMV-7). The `refs` of the reason name the value. The result has no items, and the store does not change.

**LIF-GC-5.** If each value of `roots` is in the store, the store does these actions in one transaction:

1. It removes each object that is not in the retained set, with its `refs` and its deferred check records (BUN-IMP-10).
2. It removes the bytes of each payload digest that no object of the store names after action 1.
3. A writer store removes each candidate whose snapshot action 1 removes.

`gc_collect` MUST NOT remove or change an object or bytes of the retained set. It MUST NOT remove a publication record (PUB-LOG-2), a removal record (REL-RMV-6), a declaration of a gate or of a section, data of the producer section (PRD-ISO-3) or a read context. It adds no removal record. `gc_collect` is a complete operation (STS-ENV-4): it does each action or none.

**LIF-GC-6.** The result is `ok`, with one item for each object that action 1 removed. Each item has status `ok` and the fields `ref` and `kind`. The order of the items is not specified. The envelope has the fields `payloads`, the list of the payload digests that action 2 removed, and `candidates`, the list of the candidate identities that action 3 removed. Each list is in ascending string order. A call that removes nothing gives `ok` with no items and two empty lists, and the store does not change. Thus a removal is never silent: the result names each object, each digest and each candidate that the store removed.

**LIF-GC-7.** After an `ok` `gc_collect`, each object and the bytes of the retained set are in the store. A read context that the store opens later with a retained root has the same members as before the call, and each operation in it gives the same status, reasons and items as before the call. A reference to an object that `gc_collect` removed gives the outcome of a reference that is not in the store, for example `unknown_ref`. A `publish` of a removed candidate gives `unknown_candidate` (PUB-HED-3). A later `put` or `import` can add a removed object again (IDN-CAN-3).
