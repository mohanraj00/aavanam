# Status

This page describes release 0.6.0. Version 0.6.0 is the current version. The specification has 24 contract documents in the `normative` state. No topic is in the `draft` state (#146). The reference implementation in `src/`, the backend in `backend/` and the device store in `reference/` each pass all conformance vectors. The null implementation fails, as it must.

## Implementations

Each implementation passes all the conformance vectors. The count comes from the vector files. A Go implementation passes each vector of its lists, and its lists have each vector. `conformance/pending-go.txt` names the vectors that a Go implementation does not pass yet. It has no vector line now (#132). `make check` fails if a vector is in no list of an implementation and not in the pending file, or if a list holds a vector twice.

| Implementation | Result | Command |
|---|---|---|
| Reference, Python over SQLite | All vectors | `make check` |
| Backend, storage in memory | All vectors, in two lists | `make check-backend-mem` |
| Backend, PostgreSQL 17, reads over HTTP | All vectors | `make check-backend-pg` (needs `AAVANAM_PG_DSN`) |
| Device store, SQLite with FTS5 | All vectors | `make check-device` |

The lists are `backend/conformance/passing.txt`, `backend/conformance/passing-mem-core.txt`, `backend/conformance/passing-mem-read.txt` and `reference/conformance/passing.txt`. The vectors have no measured size or time numbers. The budget bench gives these numbers for one machine. [docs/bench.md](bench.md) gives the commands, the CI rule and the first run.

## Done

Release 0.6.0 has the base profile at minor version 14, the producer section (document 19), the device store and the read binding (documents 21 and 22), the layer (document 23) and the retrieval index (document 24). [CHANGELOG.md](../CHANGELOG.md) gives the contents and the known limits of each release. [docs/operations/](operations/README.md) gives one page for each operation.

## Next

- Integrations with producers and consumers. An implementation that integrates with a host application uses the tech spec of that application (constitution principle 9).

GitHub issues own the current work. The counts in [README.md](../README.md) come from the checks.
