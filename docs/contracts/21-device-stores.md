# Device stores

State: normative. Prefix: `DEV`. Profile: `aavanam/device-sqlite/v1`.

## Scope

This document defines a storage layout for a device store and its local output store (BUN-IMP-5): two SQLite database files, their tables, the FTS5 index for `search_text` and the transaction of an import. The layout has the profile `aavanam/device-sqlite/v1`. Its clauses are not part of the base profile.

This document adds no operation, no status and no reason. Documents 11 and 20 give the behavior of a device store. A device store with a different storage does not declare this layout profile, and the base profile still applies to it.

## Topics

| Topic | Subject |
|---|---|
| `PRF` | The layout profile and its declaration |
| `LAY` | The files, the tables and the column types |
| `FTS` | The FTS5 index of the text search |
| `OUT` | The output file |
| `UPD` | The transaction of an import |
| `RMV` | The transaction of a removal |
| `MIG` | A file of an earlier version of the layout |

## Clauses

### Layout profile

**DEV-PRF-1.** The identity of the layout is `aavanam/device-sqlite/v1`. A store that declares this layout MUST agree with each clause of this document. The layout needs SQLite 3.45 or later, with FTS5. An incompatible change of the layout makes a new identity, for example `aavanam/device-sqlite/v2`.

**DEV-PRF-2.** Each file of the layout has the table `meta`, with the columns `key` (`TEXT`, the primary key) and `value` (`TEXT`). The row with the key `layout` has the value `aavanam/device-sqlite/v1`. The row with the key `role` has the value `device` or `output` (BUN-IMP-5). The store writes the two rows when it creates the file, and they do not change. Before a reader uses a file, it MUST check the two rows. If a row is absent or has a different value, the reader MUST NOT use the file as a store of this layout. This failure is a failure to open the store. It is not the result of an operation.

### Files and tables

**DEV-LAY-1.** A device store of this layout is two SQLite database files in one directory. The device file has the name `device.sqlite` and the role `device`. The output file has the name `output.sqlite` and the role `output`. It is the local output store of the device store (BUN-IMP-7). The application selects the directory and creates the two files together.

**DEV-LAY-2.** The column types follow these rules.

1. A reference or a payload digest is a `BLOB` of 32 bytes: the SHA-256 value, without the text `sha256:`. The ascending byte order of this value is the ascending string order of the reference.
2. A JSON string value of a descriptor, for example a `logical_id` or a `collection`, is a `BLOB` with the UTF-8 bytes of the string. Thus the value can have U+0000, and its byte order is the code point order.
3. A descriptor is a `BLOB` with its canonical bytes (IDN-CAN-1). The store keeps no second encoding of a descriptor.
4. A value that this document calls a JSON list is a `BLOB` with the RFC 8785 bytes of the list.

**DEV-LAY-3.** The device file has these tables, and no other table with content of the store. FTS5 adds its own shadow tables for `text_index`.

| Table | Columns | A row is |
|---|---|---|
| `meta` | `key`, `value` | DEV-PRF-2 |
| `objects` | `ref` (key), `kind`, `logical_id`, `collection`, `canonical` | One descriptor of the store. `logical_id` and `collection` are null if the descriptor does not have the field |
| `edges` | `src`, `dst`, `field` | One reference of KND-DEP-1: the descriptor `src` refers to `dst` in the field `field`. The key is the three columns |
| `payloads` | `digest` (key), `size`, `data` | The bytes of one digest, one time (IDN-DIG-2) |
| `deferred` | `digest`, `ref` | One record of BUN-IMP-10: the descriptor `ref` has a deferred check that needs the bytes of `digest`. The key is the two columns |
| `imports` | `seq` (key), `roots` | One `ok` import (DEV-UPD-2) |
| `contexts` | `id` (key), `roots`, `capabilities` | One read context (CTX-OPN-2). `roots` and `capabilities` are JSON lists |
| `context_ends` | `id` (key), `expires` | The end of the declared life of one read context (DEV-LAY-6). A context with no `life` has no row |
| `closures` | `root`, `member` | Derived. The object `member` is in the closure of `root` (KND-DEP-3). The key is the two columns |
| `table_rows` | `digest`, `idx`, `row` | Derived. Row `idx` of the table payload `digest` (`aavanam/table/v1`), in the table order, from 0. `row` is the RFC 8785 bytes of the row |
| `text_docs` | `id` (key), `digest` | Derived. One indexed digest (DEV-FTS-2). `id` is an integer, and `digest` has no duplicate |
| `text_index` | `tokens` | Derived. The FTS5 table of DEV-FTS-1 |

