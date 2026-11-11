# Producer storage

State: draft. Prefix: `PRD`. Profile: `aavanam/base/v1`.

## Scope

This document defines the producer section of a store. A producer keeps its working data in this section, for example receipts, selectors, review tasks and logs. The section keeps opaque objects under the keys of the producer, logs that only grow, and a record of each removal. It is outside each snapshot, each closure, each read context and each bundle.

The producer section is not the workspace of the reserved profile `aavanam/workspace/v1` (RSV-LST-1). A workspace is for content before a snapshot. The producer section is for data that never becomes a snapshot.

## Topics

| Topic | Subject |
|---|---|
| `SEC` | The section and its requests |
| `OBJ` | Objects |
| `LOG` | Logs |
| `RMV` | Removal and its records |
| `ISO` | Isolation from the other content |
| `PRO` | An adapter to the store protocol of a producer |

## Clauses

### The section and its requests

**PRD-SEC-1.** A writer store has one producer section (BUN-IMP-5). The section needs no declaration, and it is not a section of PUB-SEC-1. A device store and a local output store have no producer section. On these stores each operation of this document MUST give status `unsupported` with one reason, `producer_section_absent`. The store checks the role after the operation (STS-MAL-4) and before the request. The result has no items, and the store does not change. BUN-IMP-6 does not apply to the operations of this document.

**PRD-SEC-2.** Each request of this document has the field `producer`, a name of 1 to 256 characters. Each producer has its own objects, logs and removal records. An operation reads and changes only the data of the producer in its request. A producer name is not authentication, and the store does not check that the caller is the producer.

**PRD-SEC-3.** The request fields of each operation are in the table below. A key, a log name part and a removal reason are strings of 1 to the given number of characters. Each of these strings and the producer name agrees with I-JSON, as a string of a descriptor (KND-SCH-4). `data` and `entry` are base64 text (RFC 4648 section 4). `sequence` is an integer of 1 or more. A log name is a list of 1 to 8 strings. If a request has a field that is not in its row, the result is `failed` with `unknown_field`. If a field is absent, the result is `failed` with `field_count`. If a field has a value of a different form, the result is `failed` with `field_type`. The store checks these in this order, as in PUB-CND-1. The result has no items, and the store does not change (STS-MAL-2).

| Operation | Fields |
|---|---|
| `producer_put` | `producer`, `key` (512), `data` |
| `producer_get` | `producer`, `key` (512) |
| `producer_forget` | `producer`, `key` (512), `reason` (1024) |
| `producer_removals` | `producer` |
| `producer_append` | `producer`, `log` (each part 512), `sequence`, `entry` |
| `producer_read` | `producer`, `log` (each part 512) |

**PRD-SEC-4.** The store MUST NOT parse, decode, load or execute the bytes of an object or a log entry (KND-OPQ-2). It compares them only as bytes. It does not check that a key comes from its bytes. The producer and its adapter own each rule about keys and bytes (PRD-PRO-1).

**PRD-SEC-5.** The store MUST keep the data of the producer section for the life of the store. A later open of the store reads the same data.

### Objects

**PRD-OBJ-1.** `producer_put` keeps the bytes of `data` under `key`. If the producer has no object with the key, the store adds the object. The result is `ok` with one item. The item has the fields `key` and `size`, the number of bytes.

**PRD-OBJ-2.** If the producer has an object with the key and the same bytes, `producer_put` is `ok` with the same item, and the store does not change. If the bytes are different, the result is `incompatible` with reason `producer_key_conflict` and no items. The store does not change. Thus an object never changes (constitution principle 4).

**PRD-OBJ-3.** `producer_get` gives the bytes of the object with `key`. The result is `ok` with one item, with the fields `key` and `data`. `data` is the base64 text of the bytes. If the producer has no object with the key, the result is `not_found` with no items. The reason is `producer_removed` if a removal record of the producer has the key (PRD-RMV-1). If not, the reason is `unknown_producer_key`.

**PRD-OBJ-4.** After a removal, `producer_put` of the same key adds the object again. The removal record stays.

### Logs

