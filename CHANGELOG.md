# Changelog

## Unreleased

- Report the license and the redistribution status of each export item (BUN-RED-1 to BUN-RED-6). An export for a target withholds the payload bytes that the target does not permit, and keeps each descriptor. The base profile is at minor version 6.
- Add the operation `read` of document 20 (CRD-READ-1 to CRD-SRV-5). It gives the descriptor and the exact bytes of one member of a read context, with an optional selector, and the new selector profile `aavanam/row/v1`. The base profile is at minor version 7.
- Add the operation `search_text` of document 20 (CRD-TXT-1 to CRD-LOC-1). It gives the byte locations of a word sequence in the text artifacts of a read context, in a stable order, with an ASCII case rule and a byte word rule. It never gives Evidence material. A vector payload can also be base64 bytes. The base profile is at minor version 8.

## 0.1.0

- First version of the reference store over SQLite, with the draft specification at `aavanam/base/v1` minor version 3.
- Store structured data, opaque artifacts and evidence in immutable snapshots. Use exact references to read a context or record declared inputs in a lock.
- Export a snapshot with its exact dependency closure and import it into a read-only device store.
- Keep admitted facts in a serving collection. A separate evidence collection resolves citations to exact NFC text spans. The synthetic invoice records a count and a code, with one citation for each field.
- The reference store passes 400 conformance vectors. The null adapter fails 399 and exits with 1. The seam test with the released producer is pending.

See [0.1 compatibility](README.md#01-compatibility) for the scope of this version.