**DEV-LAY-4.** The tables `edges`, `closures`, `table_rows`, `text_docs` and `text_index` are a function of `objects` and `payloads`. `edges` comes only from the typed reference fields of the descriptors (KND-DEP-2). A rebuild of these tables from `objects` and `payloads` MUST give the same rows, except the `id` values of `text_docs`. A store can add the rows of `closures` and `table_rows` at the first use of a root or of a table. No operation gives a value from a derived table without the descriptor or the payload bytes that the value comes from.

**DEV-LAY-5.** (withdrawn) DEV-LAY-6 replaces this clause. This clause told a store to add the table `context_ends` before it used a file of an earlier version. Thus a program that only read such a file changed it. DEV-LAY-6 keeps the other rules of this clause, and DEV-MIG-2 gives the time of the addition.

**DEV-LAY-6.** The column `expires` of `context_ends` is an `INTEGER`: the end of the declared life of the context, in milliseconds from 1970-01-01T00:00:00Z (CTX-LIF-2). The table `contexts` keeps its three columns, so a reader of an earlier version of this layout reads the file. That reader does not read `context_ends`, so it does not end a context at the end of its life. A file of an earlier version has no table `context_ends`. Each context of that file has no declared life, and DEV-MIG-2 states when a store adds the table. When a read context ends, the store removes its `contexts` row and its `context_ends` row, or it does not use the rows again (CTX-LIF-5).

### Text index

**DEV-FTS-1.** `text_index` is the FTS5 virtual table of this statement:

```sql
CREATE VIRTUAL TABLE text_index USING fts5(tokens, tokenize='ascii', detail='full', content='');
```

The row ID of a row of `text_index` is the `id` of one row of `text_docs`. The table has no content of its own, so the store keeps no copy of the payload text.

**DEV-FTS-2.** After each `ok` import, `text_docs` has one row for each payload digest that agrees with these two conditions, and no other row:

1. A descriptor of kind `artifact` in the device file names the digest in its `payload`, with a text media type (CRD-TXT-2).
2. The device file has the bytes of the digest.

Thus the store adds the row of a digest in the transaction that makes the two conditions true (DEV-UPD-1), also if the bytes come in a later import than the descriptor (BUN-UPD-5).

**DEV-FTS-3.** The token of a word (CRD-TXT-3) is the letter `t` and then the first 16 hexadecimal digits of the SHA-256 value of the folded form of the word, in lowercase. A token has 17 ASCII letters and digits. Thus the `ascii` tokenizer does not cut it and does not change it, for a word of each length and with each byte. The document of a digest in `text_index` is the token of each word of the payload bytes, in the word order, with one space (U+0020) between two tokens. A payload with no word has an empty document.

**DEV-FTS-4.** For a `search_text` request, the store makes the token of each word of the query (CRD-TXT-3 rule 4). The FTS5 query is one phrase: the tokens in the query order, with one space between two tokens, in double quotes. The store MUST NOT put the request text or a part of it in the FTS5 query. The rows that FTS5 gives name the digests that the store scans.

**DEV-FTS-5.** The index only selects digests. The store MUST get each word, each offset and each match from the payload bytes (CRD-TXT-3, CRD-TXT-4), and it gives the items in the order of CRD-ORD-1. Equal folded forms give equal tokens. Thus the index gives each digest that has a match, and it agrees with CRD-TXT-3. Two different words can have the same token. Then the index gives a digest with no match, and the scan removes it. The candidates and their outcomes come from the members of the read context, not from the index. Thus a candidate with no bytes in the store gives `payload_missing` (CRD-TXT-5), and BUN-IMP-12 applies to each candidate before the store uses its bytes.

### Output file

**DEV-OUT-1.** The output file has the tables `meta`, `objects`, `edges`, `payloads`, `deferred`, `contexts`, `context_ends`, `closures` and `table_rows`, with the columns of DEV-LAY-3. It has no `imports`, no `text_docs` and no `text_index`. It keeps only the objects and the bytes that the local output store keeps (BUN-IMP-8), for example the locks of BUN-IMP-7.

**DEV-OUT-2.** A connection to the output file reads the device file through a read-only attachment, for example `ATTACH DATABASE 'file:device.sqlite?mode=ro' AS device`. A call on the local output store MUST NOT write to the device file. The objects and the bytes of the local output store are the rows of the two files (BUN-IMP-8).

**DEV-OUT-3.** A read context of the local output store can have a candidate of `search_text` whose digest is not in `text_docs` of the device file, because only the output file has its descriptor or its bytes. The store MUST scan the bytes of each such candidate. It uses the index only for a digest in `text_docs`. Thus the result does not depend on the file that has the candidate.

### Import transaction

**DEV-UPD-1.** Each `import` on the device store is one `BEGIN IMMEDIATE` transaction on the device file. After the checks of BUN-IMP-2 and BUN-UPD-5 pass, the transaction adds these rows, and no other row:

