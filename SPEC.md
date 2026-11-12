# Aavanam specification

Status: draft. Documents 01 to 20 are in the `draft` state. No clause is normative yet.

This file is the index. The normative text is in `docs/contracts/`. The [constitution](CONSTITUTION.md) has authority over this specification. This specification has authority over the implementation.

## Contract documents

| Prefix | Document | Scope |
|---|---|---|
| IDN | [01 Identities and native mappings](docs/contracts/01-identities-and-native-mappings.md) | Logical IDs, payload and descriptor digests, canonical encoding, native producer IDs |
| KND | [02 Kinds and typed references](docs/contracts/02-kinds-and-typed-references.md) | Kind registry, schemas, typed reference fields, derived dependencies |
| STS | [03 Statuses and reasons](docs/contracts/03-statuses-and-reasons.md) | Closed status set, reason codes, result envelopes |
| DAT | [04 Typed data profiles](docs/contracts/04-typed-data-profiles.md) | Table and record profiles, decimals, units, missing states, ordering |
| BND | [05 Bindings and units](docs/contracts/05-bindings-and-units.md) | BindingSets, RelationSets, OverlaySets, knowledge units, opaque rule companions |
| REL | [06 Evidence and relations](docs/contracts/06-evidence-and-relations.md) | Evidence links, selectors, retraction, supersession, conflicts |
| OBS | [07 Observations and coverage](docs/contracts/07-observations-and-coverage.md) | Coverage, omissions, freshness, observation scopes |
| CTX | [08 Views, contexts and capabilities](docs/contracts/08-views-contexts-and-capabilities.md) | Views, read contexts, projections, composition, skew |
| QRY | [09 Lookup and discovery](docs/contracts/09-lookup-and-discovery.md) | Exact get, data queries, lexical and semantic search, context bundles |
| LCK | [10 Consumer input locks](docs/contracts/10-consumer-input-locks.md) | Execution and build input locks, declared completeness |
| BUN | [11 Bundles and closure](docs/contracts/11-bundles-and-closure.md) | Capability closure, export, read-only import, excerpts |
| CHG | [12 Change and impact](docs/contracts/12-change-and-impact.md) | Context comparison, selector-scoped change, declared dependents |
| PUB | [13 Publication](docs/contracts/13-publication.md) | Snapshots, candidates, acceptance receipts, head updates |
| RND | [14 Rendering](docs/contracts/14-rendering.md) | Human-readable documentation from a context |
| ADP | [15 Producer adapters](docs/contracts/15-producer-adapters.md) | Producer adapter contract, selector kinds, native identity mapping |
| RSV | [16 Reserved profiles](docs/contracts/16-reserved-profiles.md) | Features in the spec but not built, with their unsupported responses |
| LIF | [17 Lifecycle](docs/contracts/17-lifecycle.md) | Items across snapshots, changes between two snapshots, history of a logical ID |
| DSC | [18 Discovery](docs/contracts/18-discovery.md) | Exact item listing in a read context by kind and logical ID |
| PRD | [19 Producer storage](docs/contracts/19-producer-storage.md) | The producer section: opaque objects, logs that only grow, removal records |
| CRD | [20 Content reads and text search](docs/contracts/20-content-reads-and-text-search.md) | The content read of a member with a selector |

The conformance vector format and the runner are in [conformance/](conformance/README.md). Vectors cite the clause IDs in these documents.

## Clause IDs

Each normative statement is one clause. A clause starts a paragraph with its ID in bold, for example `**REL-RET-1.**`.

- An ID has the form `PREFIX-TOPIC-N`.
- `PREFIX` is the three-letter prefix of the document in the table above.
- `TOPIC` is two to four uppercase letters. Each document lists its topics.
- `N` is a positive integer. Numbers increase in a topic.
- An ID is never used again. If a clause is withdrawn, keep its ID and write `(withdrawn)` after it, with the reason.
- If the meaning of a clause changes, withdraw it and write a new clause.
- The two rules above apply when a document is `normative`. In a `draft` document, a clause can change in place. The PR that changes it names the clause and the affected vectors.

`tools/check_spec.py` checks the prefixes, the topics and the uniqueness of IDs. It also checks the kind registry tables in document 02, the status, reason and operation tables in document 03, the reserved profile table in document 16, and each conformance vector against these tables.

## Keywords

The key words MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD NOT, RECOMMENDED, MAY and OPTIONAL are used as in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174), only when they are in capitals. Text without these keywords is descriptive.

## Profiles and versions

A profile is a named set of clauses and kinds, with a version. The base profile is `aavanam/base/v1`. Each clause belongs to the base profile unless its document names a different profile.

The base profile is at minor version 8. Minor version 1 adds the kind `activation` to minor version 0 (KND-REG-5). Minor version 2 adds storage sections (PUB-SEC-1) and the reason `section_violation`. Minor version 3 adds the NFC text selector (ADP-SEL-4) and the admitted-facts adapter profile (ADP-FACT-1). Minor version 4 adds the lifecycle operations of document 17 and the discovery operation of document 18. Minor version 5 adds the producer section (PRD-SEC-1), its operations and its reasons. Minor version 6 adds the redistribution observation type (BUN-RED-1), the export field `permit` and the bundle field `withheld` (BUN-RED-5). Minor version 7 adds the operation `read` and the selector profile `aavanam/row/v1` of document 20. Minor version 8 adds the operation `search_text` and the capability `aavanam/text/v1` of document 20. A minor version does not change the profile identity. Thus a descriptor of each minor version has `descriptor_profile` `aavanam/base/v1`.

- A compatible addition to a profile increments the minor version in the profile document.
- An incompatible change makes a new major version, for example `aavanam/base/v2`. Old objects keep their old profile identity. A transformation makes new objects.
- A reserved profile is listed in document 16. Operations that need it return `unsupported` until it is specified.

Extensions use the namespace `ext:<owner>/<name>/v<N>`. The owner is a DNS name or a GitHub owner. An extension MAY add kinds, fields or reason codes. It MUST NOT change the meaning of a base clause. An application that is not aware of an extension keeps the extension data as opaque content.

## Document states

Each contract document has one state: `stub`, `draft` or `normative`.

- `stub`: scope and topics only. No clauses.
- `draft`: clauses for review. Vectors can cite them.
- `normative`: clauses that the maintainers approved. A change needs a PR that names the affected vectors.
