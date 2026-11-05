# Typed data profiles

State: draft. Prefix: `DAT`. Profile: `aavanam/base/v1`.

## Scope

This document defines the typed data resource profiles: keys, types, units, decimals, missing states, ranges and ordered lists. It does not define unit conversion or arithmetic.

## Topics

| Topic | Subject |
|---|---|
| `TBL` | Table and record-map profiles |
| `DEC` | Canonical decimal strings and precision fields |
| `UNT` | Units without implicit conversion |
| `MIS` | Missing and not-applicable states |
| `RNG` | Ranges, overlap and ordered first-match |
| `PRF` | Profiles that the store does not support |

## Clauses

The topics `DEC`, `UNT`, `MIS` and `RNG` have no clauses yet. Topic `TBL` has only the base table profile, which `query_data` reads (document 09, topic `DAT`).

### Table and record-map profiles

**DAT-TBL-1.** The base profile defines the data profile `aavanam/table/v1`. Each store MUST support it. The payload is the UTF-8 encoding of a JSON object with the fields `columns`, `key` and `rows`, and no other field:

1. `columns` is a list of one or more strings. Each string is a column name. No name occurs two times.
2. `key` is a list of one or more strings. Each string is in `columns`. No string occurs two times.
3. `rows` is a list of rows. Each row is a list with one value for each column, in the order of `columns`. A value is an I-JSON value.

The whole payload MUST be I-JSON (RFC 7493), and no JSON object in it has a key more than one time. No two rows have equal values in all the columns of `key`.

**DAT-TBL-2.** The table order is the order of `rows`. The store compares two values by their canonical forms (IDN-CAN-1). Thus `1` and `1.0` are equal, and `1` and `"1"` are not equal. The store does not convert a value. The store does not read the `schema` of the data resource under this profile. The schema stays opaque.

**DAT-TBL-3.** The `put` checks of STS-MAL-3 do not include the table form. An operation that reads the payload under `aavanam/table/v1` checks the form of DAT-TBL-1. If the payload does not agree with it, the affected item has status `failed` and reason `payload_form`. The store MUST NOT read a part of such a payload.

### Profiles that the store does not support

**DAT-PRF-1.** If the store does not support the `schema_profile` of a `schema` or the `data_profile` of a `data-resource`, the store MUST keep the descriptor and its payload as opaque data. The `put` checks of STS-MAL-3 apply, and the store does not parse the payload (KND-OPQ-1).

**DAT-PRF-2.** If an operation must interpret a payload under a profile that the store does not support, the affected item MUST have status `unsupported` and reason `unsupported_profile`. If the operation has no items, the whole operation has this outcome (STS-ENV-3 rule 1). Otherwise STS-ENV-3 gives the envelope status. The store MUST NOT read the payload under a different profile, and MUST NOT return a default value.

**DAT-PRF-3.** The data profiles and the schema profiles whose names start with `aavanam/unsupported/` are reserved for conformance tests. A store MUST NOT support them. Thus DAT-PRF-1 and DAT-PRF-2 apply to each of them.
