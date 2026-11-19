# Status

Version 0.4.0 is the current version. The specification has 22 contract documents in the `draft` state. The reference implementation in `src/`, the backend in `backend/` and the device store in `reference/` pass all conformance vectors. The null implementation fails, as it must.

## Implementations

Each implementation passes all the conformance vectors. The count comes from the vector files. `make check` fails if the lists of an implementation miss a vector or hold a vector twice.

| Implementation | Result | Command |
|---|---|---|
| Reference, Python over SQLite | All vectors | `make check` |
| Backend, storage in memory | All vectors, in two lists | `make check-backend-mem` |
| Backend, PostgreSQL 17, reads over HTTP | All vectors | `make check-backend-pg` (needs `AAVANAM_PG_DSN`) |
| Device store, SQLite with FTS5 | All vectors | `make check-device` |

The lists are `backend/conformance/passing.txt`, `backend/conformance/passing-mem-core.txt`, `backend/conformance/passing-mem-read.txt` and `reference/conformance/passing.txt`. The vectors have no measured size or time numbers. The budget bench is not built.

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

## Next

- Integrations with producers and consumers. An implementation that integrates with a host application uses the tech spec of that application (constitution principle 9).

GitHub issues own the current work. The counts in [README.md](../README.md) come from the checks.
