# Fixtures

A fixture is a neutral synthetic collection. Scenario vectors load a fixture into the store. The expected outcomes of a scenario come from the spec and from the fixture definition, never from an implementation run.

The two consumer fixtures are different on purpose:

| | `equipment-handbook` | `product-specification` |
|---|---|---|
| Collection | `sample-equipment-handbook` | `sample-product-specification` |
| Subject | A fictional bench grinder | A fictional insulated flask |
| Binding roles | `checklist`, `intervals`, `torque` | `acceptance`, `requirements`, `dimensions` |
| Runtime declaration | `sample/runtime/script-a/v1` | `sample/runtime/template-b/v1` |
| Data shapes | Sparse interval rows; one row for each fastener | Min and max limits; nominal and tolerance |
| Units | h, mm, N.m (SI only) | ml, mm, g, degC (mixed) |
| Query vocabulary | service interval, torque, wheel guard | capacity, heat retention, tolerance |
| Records | Two evidence records | Two evidence records and one overlay |

The artifacts (`checklist.txt`, `acceptance.txt`) and the runtime declarations are opaque. The store does not read them (KND-OPQ-2). They stand for application components that the store does not implement.

The `admitted-invoice` fixture has two admitted facts and two citation records. Its original text has a decomposed accent. The reading has the composed accent, and both have a four-byte character before the cited spans. The `admitted-export/invoice.json` file gives the documented fields of the proposed producer export in a neutral projection. Its receipt digests are synthetic. The `admitted-kinds` fixture is a delivery note for the export extension (ADP-FXT-1 to ADP-FXT-7). It has a number with a unit, a date, a record with three members and a passage with an opaque page locator. `admitted-export/kinds.json` is its synthetic export, and `admitted-export/kinds-declaration.json` is its reader declaration. Their digests use neutral prefixes. The `nfc-comparison` fixture gives two readings for the selector checks. The `site-handbook` fixture has a base collection with three units and two layers (document 23). The layer `layer-north` replaces the unit `pump`, adds the units `filter` and `gauge`, and has an overlay, an evidence record and an observation about base objects. The layer `layer-south` adds the unit `fan`. Each payload is an inline text. The `part-notes` fixture has two snapshots of one collection and four retrieval indexes (document 24). The first snapshot has four units with short notes, and overlays that give an area to three of them. The second snapshot has a new revision of the unit `pump` and the unit `fan`. One unit binds a second note, and one unit binds a payload that is not text. Three indexes have the method `aavanam/hashed-terms/v1`, and one index has an external method name.

## Fixture file

Each folder has a `fixture.json`:

| Field | Value |
|---|---|
| `format` | `aavanam-fixture/v1` |
| `collection` | The collection of the root snapshot. Each fixture has a different collection |
| `root` | The handle of the snapshot descriptor |
| `payloads` | Named payloads. Each one has `media_type` and `file`, a file in the same folder |
| `descriptors` | A list of entries with `as` (a handle) and `value` (a descriptor), in put order |

A descriptor value can contain these handles:

| Handle | Value |
|---|---|
| `{"$ref": "h"}` | The reference of the descriptor with `"as": "h"`. It must come earlier in the list |
| `{"$payload": "p"}` | The payload object of the bytes of the file of `p` |
| `{"$ranges": "p", "text": "t"}` | The `ranges` of the text `t` in the file of `p`. The text must occur one time in that file |
| `{"$text": "t", "media_type": "m"}` | The payload object of the UTF-8 bytes of `t` |

The loader computes the ranges, digests and sizes. A fixture file does not contain them.

`check_spec.py` checks each fixture: the fields above, the kind and the fields of each descriptor against the kind table (KND-SCH-1 to KND-SCH-6), the target kind of each reference (KND-REF-3), each handle, and each file. The text of an excerpt must come from the payload of its parent reading (BUN-EXC-3). Each string of `fixture.json`, the payload declarations included, must be I-JSON: no lone surrogates and no Unicode noncharacters. A set must not have two references to the same revision (KND-SCH-5). The check compares the canonical forms (KND-SCH-3), with each payload handle replaced by its payload object and each `$ranges` handle by its byte ranges.

`check_spec.py` does not check selectors or the supersession rules (REL-RET). Thus a fixture MUST NOT use a selector field or the kind `supersession`. A scenario vector can add them in its own steps, where the spec gives the expected outcome. Each reference in a fixture MUST be a `$ref` handle, not a literal revision string. Document 01 does not yet define revisions, so the check cannot compare a literal revision with a handle. Each file in a fixture folder must be a declared payload, and each payload must be used.

## Release stores

A release store is in `fixtures/releases/<release>/`. It is the data that one release of the implementations wrote from one definition. The vectors of document 25 open it with a later release ([conformance/README.md](../conformance/README.md), "Release stores").

| File | Content |
|---|---|
| `release.json` | The definition: `format` (`aavanam-release-store/v1`), `release`, `commit` (the commit of the release tag), `title`, `payloads`, `steps`, and the optional `acceptance`, `gates` and `sections` of a vector |
| `reference/writer.sqlite`, `reference/device.sqlite`, `reference/output.sqlite` | The store files that the reference store of `src/` wrote, one for each store of a vector |
| `backend/postgresql.sql` | A dump of the PostgreSQL database that the backend wrote: its schema and its rows |
| `device/device.sqlite`, `device/output.sqlite` | The two files of document 21 that the device store of `reference/` wrote |
| `reference/contexts.json`, `backend/contexts.json`, `device/contexts.json` | The identities of the read contexts that the release opened, for each store, by the `as_context` handles of the definition |
| `bundles/<handle>.json` | Each bundle that the release exported, by the `as_bundle` handle of the definition |

A step of the definition has `operation`, `request` and `expect`, and it can have `store`, `as_ref`, `as_context` and `as_bundle`. Its operation is `put`, `create_snapshot`, `create_candidate`, `publish`, `remove`, `open_context`, `export` or `import`, and it expects `ok`. An `open_context` step has only `roots`, and it has `as_context`. `check_spec.py` checks these rules and the bundle files.

The release store `0.6.0` has two collections. The collection `sample-manual` has one published snapshot with one unit, and a second snapshot with two units. The collection `sample-evidence` is in the Evidence section, and the release removed one excerpt of it. The device store has an import of the first snapshot. The bundle `x1` has the first snapshot, and `x2` is the delta bundle of the second snapshot. The release opened one read context on the device store and three on the writer store.

`tools/make_release_store.py` makes the stored forms from the definition. It runs the conformance runner, the reference store and the two Go drivers of a checkout at the release tag, so that release writes the data. The runner of the release checks the result of each step. The tool needs `go`, a PostgreSQL database that only the run uses, and the dump program of PostgreSQL:

```sh
git worktree add --detach <folder> v0.6.0
python3 -B tools/make_release_store.py --release 0.6.0 --source <folder> \
    --pg-dsn 'postgres://<user>:<password>@127.0.0.1:5432/<database>?sslmode=disable' \
    --pg-dump 'pg_dump --dbname=<database>'
```

The tool changes the data of the release in two points only. It moves the content of the write-ahead log of each device file into the file, because the driver of the release does not close its files. It removes the lines of the dump that are commands of the client program, and the two comment lines that name the system of the dump. The dump is from PostgreSQL 17.

A new run gives different read context identities and different file bytes, because a context identity is a random value. Thus a new run replaces each stored form together. Do not edit a stored form by hand. A change of the definition needs a new run at the release tag.

## Review

Each fixture is synthetic and neutral. It names no real product, brand, customer or person. Review each new fixture for this before its first commit (constitution).
