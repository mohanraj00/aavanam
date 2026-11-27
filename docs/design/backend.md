# Design: the production backend and the device bundle

State: proposal. Issues: #47 (the production backend) and #65 (the device bundle). This document is the PR plan. It is not normative. The spec and the conformance vectors have authority over it. Section 9 lists the spec changes that come first.

## 1. Decisions and layout

The production backend is a Go service over PostgreSQL. The device store is a SQLite file with an FTS5 index. Both use one Go core. The goal is that all 631 vectors pass on the backend, and that the device and output steps pass on the device store.

- `backend/`: the Go module `github.com/mohanraj00/aavanam/backend`.
  - `core/`: the storage-neutral logic. `jsontext` (strict JSON reader), `canon` (RFC 8785 and digests), `kinds` (the kind registry of document 02), `check` (STS-MAL-3), `envelope` (document 03), `ops` (the dispatch of the operation table), `read`, `text`, `transfer` and `write` (the operations), `memstore` (a storage in memory, for tests only).
  - `pg/`: the PostgreSQL storage and its schema migrations.
  - `api/`: the network read binding (section 5).
  - `cmd/aavanam-server/`: the service. `cmd/aavanam-conform/`: the conformance driver (section 6).
  - `bench/`: the budget generator and measurement (section 7).
  - `conformance/passing.txt`: the vector IDs that pass on the backend.
- `reference/`: the Go module `github.com/mohanraj00/aavanam/reference`, the device reader that #65 names. It is an application module, not a part of the core store package. It imports `backend/core` with a `replace` directive to `../backend`.
  - `device/`: the SQLite storage, the FTS5 index and the bundle update. `cmd/aavanam-device/`: the device driver. `devicetest/`: the tests. `conformance/passing.txt`.
- Versions: Go 1.27 (the `go` directive is `go 1.27`). PostgreSQL 17. SQLite 3.45 or later, with FTS5. CI tests only these versions.
- Third-party dependencies, three in total:
  - `github.com/jackc/pgx/v5` in `backend/`. The PostgreSQL driver. The standard library has no PostgreSQL driver.
  - `modernc.org/sqlite` in `reference/`. A SQLite driver in Go with FTS5 included. It needs no cgo, so a device build can cross-compile.
  - `golang.org/x/text` in `backend/`, only the package `unicode/norm`. `core/check` uses it for the NFC check of ADP-SEL-4. The standard library has no normalization tables. The PostgreSQL driver already needs this module.
- No JSON or RFC 8785 library. The standard `encoding/json` decoder changes a lone surrogate escape and invalid UTF-8 into U+FFFD, and it accepts a duplicate key. These changes hide `malformed_json`, `duplicate_key` and `non_ijson_value` (STS-MAL-3 step 1). Thus `core/jsontext` is a strict reader with an explicit stack and no recursion, as `src/aavanam/jsontext.py` is.
- CI runs PostgreSQL as a GitHub Actions service container, `postgres:17` pinned by digest, with a health check. The jobs read `AAVANAM_PG_DSN`. A local run uses the same image in Docker.

## 2. Schema

Each table that has source content takes inserts only, with one exception: the update of `remove` or of `gc_collect` deletes rows (see Removal below). Each JSON string value (a logical ID, a collection name, a policy name, a producer key) goes into a `bytea` column as UTF-8. Reason: a PostgreSQL `text` value cannot hold U+0000, and I-JSON permits it. A reference or a digest goes into a `bytea` column of 32 bytes. The ascending byte order of the 32 bytes is the ascending string order of `sha256:<hex>`. The byte order of UTF-8 is the code point order, so each ordered read gives the order of the reference implementation. Each ordered text column uses `COLLATE "C"`. The schema never uses `jsonb`, because `jsonb` changes numbers and removes duplicate keys.

