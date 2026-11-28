# Aavanam specification

Status: normative. Documents 01 to 24 are in the `normative` state. No topic is in the `draft` state.

This file is the index. The normative text is in `docs/contracts/`. The [constitution](CONSTITUTION.md) has authority over this specification. This specification has authority over the implementation.

## Contract documents

| Prefix | Document | Scope |
|---|---|---|
| IDN | [01 Identities and native mappings](docs/contracts/01-identities-and-native-mappings.md) | Logical IDs, payload and descriptor digests, canonical encoding, native producer IDs |
| KND | [02 Kinds and typed references](docs/contracts/02-kinds-and-typed-references.md) | Kind registry, schemas, typed reference fields, derived dependencies |
| STS | [03 Statuses and reasons](docs/contracts/03-statuses-and-reasons.md) | Closed status set, reason codes, result envelopes |
| DAT | [04 Typed data profiles](docs/contracts/04-typed-data-profiles.md) | Table and record profiles, decimals, units, missing states, ordering |
| BND | [05 Bindings and units](docs/contracts/05-bindings-and-units.md) | BindingSets, RelationSets, OverlaySets, knowledge units, opaque rule companions |
| REL | [06 Evidence and relations](docs/contracts/06-evidence-and-relations.md) | Evidence links, selectors, retraction, supersession, conflicts, removal from the Evidence section |
| OBS | [07 Observations and coverage](docs/contracts/07-observations-and-coverage.md) | Coverage, omissions, freshness, observation scopes |
| CTX | [08 Views, contexts and capabilities](docs/contracts/08-views-contexts-and-capabilities.md) | Views, read contexts and their life, projections, composition, skew |
| QRY | [09 Lookup and discovery](docs/contracts/09-lookup-and-discovery.md) | Exact get, data queries, lexical and semantic search, context bundles |
| LCK | [10 Consumer input locks](docs/contracts/10-consumer-input-locks.md) | Execution and build input locks, declared completeness |
| BUN | [11 Bundles and closure](docs/contracts/11-bundles-and-closure.md) | Capability closure, export, import, the bundle update of a device store, the delta bundle, the raw bundle form, removal from a device store, excerpts |
| CHG | [12 Change and impact](docs/contracts/12-change-and-impact.md) | Context comparison, selector-scoped change, declared dependents |
| PUB | [13 Publication](docs/contracts/13-publication.md) | Snapshots, candidates, acceptance receipts, head updates |
| RND | [14 Rendering](docs/contracts/14-rendering.md) | Human-readable documentation from a context |
| ADP | [15 Producer adapters](docs/contracts/15-producer-adapters.md) | Producer adapter contract, selector kinds, native identity mapping |
| RSV | [16 Reserved profiles](docs/contracts/16-reserved-profiles.md) | Features in the spec but not built, with their unsupported responses |
| LIF | [17 Lifecycle](docs/contracts/17-lifecycle.md) | Items across snapshots, changes between two snapshots, history of a logical ID, removal of content that no retained root reaches |
| DSC | [18 Discovery](docs/contracts/18-discovery.md) | Exact item listing in a read context by kind and logical ID, and the filter on declared metadata |
| PRD | [19 Producer storage](docs/contracts/19-producer-storage.md) | The producer section: opaque objects, logs that only grow, removal records |
| CRD | [20 Content reads and text search](docs/contracts/20-content-reads-and-text-search.md) | The content read of a member with a selector |
| DEV | [21 Device stores](docs/contracts/21-device-stores.md) | The SQLite layout of a device store and its output file, the layout profile, the FTS5 index |
| NET | [22 Network read binding](docs/contracts/22-network-read-binding.md) | The network binding of the operations that do not write content: transport, limits, version rule |
| LYR | [23 Layers](docs/contracts/23-layers.md) | The layer profile: a layer for one scope on a base collection, precedence, layered read contexts, isolation, the view of a lock |
| IDX | [24 Retrieval indexes](docs/contracts/24-retrieval-indexes.md) | The semantic index profile: an index derived from one snapshot, its method, the search, a stale index, the indexes of a lock |
| MIG | [25 Migration between versions](docs/contracts/25-migration.md) | A store, a device file, a database and a bundle of an earlier release; changes of a stored form; derived data; a different major version |

The conformance vector format and the runner are in [conformance/](conformance/README.md). Vectors cite the clause IDs in these documents.

## Clause IDs

Each normative statement is one clause. A clause starts a paragraph with its ID in bold, for example `**REL-RET-1.**`.

- An ID has the form `PREFIX-TOPIC-N`.
- `PREFIX` is the three-letter prefix of the document in the table above.
- `TOPIC` is two to four uppercase letters. Each document lists its topics.
- `N` is a positive integer. Numbers increase in a topic.
- An ID is never used again. If a clause is withdrawn, keep its ID and write `(withdrawn)` after it, with the reason.
- If the meaning of a clause changes, withdraw it and write a new clause.
- The two rules above apply to each clause of a `normative` document, except a clause of a `draft` topic. A clause of a `draft` document or of a `draft` topic can change in place. The PR that changes it names the clause and the affected vectors.