**PRD-LOG-1.** A log has a name, a list of strings. The store compares two names part by part. A log is a list of entries. Each entry has a sequence number and opaque bytes. The first entry has the sequence number 1, and each next entry has the next integer.

**PRD-LOG-2.** `producer_append` adds `entry` to the log `log` if `sequence` is the number of entries in the log plus 1. The result is `ok` with one item, with the field `sequence`. If `sequence` has a different value, the result is `incompatible` with reason `log_moved` and no items, and the store does not change. Thus two writers that read the same log cannot both add the same sequence number.

**PRD-LOG-3.** `producer_read` gives the entries of the log `log` in the order of their sequence numbers. Each entry is one `ok` item with the fields `sequence` and `entry`, the base64 text of the bytes. A log with no entry gives `ok` with no items.

**PRD-LOG-4.** No operation changes or removes an entry of a log. `producer_forget` does not change a log. If an entry holds data that the producer must remove, the producer keeps that data in an object and puts only the key in the entry.

### Removal and its records

**PRD-RMV-1.** `producer_forget` removes the object with `key`, and records the key, the time and the reason. If the producer has the object, the store removes the bytes and adds a removal record in one transaction. A removal record has the fields `sequence`, `key`, `time` and `reason`. `sequence` counts the removal records of the producer from 1. `time` is the UTC time of the store clock in the form `YYYY-MM-DDTHH:MM:SSZ` (RFC 3339). The result is `ok` with one item that has the fields of the record.

**PRD-RMV-2.** If the producer has no object with the key, `producer_forget` gives `not_found` with the reason of PRD-OBJ-3 and no items. The store does not change.

**PRD-RMV-3.** After an `ok` `producer_forget`, no operation of the store gives the bytes of the removed object. This topic does not require the removal of the bytes from the storage media, a journal, a backup or a copy that the application made.

**PRD-RMV-4.** `producer_removals` gives the removal records of the producer in the order of their sequence numbers. Each record is one `ok` item with its fields. No operation changes or removes a removal record.

**PRD-RMV-5.** A removal from the producer section MUST NOT remove or change an object, a descriptor or payload bytes outside the producer section. This is also true if the removed bytes are equal to the bytes of a payload. The removal of a source from the closure of a snapshot is a feature of the reserved profile `aavanam/gc/v1` (RSV-LST-1). It is not part of this document.

### Isolation from the other content

**PRD-ISO-1.** An object or a log entry of the producer section is not a descriptor and not payload bytes. A producer key is not a reference. Thus no snapshot, closure (KND-DEP-3), read context, lock or bundle contains producer data, and no reference field can name it. An operation that is not of this document MUST NOT read or give producer data.

**PRD-ISO-2.** Producer bytes do not supply the bytes of a payload. If the bytes of a payload field are only in the producer section, `put` of the descriptor gives `payload_missing` (STS-MAL-3 step 5). A `get` in a read context of a reference that is equal to a producer key gives the result for a reference that is not in the store.

**PRD-ISO-3.** `export` never has producer data (BUN-EXP-2), and `import` never writes to the producer section. The operations of this document do not change a descriptor, payload bytes, a context or the catalog of publications.

### An adapter to the store protocol of a producer

**PRD-PRO-1.** A producer can define a store protocol with five operations: put, get, append, read and forget. An adapter can implement this protocol on the producer section. The adapter is an application component. It does the checks of the producer before each call, for example the key of the bytes, the form of the bytes, the form of a log name and the fields of a log chain. The store does not do these checks (PRD-SEC-4).

**PRD-PRO-2.** The adapter uses these operations:

| Protocol operation | Store operations |
|---|---|
| put | `producer_put` |
| get | `producer_get` |
| append | `producer_read` to find the next sequence number, then `producer_append` with it |
| read | `producer_read` |
| forget | `producer_forget` |
| A read of the removal log of the producer | `producer_removals` |

**PRD-PRO-3.** The adapter MUST give a status other than `ok` to the producer as a failure of the protocol, never as a value or a success (STS-SET-2). It gives `not_found` as the error of the protocol for an absent object. If an append gives `log_moved`, the adapter can read the log again and try again with the new sequence number.
