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

The `admitted-invoice` fixture has two admitted facts and two citation records. Its original text has a decomposed accent. The reading has the composed accent, and both have a four-byte character before the cited spans. The `admitted-export/invoice.json` file gives the documented fields of the proposed producer export in a neutral projection. Its receipt digests are synthetic. The `nfc-comparison` fixture gives two readings for the selector checks.

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

## Review

Each fixture is synthetic and neutral. It names no real product, brand, customer or person. Review each new fixture for this before its first commit (constitution).