1. The `objects` and `edges` rows of each descriptor of the bundle that the file does not have.
2. The `payloads` row of each digest of the bundle that the file does not have.
3. The changes of `deferred` that BUN-IMP-10 and BUN-IMP-11 give.
4. The `text_docs` and `text_index` rows that DEV-FTS-2 gives.
5. The `closures` rows of each root of the bundle. The store can also add them at the first use of the root (DEV-LAY-4).
6. One `imports` row (DEV-UPD-2).

If the import is not `ok`, the transaction MUST roll back, and the two files do not change (BUN-UPD-6).

**DEV-UPD-2.** The `imports` row of an `ok` import has `seq`, the number of `imports` rows before the call plus 1, and `roots`, the JSON list of the roots of the bundle in the order of the bundle. An import of a bundle whose objects and bytes are all in the store also adds a row. An `imports` row is a record of a call. It is not content of the store in the sense of BUN-UPD-2, and only `gc_collect` reads it (BUN-RMV-1). An application can read the table to find the roots of the newest bundle, because an import does not copy the catalog of the writer store (BUN-CLS-3).

**DEV-UPD-3.** An import MUST NOT delete or change a row of `meta`, `objects`, `edges`, `payloads`, `imports`, `contexts`, `closures`, `table_rows` or `text_docs` (BUN-UPD-3). Only the transaction of DEV-RMV-1 deletes a row of content. It removes a `deferred` row only as BUN-IMP-10 states. `put`, `create_snapshot`, `create_candidate` and `publish` on the device store give `read_only_store` and change no row (BUN-IMP-6). `open_context` on the device store adds one `contexts` row and can add `closures` rows. For a request with `life`, it also adds one `context_ends` row. It changes no other table. `close_context` changes only the tables `contexts` and `context_ends`.

### Removal transaction

**DEV-RMV-1.** Each `gc_collect` on the device store is one `BEGIN IMMEDIATE` transaction on the device file. The store reads the objects and the open read contexts of the output file for the retained roots (BUN-RMV-1), and it MUST NOT write to the output file. For each object that the call removes (LIF-GC-5), the transaction deletes its `objects` row, each `edges` row with the object as `src`, each `deferred` row with the object as `ref`, and each `closures` row with the object as `root` or as `member`. For each payload digest that the call removes, the transaction deletes its `payloads` row, its `table_rows` rows, its `text_docs` row and the row of `text_index` with that row ID. The transaction MUST NOT delete an `imports` row or a `meta` row. After the transaction, the derived tables agree with DEV-LAY-4 and DEV-FTS-2. If the call is not `ok`, the transaction MUST roll back.

**DEV-RMV-2.** The device file can have a `meta` row with the key `removals`. Its value is a decimal integer of 1 or more: the number of transactions of DEV-RMV-1 that removed an object or payload bytes. A file with no such row has the count 0. Each transaction of DEV-RMV-1 that removes an object or payload bytes MUST add 1 to the count in that transaction. More than one program can have the two files open at the same time. A store that keeps a descriptor, a table head or other content of the device file in memory MUST read the count at the start of each call, and it MUST drop that content if the count changed. An update of the device file or of the output file that follows a check of content MUST read the count after it has the write lock of its file. If the count is not the count at the start of the call, the update MUST NOT write, and the store applies the checks of the call again. A reader of an earlier version of this layout does not read the row. That reader does not have this protection.

### A file of an earlier version

**DEV-MIG-1.** A file of an earlier version of this layout has the `meta` rows of DEV-PRF-2, with the layout identity `aavanam/device-sqlite/v1`. It can be different from a file of this version only in these points:

| Difference | A store of this version |
|---|---|
| The file has no table `context_ends` | It reads the file as a file whose table `context_ends` has no row (DEV-LAY-6) |
| The device file has no `meta` row `removals` | It reads the count 0 (DEV-RMV-2) |

A store of this version MUST open such a file, and document 25 gives the results of its operations (MIG-VER-2).

**DEV-MIG-2.** A file can have no table `context_ends`. A store adds the table in the transaction that adds its first row. This is the transaction of an `open_context` with `life` (DEV-UPD-3). It adds the `meta` row `removals` in the first transaction of DEV-RMV-1 that removes an object or payload bytes (DEV-RMV-2). A store MUST NOT add a table or a `meta` row to a file of an earlier version at a different time, and it MUST NOT change a row that the earlier version wrote, except as DEV-UPD-3 and DEV-RMV-1 permit.

**DEV-MIG-3.** The open of the two files MUST NOT change a byte of the device file or of the output file. A call that adds, changes or removes no row MUST NOT change a byte of the two files either. Thus a program does not change a file of an earlier version if it only reads through a read context that the file has, and if no read adds a derived row. A read can add a derived row at the first use of a table, as DEV-LAY-4 permits. The files that SQLite keeps beside a database file, for example `device.sqlite-wal`, are not the device file.
