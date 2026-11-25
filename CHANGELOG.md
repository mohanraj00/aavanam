# Changelog

## 0.5.0

- Add the layer (document 23, LYR-DEF-1 to LYR-EXP-3, #44). A layer adds members to a base collection, or replaces members of it, for one scope, for example one tenant. `open_context` gets the field `layer`. A layered read context has the base snapshot and one layer. It has no content of a different layer, and a reference that is not a member gives `unknown_ref`. A lock of a layered context records the base snapshot and the layer in its field `view`. The layer is the profile `aavanam/layer/v1`: a store declares it separately, and the base profile stays at minor version 12. A reference store declares the profile with `Store(profiles=...)`, and passes the 52 vectors of `conformance/vectors/layers/`. The backend and the device store declare the profile and pass these vectors (#132). A Go store declares a profile in its field `Profiles`, and the service has the option `--profile`.
- Add the retrieval index (document 24, IDX-PRF-1 to IDX-LOC-2, #45). The reserved semantic search is now the profile `aavanam/semantic-index/v1`, which a store declares separately. An index is an object of kind `retrieval-index` with its snapshot, its method and the parameters of the method. `semantic_search` gives items with `ref`, `logical_id`, `snapshot`, `index` and `score`, and it permits a `filter`. A lock can record its `indexes`. An index of an earlier snapshot gives `index_stale` in a context of a later snapshot. The method `aavanam/hashed-terms/v1` is deterministic and needs no model: the same snapshot and the same index descriptor give the same items. The profile makes no statement about retrieval quality. A store that does not declare the profile keeps the response `reserved_profile`. The reference store passes the 45 vectors of `conformance/vectors/retrieval/`. The backend and the device store declare the profile and pass these vectors (#132). The network read binding does not have `semantic_search` (NET-SCP-1), so the read listener gives HTTP 404 for it on a store that declares the profile (NET-SCP-3).
- Use the read indexes of the design in the backend (#128). A read context is the closures of its roots. `search_text` reads candidates from a text index, and `read` of a row and `query_data` read through a row cache. Schema change 3 of PostgreSQL adds `closures`, `text_tokens`, `table_heads` and `table_rows`, and removes `context_members`. Schema change 4 adds the index `objects_layers` (#132). The service applies each schema change when it opens a store of version 0.4.0. After that, a service of version 0.4.0 refuses the database. The device store has no layout change. No clause and no vector changes.
- Serve the read binding on a Unix socket (#125). The service option `--read-socket <absolute path>` replaces the TCP read listener, and `--read-socket-mode <octal>` sets the mode of the socket file. The default mode is `0600`. The paths, bodies, limits and envelopes are the same as on TCP. Document 22 does not change.
- Add the budget bench (#105). `backend/bench` and `reference/bench` measure the reads on the memory storage, on PostgreSQL and on the device store, for a synthetic store from a fixed seed. `make check-bench` runs the shape `S0` in CI and fails on a clear miss. `make bench-s1` runs the shape `S1` on PostgreSQL with one client and with 16 clients, and CI does not run it. Each number is for one machine only. The `S1` record with 16 clients for this release is in `docs/bench.md`.
- Add a read-only example page that browses a store through the read API `aavanam-read/v1` (#62). It is in `examples/browse_page/` and uses only the Python standard library. It uses only operations of NET-SCP-1, so it writes no content. The read API has no operation that lists collections, so the page lists the collections of its configuration.
- Each of the three implementations passes all 739 conformance vectors: the Python reference store, the backend (in memory and on PostgreSQL 17) and the device store. The layer and retrieval index vectors run on a store that declares the profile. `conformance/pending-go.txt` has no vector line. The specification has 466 clauses in 24 draft contract documents. The base profile stays at minor version 12. The null adapter passes 1 vector, fails 738 and exits with 1. The Python tests pass: 94 in `conformance/` and 201 in `tests/`. If an optional tool or variable is not available, 30 tests in `tests/` skip. The Go tests of the two modules pass.
- The release has three tags: `v0.5.0`, `backend/v0.5.0` and `reference/v0.5.0`. It has no package upload and no container image.

Known limits:

- An outside Go module can use `reference` only after the tag `backend/v0.5.0` exists. `reference/go.mod` requires `backend v0.5.0`, and its `replace` to `../backend` applies only in this repository.
- The read binding has no access control. Use the loopback address or a Unix socket with mode `0600`, or put your own access control in front of the listener.
- The Unix socket listener checks only the mode bits of the socket file (#137). On macOS, an inheritable ACL on the socket directory can give a different local account access. Do not put an inheritable ACL on the socket directory. Also, a second start of the service on the same paths can remove the socket of a running service when its accept queue is full.
- On a PostgreSQL store of version 0.4.0, the first `search_text` writes the missing rows of the text index. If a `put` or `import` with more than 4 MiB of token bit sets runs at the same time, PostgreSQL can stop one of the two calls as a deadlock, and that call gives `storage_error` (#138). This fault comes from an analysis of the code, and no test reproduces it. Retry the call.
- An update bundle for a device holds the full closure of its roots, and each payload is base64 (#129). Thus a 1% update bundle has about the same size as the first bundle.
- The project publishes no container image of the backend server (#131). Build the server from the tag.
- The retrieval index method `aavanam/hashed-terms/v1` has no measured retrieval quality. `semantic_search` reads each text of the snapshot for each call, in each implementation, and it has no measured time. The network read binding does not have `semantic_search` (NET-SCP-1).
- The pins of the PDF example have dependency hashes for Linux x86_64 and macOS arm64 only. On a different platform, the install stops with a hash error.

## 0.4.0

- Add the production backend, a Go module in `backend/` (#47, #98 to #104). It has the Go core, the PostgreSQL storage and schema, the publication and the activation gate, the read operations and the text search, export, import and the input locks. The conformance driver is `aavanam-conform`. The release tag is `backend/v0.4.0`.
- Add the network read binding `aavanam-read/v1` (document 22, #104). The backend serves the read operations over HTTP. Access control is outside the scope of the binding. The listener uses the loopback address by default.
- Add the device store, a Go module in `reference/` (document 21, #65, #106, #107). It keeps a device store in SQLite, layout `aavanam/device-sqlite/v1`, with an FTS5 text index. The conformance driver is `aavanam-device`. The release tag is `reference/v0.4.0`.
- Add the bundle update, topic UPD (BUN-UPD-1 to BUN-UPD-6, #97). A device store accepts a second import and adds only the new content. A context that was open before the update keeps its members. The base profile is at minor version 12.
- Use the signed activation records of entelos v0.5.0 in the activation seam test and the library example (#91, #93, #94, #95). The stand-in records are removed. The seam test checks each signed record with `entelos-verify`, and maps the typed record fields to the dependencies of the activation.
- Add the design of the production backend and the device bundle, `docs/design/backend.md` (#96), and the guidance on which implementation to use for what (#108).
- Each of the three implementations passes all 642 conformance vectors: the Python reference store, the backend (in memory and on PostgreSQL 17) and the device store. The specification has 409 clauses in 22 draft contract documents. The null adapter passes 1 vector, fails 641 and exits with 1.
- Move to the next release: the budget bench (#105) and the Unix socket listener (#125). This release has no measured size or time numbers.
- The release has three tags: `v0.4.0`, `backend/v0.4.0` and `reference/v0.4.0`. It has no package upload and no container image.

Known limits:

- An outside Go module can use `reference` only after the tag `backend/v0.4.0` exists. `reference/go.mod` requires `backend v0.4.0`, and its `replace` to `../backend` applies only in this repository.
- The read binding has no access control. Run the listener on the loopback address, or put your own access control in front of it.
- The backend and the device store pass the conformance vectors. They have no measured budget numbers.
- The pins of the PDF example have dependency hashes for Linux x86_64 and macOS arm64 only. On a different platform, the install stops with a hash error.

## 0.3.0

- Add the half-open range match and the interval overlap match to `query_data` (DAT-DEC-1 to DAT-RNG-6, QRY-DAT-5, #67). A request declares the bound columns and their type, `decimal` or `date`, in the field `ranges`. A missing value gives `missing_value`, and a value of a different type gives `value_type`. The store does not convert a number or a unit. The base profile is at minor version 9.
- Add the filter on declared metadata to `list_items` and `export` (DSC-FLT-1 to DSC-FLT-6, BUN-FLT-1 to BUN-FLT-5, #5). A filter has exact clauses and half-open interval clauses on the overlay values of each item. The store reads no filter value from an activation. An item with absent, missing or conflicting metadata gives a closed status, not a match. A filtered export has the matched units as its roots, and its bundle records the filter. It gives no bundle if a matched unit has a unit that does not match in its closure. The base profile is at minor version 10.
- Add the seam test of the activation gate (PUB-GAT-3, PUB-ACT-3, PUB-HED-3 to PUB-HED-6, #4). It checks that a valid activation moves the head, a refused activation leaves the store unchanged, and a repeat with the same request key gives the earlier outcome without a second publication. The seam test uses a stand-in activation record until entelos publishes the record format in a tagged release.
- Accept the optional members of the groundgate 0.15 `facts@1` export (ADP-FXT-2, ADP-FXT-4, ADP-FXT-5, ADP-FXT-7, #80, PR 85): a page and box locator on each fact kind, `record`, `replaced`, `passage_sha256` and `completeness`. The adapter keeps the fact and citation members unchanged, and stores `completeness` once on the relation set. The PDF example takes the page from the citation locator.
- Accept an import of a bundle with absent withheld bytes (BUN-IMP-9 to BUN-IMP-12, BUN-RED-7, #58). The result was `failed` with `bundle_incomplete`, and is now `ok`: the store keeps each descriptor without the bytes and defers the checks. Each read checks only the descriptor that uses the bytes, with the deferred records and bytes of both stores for a local output store. This includes bytes that the local output store had before the device import. A failed or incomplete check makes the bytes absent for that descriptor only. An export withholds a shared digest if any descriptor in its requested closure fails a check or still needs absent bytes. A descriptor outside that closure does not affect the export. An export withholds absent bytes, keeps each descriptor and reports each affected item with `withheld` `true`. The base profile is at minor version 11.
- Add the library example (ADP-FXT-1 to ADP-FXT-7, PUB-GAT-3, PUB-ACT-3, DSC-FLT-1 to DSC-FLT-5, DAT-RNG-2, #83). It admits two facts from a synthetic source and moves one update through the activation gate. A publish with no activation is refused. A publish with the stand-in activation record and a build lock moves the head. A filtered read of the head gives one fact for one branch and date.
- The specification has 368 clauses in 20 draft contract documents. The reference store passes 631 conformance vectors. The null adapter fails 630 and exits with 1.

Known limits:

- The activation seam test and the library example use stand-in activation records. Entelos must publish the record format in a tagged release. Then the test and the example can use released records.
- The pins of the PDF example have dependency hashes for Linux x86_64 and macOS arm64 only. On a different platform, the install stops with a hash error.

## 0.2.0

- Add the lifecycle and discovery contracts, documents 17 and 18 (#43). The operation `snapshot_changes` gives the changes between two snapshots. The operation `item_history` gives the history of a logical ID. The operation `list_items` gives the exact item listing of a read context.
- Add the producer section, document 19 (#7). A producer keeps its objects, its logs and a record of each removal in this section, outside each snapshot. The adapter `ChainedLogStore` gives the five operations of the producer store protocol (PRD-PRO-1).
- Report the license and the redistribution status of each export item (BUN-RED-1 to BUN-RED-6). An export for a target withholds the payload bytes that the target does not permit, and keeps each descriptor. The base profile is at minor version 6.
- Add the operation `read` of document 20 (CRD-READ-1 to CRD-SRV-5). It gives the descriptor and the exact bytes of one member of a read context, with an optional selector, and the new selector profile `aavanam/row/v1`. The base profile is at minor version 7.
- Add the operation `search_text` of document 20 (CRD-TXT-1 to CRD-LOC-1). It gives the byte locations of a word sequence in the text artifacts of a read context, in a stable order, with an ASCII case rule and a byte word rule. It never gives Evidence material. A vector payload can also be base64 bytes. The base profile is at minor version 8.
- Keep the calibration data of a producer in the producer section (PRD-CAL-1 to PRD-CAL-5, #61): profile versions, gold set digests, splits logs and calibration events. The store does not change its behavior.
- Add the optional export extension of admitted facts (ADP-FXT-1 to ADP-FXT-7, #66). The function `put_export_facts` reads a `facts@1` export directly, with a reader declaration of the digest forms and the span unit.
- Build a wheel and a source archive in CI from a clean copy of each commit, and run the examples from the installed wheel (#68). The quickstart example reads its quote with the operation `read`.
- Add the PDF example (#71). groundgate reads a synthetic PDF data sheet. The example keeps the admitted facts in two versions of a snapshot, and reads each fact back with its page, its span and its digests.
- Make the stated counts from the spec and the vector folder, and check each stated count in the project check (#70).
- The N1 seam test pins groundgate 0.15.0 (#54, PR 81). CI runs the 46 store vectors of the groundgate wheel through `ChainedLogStore` on the producer section, with the calibration items of PRD-CAL-1.
- The specification has 337 clauses in 20 draft contract documents. The reference store passes 565 conformance vectors. The null adapter fails 564 and exits with 1.

Known limits:

- The admitted-facts adapter refuses the optional members of the groundgate 0.15 `facts@1` export: a locator on a fact that is not a passage, `record`, `replaced`, `passage_sha256` and `completeness` (#80). Thus the PDF example takes the page from the reading layout, not from the export.
- The pins of the PDF example have the `cryptography` and `cffi` hashes for Linux x86_64 and macOS arm64 only. On a different platform, the install stops with a hash error.

## 0.1.0

- First version of the reference store over SQLite, with the draft specification at `aavanam/base/v1` minor version 3.
- Store structured data, opaque artifacts and evidence in immutable snapshots. Use exact references to read a context or record declared inputs in a lock.
- Export a snapshot with its exact dependency closure and import it into a read-only device store.
- Keep admitted facts in a serving collection. A separate evidence collection resolves citations to exact NFC text spans. The synthetic invoice records a count and a code, with one citation for each field.
- The reference store passes 400 conformance vectors. The null adapter fails 399 and exits with 1. The seam test with the released producer is pending.

See [0.1 compatibility](README.md#01-compatibility) for the scope of this version.
