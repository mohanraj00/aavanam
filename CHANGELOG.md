# Changelog

## Unreleased

- Add the half-open range match and the interval overlap match to `query_data` (DAT-DEC-1 to DAT-RNG-6, QRY-DAT-5, #67). A request declares the bound columns and their type, `decimal` or `date`, in the field `ranges`. A missing value gives `missing_value`, and a value of a different type gives `value_type`. The store does not convert a number or a unit. The base profile is at minor version 9.
- Accept an import of a bundle with absent withheld bytes (BUN-IMP-9 to BUN-IMP-12, BUN-RED-7, #58). The result was `failed` with `bundle_incomplete`, and is now `ok`: the store keeps each descriptor without the bytes and defers the checks. Each read checks only the descriptor that uses the bytes, with the deferred records and bytes of both stores for a local output store. This includes bytes that the local output store had before the device import. A failed or incomplete check makes the bytes absent for that descriptor only. An export withholds a shared digest if any descriptor in its requested closure fails a check or still needs absent bytes. A descriptor outside that closure does not affect the export. An export withholds absent bytes, keeps each descriptor and reports each affected item with `withheld` `true`. The base profile is at minor version 10.

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