| Table | Columns | Source |
|---|---|---|
| `objects` | `ref` (key), `kind`, `logical_id`, `collection`, `canonical` | Each descriptor, in its canonical bytes (IDN-CAN-1). On PostgreSQL, `collection` of a layer has its base collection, for the section check of LYR-DEF-5 |
| `edges` | `src`, `dst`, `field` | The `refs` of KND-DEP-1, computed from the descriptor. No other dependency list (KND-DEP-2) |
| `payloads` | `digest` (key), `size`, `data` | The bytes of each digest, one copy (IDN-DIG-2) |
| `deferred` | `ref`, `digest`, `check` | The deferred checks of BUN-IMP-9 and BUN-IMP-10 |
| `candidates` | `id` (key), `snapshot`, `policy`, `expected_head`, `checks` | PUB-CND-3, a catalog record |
| `heads` | `collection` (key), `snapshot` | PUB-HED-1. The only table that takes an update |
| `publications` | `seq`, `collection`, `candidate`, `receipt`, `activation`, `previous_head`, `head` | PUB-LOG-2 |
| `gates`, `sections` | `collection`, `section` | Store configuration (PUB-GAT-2, PUB-SEC-2) |
| `collection_locks` | `collection` (key) | One lock row for each collection name, also with no head (section 3). Not content |
| `producer_objects`, `producer_entries`, `producer_removals` | as document 19 | The Producer section (PRD-SEC-1) |
| `contexts` | `id` (key), `roots`, `capabilities`, `skew_policy`, `opened`, `expires`, `gone` | The read contexts (CTX-OPN-2). `expires` is the end of the declared life in milliseconds, or null (CTX-LIF-1). `gone` has the removed objects of the closures at the time of `open_context`, which are not members (REL-RMV-8) |
| `removals` | `sequence` (key), `ref`, `kind`, `time`, `reason` | The removal records (REL-RMV-5). Inserts only |
| `removal_epoch` | `epoch` | One row. Each update that removes content increments it. Not content |
| `closures` | `root`, `member` | Derived: the closure of each root of a read context (KND-DEP-3) |
| `text_tokens` | `digest` (key), `bits` | Derived: the set of the word tokens of a text payload (CRD-TXT-3), as a bit set |
| `table_heads` | `digest` (key), `columns`, `count` | Derived: the declared columns and the number of rows of a payload of `aavanam/table/v1` |
| `table_rows` | `digest`, `idx`, `cells` | Derived: the rows of that payload, in the table order. `cells` is the RFC 8785 bytes of the row |

