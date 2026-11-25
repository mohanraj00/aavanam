# Status

Version 0.4.0 is the current version. The specification has 24 contract documents in the `draft` state. The reference implementation in `src/` passes all conformance vectors. The backend in `backend/` and the device store in `reference/` pass each vector that is not in `conformance/pending-go.txt`. The null implementation fails, as it must.

## Implementations

The reference implementation passes all the conformance vectors. The count comes from the vector files. A Go implementation passes each vector of its lists. `conformance/pending-go.txt` names the vectors that the Go implementations do not pass yet: the vectors of the layer profile of document 23 and of the retrieval index profile of document 24, which the Go implementations do not declare (#132). `make check` fails if a vector is in no list of an implementation and not in the pending file, or if a list holds a vector twice.

| Implementation | Result | Command |
|---|---|---|
| Reference, Python over SQLite | All vectors | `make check` |
| Backend, storage in memory | All vectors but the pending vectors, in two lists | `make check-backend-mem` |
| Backend, PostgreSQL 17, reads over HTTP | All vectors but the pending vectors | `make check-backend-pg` (needs `AAVANAM_PG_DSN`) |
| Device store, SQLite with FTS5 | All vectors but the pending vectors | `make check-device` |

The lists are `backend/conformance/passing.txt`, `backend/conformance/passing-mem-core.txt`, `backend/conformance/passing-mem-read.txt` and `reference/conformance/passing.txt`. The vectors have no measured size or time numbers. The budget bench gives these numbers for one machine. [docs/bench.md](bench.md) gives the commands, the CI rule and the first run.

## Done

- Specification and conformance: the vector format, the runner, a null implementation and the vectors of each contract document.
- Core: the reference implementation over SQLite, the typed references, the snapshots, the input locks, lookup, rendering and the producer adapter contract.
- Transfer and composition: export, import, device stores and composed contexts, with end-to-end acceptance vectors. An export reports the license and the redistribution status of each item, and withholds the bytes that its target does not permit.
- Activation gate: gated collections, the `activation` kind, the activation checks of `publish` and the `publications` read.
- Lifecycle and discovery: the changes between two snapshots, the history of a logical ID and the exact item listing (documents 17 and 18).
- Filters: the half-open range match and the interval match on declared columns, and the filter on declared metadata for a listing and an export.
- Producer exports: the admitted-facts adapter accepts the optional export members, and the PDF example reads the page from the citation locator (#80, PR 85).
- Device imports: a bundle with absent withheld bytes keeps each descriptor and defers the byte checks until a read (#58).
- Bundle update: a device store accepts a second import and adds only the new content. A context that was open before the update keeps its members (BUN-UPD-1 to BUN-UPD-6, #97). Documents 21 and 22 specify the SQLite layout of a device store and the network read binding. The reference implementation does not declare these two profiles.
- Activation integration: the seam test checks a valid activation, a refused activation and a repeat (#4). The library example moves one update through the gate and reads the head with a metadata filter (#83). Both use the signed activation records of entelos v0.5.0 (#95).
- Layers: a layer adds or replaces members of a base collection for one scope. A layered read context has the base snapshot and one layer, and no content of a different layer. A lock of a layered context records the base snapshot and the layer (document 23, the profile `aavanam/layer/v1`, #44). A store of the reference implementation declares the profile with `Store(profiles=...)`. The runner declares the profile only for the vectors of the profile, so each base vector runs on a store of the base profile only. The reference implementation passes the vectors. The Go implementations do not have layers yet (#132).
- Retrieval indexes: an index is an object that records its snapshot, its method and the parameters of the method. `semantic_search` gives the items of that snapshot that share a term with the query. Each result names its item, its snapshot and its index. A lock can record the indexes that a run used, and an index of an earlier snapshot gives `index_stale` in a context of a later snapshot (document 24, the profile `aavanam/semantic-index/v1`, #45). The one method of the profile, `aavanam/hashed-terms/v1`, is deterministic and needs no model. No measured retrieval quality exists for it. The reference implementation keeps no derived data: it makes the items again from the snapshot. The Go implementations keep the reserved response (#132).

## Next

- Integrations with producers and consumers. An implementation that integrates with a host application uses the tech spec of that application (constitution principle 9).

GitHub issues own the current work. The counts in [README.md](../README.md) come from the checks.
