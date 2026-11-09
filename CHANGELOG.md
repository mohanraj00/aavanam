# Changelog

## 0.1.0

- First version of the reference store over SQLite, with the draft specification at `aavanam/base/v1` minor version 3.
- Store structured data, opaque artifacts and evidence in immutable snapshots. Use exact references to read a context or record declared inputs in a lock.
- Export a snapshot with its exact dependency closure and import it into a read-only device store.
- Keep admitted facts in a serving collection. A separate evidence collection resolves citations to exact NFC text spans. The synthetic invoice records a count and a code, with one citation for each field.
- The reference store passes 400 conformance vectors. The null adapter fails 399 and exits with 1. The seam test with the released producer is pending.

See [0.1 compatibility](README.md#01-compatibility) for the scope of this version.