`tools/check_spec.py` checks the prefixes, the topics and the uniqueness of IDs. It also checks the kind registry tables in document 02, the status, reason and operation tables in document 03, the reserved profile table in document 16, the tables of each profile document, for example document 23, and each conformance vector against these tables.

## Keywords

The key words MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD NOT, RECOMMENDED, MAY and OPTIONAL are used as in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174), only when they are in capitals. Text without these keywords is descriptive.

## Profiles and versions

A profile is a named set of clauses and kinds, with a version. The base profile is `aavanam/base/v1`. Each clause belongs to the base profile unless its document names a different profile.

The base profile is at minor version 14. Minor version 1 adds the kind `activation` to minor version 0 (KND-REG-5). Minor version 2 adds storage sections (PUB-SEC-1) and the reason `section_violation`. Minor version 3 adds the NFC text selector (ADP-SEL-4) and the admitted-facts adapter profile (ADP-FACT-1). Minor version 4 adds the lifecycle operations of document 17 and the discovery operation of document 18. Minor version 5 adds the producer section (PRD-SEC-1), its operations and its reasons. Minor version 6 adds the redistribution observation type (BUN-RED-1), the export field `permit` and the bundle field `withheld` (BUN-RED-5). Minor version 7 adds the operation `read` and the selector profile `aavanam/row/v1` of document 20. Minor version 8 adds the operation `search_text` and the capability `aavanam/text/v1` of document 20. Minor version 9 adds the request field `ranges` of `query_data` (QRY-DAT-5), the range match and the interval match of document 04, and the reasons `missing_value` and `value_type`. Minor version 10 adds the filter of document 18 (DSC-FLT-1), the request field `filter` of `list_items` and of `export`, the bundle field `filter` (BUN-FLT-4) and the reasons `no_match` and `unmatched_dependency`. Minor version 11 adds the import of withheld digests and the deferred checks (BUN-IMP-9 to BUN-IMP-12), and the export rule for absent withheld bytes (BUN-RED-7), of document 11. Minor version 12 permits an `import` on a device store that has objects (BUN-IMP-6), and adds the bundle update (BUN-UPD-1 to BUN-UPD-6), the rule for a device bundle of a Serving context (BUN-CLS-5), the outcome of a validator that does not answer before its deadline (PUB-RCP-4) and the index rule of CRD-TXT-3. Minor version 13 adds the export field `base_context`, the bundle field `base` and the delta bundle (BUN-DLT-1 to BUN-DLT-5), and the raw bundle form `aavanam-bundle-raw/v1` (BUN-RAW-1 to BUN-RAW-5), of document 11. Minor version 14 adds the removal from the Evidence section (REL-RMV-1 to REL-RMV-13), the operations `remove` and `removals`, and the reasons `removed` and `unsupported_role`. It adds the life of a read context (CTX-LIF-1 to CTX-LIF-6), the request field `life` of `open_context`, the item field `removed` of `open_context` and the operation `close_context`. It specifies `gc_collect` (LIF-GC-1 to LIF-GC-7, BUN-RMV-1 to BUN-RMV-3), which document 16 reserved before, and it adds the statuses `partial` and `not_found` to `item_history` (LIF-HST-6). A minor version does not change the profile identity. Thus a descriptor of each minor version has `descriptor_profile` `aavanam/base/v1`.

- A compatible addition to a profile increments the minor version in the profile document.
- An incompatible change makes a new major version, for example `aavanam/base/v2`. Old objects keep their old profile identity. A transformation makes new objects.
- Document 25 gives the rules for data that an earlier release wrote, and for data of a different major version (MIG-VER-1, MIG-MAJ-1).
- Document 21 has the layout profile `aavanam/device-sqlite/v1`, and document 22 has the API profile `aavanam-read/v1`. Document 23 has the layer profile `aavanam/layer/v1`. A store declares each of these profiles separately from the base profile. Their clauses apply only to a store that declares them. The layer profile has its own kind, field and reason tables (LYR-PRF-2, LYR-PRF-3). Thus it does not change the tables or the minor version of the base profile. Document 24 has the retrieval index profile `aavanam/semantic-index/v1`, which document 16 reserved. It has its own kind, field, reason, operation and capability tables (IDX-PRF-2 to IDX-PRF-4), and it does not change the tables or the minor version of the base profile either.
- A reserved profile is listed in document 16. Operations that need it return `unsupported` until it is specified.

Extensions use the namespace `ext:<owner>/<name>/v<N>`. The owner is a DNS name or a GitHub owner. An extension MAY add kinds, fields or reason codes. It MUST NOT change the meaning of a base clause. An application that is not aware of an extension keeps the extension data as opaque content.

## Document states

Each contract document has one state: `stub`, `draft` or `normative`.

- `stub`: scope and topics only. No clauses.
- `draft`: clauses for review. Vectors can cite them.
- `normative`: clauses that the maintainers approved. A change needs a PR that names the affected vectors.

A `normative` document can keep a topic in the `draft` state. The row of that topic in the topic table of the document then ends with `(draft)`, for example ``| `EXE` | Execution input locks (draft) |``. The compatibility statement in [README.md](README.md) lists each `draft` topic.

Each contract document is `normative`. `tools/check_spec.py` refuses a document in a different state. It also refuses a `draft` topic that the compatibility statement does not list, and a listed topic that its document does not mark.