- Sections. The Serving section and the Evidence section are the `sections` declarations on collections. Both are in one logical store (PUB-SEC-6), so they use one set of tables. The Producer section is in its own tables. A device store has no section tables.
- Locks and bundles. A lock is a descriptor of kind `execution-lock` or `build-lock` in `objects`. A bundle is not stored. `export` makes it from `objects` and `payloads`, and `import` writes its content into these tables.
- Immutability. The service connects with a role that has `INSERT` and `SELECT` on the content tables, and no `UPDATE` or `DELETE`. A trigger on each content table refuses `UPDATE` and `DELETE` as a second barrier. `heads`, `deferred`, `contexts` and the Producer tables take updates or deletes only as the spec states (PUB-HED-3, BUN-IMP-10, PRD-RMV-1). Only the conformance role can `TRUNCATE`.
- Derived tables. `closures`, `text_tokens`, `table_heads` and `table_rows` are a function of the content. They are separate from the source content (constitution principle 4). A rebuild from the content gives the same rows. A row comes at the first use of its root, its text digest or its table, so a store from a version with no derived table needs no separate rebuild.
- Canonical bytes. `core/canon` makes the canonical form from the parsed value. It sorts object keys by UTF-16 code units (RFC 8785 section 3.2.3), not by UTF-8 bytes. It writes numbers in the ECMAScript form, as `es_number` in `conformance/runner.py` does. It escapes only `"`, `\` and the control characters below U+0020, and it never escapes `<`, `>`, `&`, U+2028 or U+2029. The I-JSON check compares the exact decimal value of a number literal (`math/big`) with the nearest double. The runner computes each revision by itself and compares it with the returned `ref`, so each `put` vector checks the byte equality.

## 3. Write path

- `put`. `core/check` applies STS-MAL-3 to each descriptor in Go, before the transaction, in the order of the spec. The checks read the store through the storage interface. The storage does not change the order, because no check is a database constraint. Then one transaction writes all descriptors, or none (STS-MAL-2). A repeat descriptor is an `INSERT ... ON CONFLICT DO NOTHING`, so the store does not change (IDN-CAN-3). A failed commit gives `batch_aborted` (STS-ENV-5). The store never deletes a descriptor, so a target that a check found stays present until the commit.
- The collection lock. A transaction that must see a stable head or a stable section of a collection first writes the `collection_locks` row of the name with `INSERT ... ON CONFLICT DO NOTHING`, then locks it with `SELECT ... FOR UPDATE`. The row exists also for a collection with no head and no snapshot. Thus two first publications, or a declaration and a snapshot write, wait for each other. The fingerprint does not include this table.
- `create_snapshot`, and a `put` of a `snapshot` descriptor, apply STS-MAL-3 from step 3 (PUB-SNP-2). The write transaction takes the collection lock, reads the section of the collection again and applies PUB-SEC-3 again before the commit. It writes the snapshot in the same transaction. The `closures` rows of the snapshot come at its first use as a root (section 4). `create_candidate` writes one catalog record (PUB-CND-4).
- `publish` and the activation gate. Step 1 to 4 of PUB-HED-3 and the activation checks (PUB-ACT-3) read without a lock. The validator runs with no open transaction (PUB-RCP-3). Then one transaction takes the collection lock, applies step 3 and the activation checks again, compares the expected head (null if the collection has no head), and writes the head and the publication. A second `publish` at the same time waits for the lock. Then it gives `head_moved`. A validator is a function that the host application registers (`core.RegisterPolicy`). The service calls a validator over HTTP at a configured address. If the validator does not answer before its deadline, the result is `failed` with `deadline_exceeded`, and the store does not change (section 9).
- Gates and sections. The service reads them from its configuration at start, before the first call (PUB-GAT-1, PUB-SEC-1). It writes them into `gates` and `sections`, and keeps them for the life of the store. A section declaration takes the collection lock. It fails, and the store does not change, if the collection has a different section or a snapshot in the store (PUB-SEC-2). Thus a declaration from a second open of the store and a snapshot write cannot both succeed. A Go test with two connections covers this, as `tests/test_sections.py` does for the reference.
- Deferred checks. Each write that adds bytes of a digest, and each `import` that keeps a withheld digest, takes a digest lock in its transaction: `pg_advisory_xact_lock` on the first 8 bytes of the digest, in ascending digest order, so two writes cannot deadlock. After the lock, the transaction reads `payloads` and `deferred` again.
  - An `import` with a withheld digest applies BUN-IMP-11 again in its transaction. If the bytes are now in the store, the payload bytes step of STS-MAL-3 (digest and size) and each excerpt check that the bytes permit run again. A failed check gives its status and reason, and the import does not commit. If the bytes are still absent, the import writes the `deferred` rows (BUN-IMP-10).
  - A write that adds bytes applies each `deferred` check of that digest with the bytes then in the store or in the write (BUN-IMP-10), and removes the rows that pass.
  - The two writes take the same lock, so the second sees the commit of the first. Go tests with two connections cover the three cases of `tests/test_withheld.py`.
- Producer section. `producer_append` locks the log row and compares the sequence with the count plus 1 (PRD-LOG-2). `producer_forget` removes the bytes and adds the removal record in one transaction (PRD-RMV-1).
- Removal (#69). `remove` and `gc_collect` are the only updates that delete rows of `objects`, `edges`, `payloads` and `candidates`. The trigger of these tables permits a DELETE only in a transaction that set `aavanam.removal`, and only these two updates set it. Each such update takes an exclusive advisory lock for its transaction. Each other update that writes takes the same lock in shared mode before its first write, and then reads `removal_epoch`. If the epoch is not the epoch at the start of the call, a removal committed after the checks of the call. Then the update stops with no write, and the dispatch runs the call again, at most 4 times. Thus a write does not keep a reference to an object that a removal took after the check of KND-REF-2. A second run of a `publish` can call the validator of the policy a second time. The removal increments `removal_epoch` in its transaction. Each call reads the epoch one time, and a process drops its parsed descriptors and its context values if the epoch changed. `remove` keeps the `edges` rows of the removed object, so a stored closure stays correct (REL-RMV-8). `gc_collect` deletes the `edges` and `closures` rows of each removed object, and the `text_tokens`, `table_heads` and `table_rows` rows of each removed digest. `remove` finds the objects that name a digest with a scan of the descriptor text. This scan has no index, so it reads each descriptor one time for each payload of the removed object.
- Store fingerprint. The conformance driver computes a SHA-256 digest over each content table, the catalog tables and the Producer tables, row by row in key order, as `Store.fingerprint` in `src/aavanam/store.py` does. It does not include `contexts`, the configuration tables or the derived tables. The service has no fingerprint endpoint.

Check order. Each check of the vectors runs in Go, and no vector plants a storage fault. Thus the exact check order of STS-MAL-3 holds on PostgreSQL and on SQLite, and this plan has no reference-only set. If a vector fails only because of a storage limit, the author changes the spec first. A core set and a reference-only set come only from a spec change that gives the rule and the list.

## 4. Read path

- `open_context` resolves the heads of the request in one statement (CTX-VIW-3), checks the roots, and writes one `contexts` row with the resolved roots. The identity is 128 random bits in hexadecimal, with the prefix `ctx-`. The content of a context is the closures of its roots, so a context has no member list of its own. A member check is an index read on `closures (root, member)`. The `closures` rows of a root come at its first use as a root, from one recursive statement on `edges`. The statement writes the rows in ascending order of `root` and `member`, so two calls with the same roots in a different sequence do not wait for each other in a circle. The closure of a stored root does not change, because `put` and `import` refuse a descriptor with a target that the store does not have. `open_context` does not read a closure for the skew check of a request with fewer than two different snapshot roots (CTX-SKW-2). A context that is not in `contexts`, or is older than the configured retention, gives `context_expired`. The conformance driver sets no retention. A layered context (document 23) has two roots in its row: the base snapshot and then the layer. No other context has a root of kind `layer` (CTX-OPN-1). The storage keeps the closures of the two roots. The core removes each replaced member and each object that only a replaced member reaches (LYR-PRE-2). For that, it reads only the closures of the replaced members, and it keeps the result for the next call with the context.
- Content reads. `get` and `read` read `objects` and `payloads` (CRD-READ-2). A row selector (`aavanam/row/v1`) reads one `table_rows` row and the columns of `table_heads`. The store writes the `table_heads` row and the `table_rows` rows when it first parses the table under DAT-TBL-3, so a row read does not parse the whole payload. The cache has only a payload that agrees with DAT-TBL-1. A table with no row has a `table_heads` row, so `query_data` gives `unknown_column` from the cache as from a parse. `query_data` reads each row of the table from `table_rows` and applies the filters in `core/read`. If the descriptor has a deferred record, the read gets the bytes and makes the deferred checks first (BUN-IMP-12). Each item uses the canonical bytes, never a second encoding.
- Text search (document 20). The words, the offsets and the matches always come from the payload bytes, in `core/text`. The index only gives the candidate digests:
  1. The token of a word is the first 8 bytes of the SHA-256 digest of its folded form (CRD-TXT-3). The store writes one `text_tokens` row for each digest that an `artifact` with a text media type names (CRD-TXT-2). The row is a bit set of the different tokens of the bytes: each token sets 4 bits, and the set has 2 bytes for each different token.
  2. A query gives its words, and each word gives its token. The index reads the bit set of each text digest of the members of the context, in one statement. It returns each digest whose set has each bit of each query token.
  3. `core/text` scans the bytes of each such digest and gives the matches of CRD-TXT-4 in the order of CRD-ORD-1.
  4. A candidate with no bytes gives `payload_missing` (CRD-TXT-5). This comes from the members, not from the index. A member with a deferred record gets its deferred checks before the index applies (BUN-IMP-12), so a member whose bytes are in the store and whose check fails gives `payload_missing` too.
  5. A digest with no `text_tokens` row is not in the index. The search scans its bytes, as with no index, and then writes its row. It writes the rows of one group of payloads before it reads the next group, so it keeps the tokens of one group at most.

  Equal words give equal tokens and equal bits, so the index drops no digest that has a match. Two different tokens can set the same bits, and the set keeps no word order. Then step 3 removes the false candidate. Thus the result is exact on PostgreSQL, and a rebuild of the index gives the same items (CRD-ORD-3). The store never uses `tsvector`, a stemmer or a locale, and never sends the request text to an index query. The phrase match, the separator rule for byte 0, the bytes that are not UTF-8 and the ASCII case rule come from the scan in `core/text`. The tests of `core/text` and the vectors of document 20 check them.

  The process keeps the member list of a context and its text digests in memory, because the content of a context does not change (CTX-OPN-2). This cache has a size bound and holds no payload bytes. Each call reads the deferred records again, in one statement. Each call reads the bytes of the candidates in groups of 8 MiB at most, with one statement for each group.

  Reason for a bit set, and not one row for each word: `S1` has about 300 million words in 2 GiB of text. One row for each word, or for each different word of each digest, is larger than the payload bytes, and the size target of section 7 is 1.5 times the payload bytes for all relations. The device store keeps the word order in its FTS5 index (section 8), because a device has one bundle.
- Lexical search (QRY-LEX-2) reads each `reading` and `excerpt` candidate with `position(query IN data)` on `bytea`, an exact byte match. `core/read` counts the occurrences that do not overlap.
- Filters (DSC-FLT, BUN-FLT, DAT-RNG) run in `core/read` on the values that the overlays and the rows give. The storage only returns the candidates.
- `export` makes the bundle from the closure of the roots, in the canonical bytes. `import` applies BUN-IMP-2 in Go, then writes in one transaction (BUN-IMP-4).

## 5. Network read binding

- Transport: HTTP/1.1 over TCP. Each operation is `POST /v1/op/<operation>`. The body is the request JSON text. The server does not decode it before `core/jsontext`, so `malformed_json` and `duplicate_key` stay visible. The response has status 200 and the result envelope (STS-ENV-1) as its body. Each status of the operation table is in the envelope, never in the HTTP status.
- Scope: the operations that do not write content: `open_context`, `get`, `query_data`, `search`, `context_bundle`, `evidence`, `coverage`, `observations`, `render`, `compare`, `impact`, `list_items`, `read`, `search_text`, `snapshot_changes`, `item_history`, `publications` and `export`. An operation name that is not in the operation table gives the envelope of STS-MAL-4 (`unsupported_operation` or `reserved_profile`). A write operation of the table gives HTTP 404 on the read listener. The writes go to a second listener, which binds to a Unix socket by default.
- Limits: the server declares a body size limit and a deadline. A larger body gives HTTP 413 with no envelope. A deadline gives `deadline_exceeded`.
- Version rule: the API identity is `aavanam-read/v1`. `GET /v1/about` returns `api`, `api_minor`, `profile`, `profile_minor` and the list of operations. A compatible change increments `api_minor`: a new operation, a new envelope or item field, or a new reason code with a status of the table. Any other change is incompatible and makes `/v2`: a removed or renamed operation or field, a changed field type, a changed status or meaning.
- Compatibility statement: a client of `aavanam-read/v1` MUST ignore an envelope or item field that it does not know. It MUST treat a reason code that it does not know by its status, and only `ok` is success (STS-SET-2). After `/v2` starts, the server keeps `/v1` for one or more minor releases, and the release notes give the last one.
- Access control is outside the functional scope (constitution). The read listener binds to the loopback address by default. A deployment puts the host access control in front of it.

## 6. Vector runner adapter

`conformance/backend_adapter.py` uses only the Python standard library. It starts two processes: `aavanam-conform` (the writer store, PostgreSQL) and `aavanam-device` (the device store and its output store, SQLite). Each process reads one JSON object per line on stdin and writes one per line on stdout.

| Adapter method | Message | Effect |
|---|---|---|
| `reset()` | `{"cmd": "reset"}` to both | `aavanam-conform` truncates all tables and clears the policies. `aavanam-device` deletes its two files and makes new ones. After it, a store declares no profile |
| `declare(profiles)` | `{"cmd": "declare", "profiles"}` to both | Each driver makes new empty stores that declare the profiles (LYR-PRF-1, IDX-PRF-1). The runner calls it after `reset()`, only for a vector of a profile |
| `call(op, req, store)` | `{"cmd": "call", "store", "op", "request"}` | The result envelope. A read operation on `writer` goes to the HTTP read listener (section 5). An operation that a declared profile adds, `semantic_search`, is not in the read binding, so it goes over the line protocol |
| `fingerprint(store)` | `{"cmd": "fingerprint", "store"}` | The digest of section 3 |
| `accept(policy, validate)` | `{"cmd": "accept", "policy"}` to both | The driver registers a policy that asks the adapter. During a call, the driver writes `{"validate": {"policy", "candidate", "receipt", "activation"}}`. The adapter calls `validate` and writes `{"accepted": true}` or `false` |
| `gate(collection)` | `{"cmd": "gate", "collection"}` to both | PUB-GAT-1 |
| `section(collection, section)` | `{"cmd": "section", ...}` to the writer | PUB-SEC-1. Only a writer store |

- Store roles: `writer` is the PostgreSQL store. `device` and `output` are the two SQLite files of BUN-IMP-5. The output file attaches the device file read-only (BUN-IMP-8).
- Selection: `AAVANAM_WRITER=backend|reference` and `AAVANAM_DEVICE=go|reference`. Thus the device store can run with the Python writer before the backend is complete. `AAVANAM_READ_HTTP=0` sends all calls over stdin, for a debug run.
- Ratchet: the runner gets the option `--expect FILE`. The run passes if each listed vector passes. The run also prints each vector that passes and is not listed. Each lane adds its vectors to its `passing.txt`. The last PR lists all vectors.

## 7. Size and time budget

These numbers are targets, not claims. A claim needs a measured result (constitution).

| Store | Descriptors | Payload bytes | Collections | Members of one context | Largest table |
|---|---|---|---|---|---|
| `S1` (server) | 200 000 | 2 GiB | 50, each with a head | 20 000 | 100 000 rows, 20 MiB |
| `S0` (CI and device) | 20 000 | 200 MiB | 10 | 2 000 | 10 000 rows, 2 MiB |

| Operation | p95 target on `S1`, one client | p95 target on `S0`, device |
|---|---|---|
| `open_context` with one head | 25 ms | 25 ms |
| `read` of one row (`aavanam/row/v1`) | 10 ms | 5 ms |
| `read` of a 64 KiB artifact | 10 ms | 5 ms |
| `search_text`, two words, 2 000 text artifacts | 100 ms | 100 ms |

- Size target: the PostgreSQL relations of `S1` take 1.5 times the payload bytes plus 2 KiB for each descriptor, or less, with all indexes. The bench reads `pg_total_relation_size`.
- Method: `backend/bench` makes the store from a fixed seed with neutral synthetic data. It runs 1 000 calls of each operation over the HTTP read binding on the loopback address, with PostgreSQL on the same host, and writes p50, p95 and the size as JSON.
- CI runs `S0` on each PR that changes `backend/` or `reference/`. The job fails only on a clear miss: a p95 that is more than three times its target and is not a known miss with an open issue. Reason: shared runners vary. [docs/bench.md](../bench.md) gives the rule, the device measures and the results. The maintainer runs `S1` on a declared machine and records the result with the machine description.

## 8. Device bundle (#65)

- Layout: a device store is two SQLite files, `device.sqlite` and `output.sqlite`, with the layout profile `aavanam/device-sqlite/v1`. The tables are `meta` (layout and role), `objects`, `edges`, `payloads`, `deferred`, `imports`, `contexts`, `closures`, `table_rows`, `text_docs` (a row ID for each indexed digest) and `text_index`. The column types follow section 2, with `BLOB` in place of `bytea`. The output file has no `imports` and no text tables. It reads the device objects through `ATTACH ... ?mode=ro`.
- FTS5 rule: `text_index` is `fts5(tokens, tokenize='ascii', detail='full', content='')`. The document of a digest is the token of each word, in word order, with one space between tokens. The FTS5 token of a word is `t` and the first 16 lowercase hexadecimal digits of the SHA-256 digest of its folded form. A token has 17 ASCII letters and digits, so the `ascii` tokenizer never cuts or changes it. The query is one FTS5 phrase of the query tokens, and FTS5 gives the candidate digests. Then section 4, steps 3 and 4, give the items. The request text never goes into the FTS5 query (CRD-TXT-3).
- Bundle update: this replaces the `read_only_store` refusal of a second `import` (BUN-IMP-6). A device store accepts each `import` that passes BUN-IMP-2, with the checks against the bundle and the store. One `BEGIN IMMEDIATE` transaction adds only the absent objects and bytes, the text tokens of the new text digests, the `closures` rows of the new roots and one `imports` row. `put`, `create_snapshot`, `create_candidate` and `publish` still give `read_only_store`. A failed update does not change the store (BUN-IMP-4).
- Layered contexts: the `contexts` row of a layered context has the base snapshot and then the layer in `roots`, as in section 4. Thus the layout has no change for the layer profile, and `open_context` changes no other table (DEV-LAY-3, DEV-UPD-3).
- Old contexts: an update never removes or changes an object, a `contexts` row or a `closures` row. Thus a context on the roots of B1 has the same members after the import of B2 (CTX-OPN-2). A context on the roots of B2 finds the words of B2. A removal of old content needs `gc_collect`, which stays reserved (document 16).
- No Evidence bytes: a device bundle is an `export` whose roots are snapshots of `serving` collections. By PUB-SEC-3 and PUB-SEC-4, it has no Evidence material (CRD-SRV-1). Two checks prove this. A vector exports a Serving head from a store that has Evidence in an `evidence` collection, and expects no Evidence descriptor and no Evidence digest in the bundle. A test in `reference/devicetest/` imports that bundle and checks that the device file has no row of kind `source`, `reading`, `excerpt` or `evidence`, and no payload that only such a descriptor names. The device store does not refuse Evidence descriptors at import. Reason: BUN-CLS-3 and the acceptance vectors move Evidence to a device today. A refusal needs a separate spec decision.
- Shared code: the device reader shares the Go core (`jsontext`, `canon`, `check` and the operations). It adds only the SQLite storage, the FTS5 index and the bundle update. Reason: one Go implementation of the read contract for both stores. The Python reference stays the independent implementation, so the vectors still compare two implementations.

## 9. Spec and vector changes first

All of these go into PR 1, spec first, then the vectors, then `src/`.

- Document 11, topic `IMP`: change BUN-IMP-6 in place (a draft) so that a second `import` is permitted. Add the topic `UPD` with the bundle update rules of section 8: the checks, the added objects only, the unchanged contexts and the deferred checks across two imports (BUN-IMP-10).
- Document 11, topic `CLS` or `UPD`: the device bundle of a Serving context has no Evidence material, with a reference to PUB-SEC-4.
- New document 21, Device stores (prefix `DEV`): the SQLite layout, the layout profile, the FTS5 token rule and the output file.
- New document 22, Network read binding (prefix `NET`): the transport, the scope, the limits, the version rule and the compatibility statement of section 5.
- Document 13, topic `RCP`: the outcome of a validator that does not answer before its deadline.
- Document 20, CRD-TXT-3: an index MUST NOT drop a candidate that has a match. It refers to the FTS5 rule of document 21.
- `SPEC.md`: the rows of documents 21 and 22, and base profile minor version 12.
- Vectors in `devices/`: replace `device-import-again-refused` and keep its reason in the PR. Add the update vectors with pairs: a second import, an old context after the update, a search in the new context and a failed update that leaves the store unchanged. Add the Serving export vector of section 8 in `redistribution/` or `devices/`.
- `src/aavanam/`: the bundle update in the reference device store.

## 10. PR plan

Level 1 paths: `SPEC.md`, `docs/contracts/`, `conformance/`, `.github/`, `AGENTS.md`, `tools/claude_review.py`, `go.mod` and `go.sum`. PR 1 and PR 2 hold all the level 1 changes. No later PR adds a dependency. If one must, it becomes level 1.

| PR | Title | Files | After | Level | Vectors that pass | Runner hint |
|---|---|---|---|---|---|---|
| 1 | Specify the bundle update, the device layout and the read binding | `SPEC.md`, `docs/contracts/11,13,20,21,22`, `conformance/vectors/devices/`, `conformance/README.md`, `src/aavanam/`, `tools/check_spec.py` if needed | this design | 1 | All, on the reference | Well-specified build with clear tests |
| 2 | Add the Go modules, the backend adapter and the CI jobs | `backend/go.mod`, `backend/go.sum`, `reference/go.mod`, `reference/go.sum`, `backend/cmd/aavanam-conform/` (a driver that gives `unsupported_operation`), `conformance/backend_adapter.py`, `conformance/runner.py` (`--expect`), `conformance/test_runner.py`, `.github/workflows/ci.yml`, `.github/gates.json`, `.github/dependabot.yml`, `AGENTS.md` (layout), `Makefile` | 1 | 1 | None. The empty lists pass, and the null driver fails | Mechanical |
| 3 | Add the Go core: JSON, canonical form, kinds, checks, put and contexts | `backend/core/{jsontext,canon,kinds,check,envelope,ops,memstore}/`, `backend/core/write/put.go`, `backend/core/write/snapshot.go`, `backend/core/read/context.go`, `backend/core/read/get.go`, empty `Register` stubs in `read`, `text`, `transfer`, `write` | 2 | 0 | `kinds/`, `identities/`, `statuses/`, `reserved/` on `memstore` | Complex coding |
| 4 | Add the PostgreSQL storage and the schema | `backend/pg/`, `backend/cmd/aavanam-conform/`, `backend/conformance/passing.txt` | 3 | 0 | The vectors of PR 3, on PostgreSQL | Complex coding |
| 5 | Add the publication, the activation gate and the Producer section | `backend/core/write/` (except the files of PR 3) | 4 | 0 | `publication/`, `producer/`, writer vectors of `regressions/` | Complex coding |
| 6 | Add the read operations and the text search | `backend/core/read/` (except the files of PR 3), `backend/core/text/` | 3 | 0 | `reads/`, `lookup/`, `discovery/`, `lifecycle/`, `rendering/`, `impact/`, `coverage/`, `relations/`, `contexts/`, `bindings/`, on `memstore` | Complex coding |
| 7 | Add export, import and the input locks | `backend/core/transfer/` | 6 | 0 | `locks/`, `redistribution/`, `filters/`, `devices/`, `acceptance/`, `scenarios/`, `admitted/`, on `memstore` | Complex coding |
| 8 | Add the network read binding and pass all vectors on PostgreSQL | `backend/api/`, `backend/cmd/aavanam-server/`, `backend/conformance/passing.txt` | 5, 7 | 0 | All 631 on PostgreSQL, reads over HTTP | Well-specified build with clear tests |
| 9 | Add the budget bench | `backend/bench/` | 8 | 0 | None. The `S0` budget job passes | Well-specified build with clear tests |
| 10 | Add the device store in SQLite with FTS5 | `reference/device/`, `reference/cmd/aavanam-device/` | 3 | 0 | The `device` and `output` steps, with the Python writer, after PR 7 | Complex coding |
| 11 | Add the device store tests | `reference/devicetest/`, `reference/conformance/passing.txt` | 10 | 0 | The `device` and `output` steps. The Evidence test, the layout test and the update test | Well-specified build with clear tests |
| 12 | State which implementation to use for what | `README.md`, `docs/STATUS.md`, `tools/check_project.py` (both lists have all vectors) | 8, 11 | 0 | All, on the reference, the backend and the device store | Mechanical |

File split for two writers and the device harness. PR 3 is the one point of synchronization. It creates each package and the storage interface, so the lanes have no shared file after it:

- Lane A: PR 3, PR 4, PR 5, PR 8 and PR 9. Files: `backend/core/{jsontext,canon,kinds,check,envelope,ops,memstore,write}/`, `backend/pg/`, `backend/api/`, `backend/cmd/`, `backend/bench/`, `backend/conformance/`.
- Lane B: PR 6 and PR 7. Files: `backend/core/read/` (except the two files of PR 3), `backend/core/text/`, `backend/core/transfer/`. Lane B runs the vectors on `memstore` with `aavanam-conform --storage mem`, and gives the results in its PR bodies. Lane A adds them to `passing.txt` in PR 8.
- Lane C (a different harness): PR 10 and PR 11. Files: `reference/` only. PR 11 has only test files and `reference/conformance/passing.txt`.

Completion criteria for each PR: `make check` passes. The Go jobs of PR 2 pass with the `passing.txt` of the PR. Each new clause has a vector with a pair. Each refusal vector in PR 1 fails on the reference with its planted fault, then passes. PR 8 and PR 12 close the criteria of #47. PR 1, PR 10 and PR 11 close the criteria of #65.
