# Typed data profiles

State: draft. Prefix: `DAT`. Profile: `aavanam/base/v1`.

## Scope

This document defines the typed data resource profiles: keys, types, units, decimals, missing states, ranges and ordered lists. It does not define unit conversion or arithmetic. It defines no not-applicable state.

## Topics

| Topic | Subject |
|---|---|
| `TBL` | Table and record-map profiles |
| `DEC` | Decimal strings and their order |
| `DAY` | Calendar date strings and their order |
| `UNT` | Units without implicit conversion |
| `MIS` | Missing values |
| `RNG` | Ranges, overlap and ordered first-match |
| `PRF` | Profiles that the store does not support |

## Clauses

Topic `TBL` has only the base table profile, which `query_data` reads (document 09, topic `DAT`). The topics `DEC`, `DAY`, `UNT`, `MIS` and `RNG` define the range match and the interval match. `query_data` applies them to the columns that a request declares (QRY-DAT-5).

### Table and record-map profiles

**DAT-TBL-1.** The base profile defines the data profile `aavanam/table/v1`. Each store MUST support it. The payload is the UTF-8 encoding of a JSON object with the fields `columns`, `key` and `rows`, and no other field:

1. `columns` is a list of one or more strings. Each string is a column name. No name occurs two times.
2. `key` is a list of one or more strings. Each string is in `columns`. No string occurs two times.
3. `rows` is a list of rows. Each row is a list with one value for each column, in the order of `columns`. A value is an I-JSON value.

The whole payload MUST be I-JSON (RFC 7493), and no JSON object in it has a key more than one time. No two rows have equal values in all the columns of `key`.

**DAT-TBL-2.** The table order is the order of `rows`. The store compares two values by their canonical forms (IDN-CAN-1). Thus `1` and `1.0` are equal, and `1` and `"1"` are not equal. The store does not convert a value. The store does not read the `schema` of the data resource under this profile. The schema stays opaque.

**DAT-TBL-3.** The `put` checks of STS-MAL-3 do not include the table form. An operation that reads the payload under `aavanam/table/v1` checks the form of DAT-TBL-1. If the payload does not agree with it, the affected item has status `failed` and reason `payload_form`. The store MUST NOT read a part of such a payload.

### Declared types

A table of `aavanam/table/v1` does not give the type of a column, and its schema stays opaque (DAT-TBL-2). Thus a request declares the type of each value that a range clause compares (DAT-RNG-1). The declared types are `decimal` and `date`, and no other type.

**DAT-DEC-1.** A value of the declared type `decimal` is a JSON string of 1 to 64 characters with this form, and no other character:

1. an optional `-`;
2. the digit `0`, or a digit of `1` to `9` and then zero or more digits;
3. optionally, a `.` and then one or more digits.

Thus `0`, `-12`, `100.50` and `0.001` are decimal values. The strings `+1`, `01`, `1.`, `.5`, `1e3` and the empty string are not decimal values. A JSON number is not a decimal value, because a JSON number can have a binary rounding error. The store MUST NOT convert a JSON number into a decimal value.

**DAT-DEC-2.** The store compares two decimal values by their exact numeric values. It MUST NOT round a value, and it MUST NOT compare through a binary floating-point number. Thus `1`, `1.0` and `1.00` are equal in this order, and `-0` is equal to `0`. This order applies only to a range clause. The equality of `where` stays the equality of DAT-TBL-2, so a `where` value `"1.0"` is not equal to the table value `"1"`.

**DAT-DAY-1.** A value of the declared type `date` is a JSON string of the form `YYYY-MM-DD` with exactly 10 characters. `YYYY` is a year from `0001` to `9999`, `MM` is a month and `DD` is a day of that month in the proleptic Gregorian calendar. Thus `2026-02-28` is a date value, and `2026-02-30`, `2026-2-3` and `2026-02-28T00:00:00Z` are not date values. A date value has no time zone and no time of day. The store does not get a date from a clock.

**DAT-DAY-2.** The store compares two date values in calendar order. For two date values, this order is equal to the string order.

### Units

**DAT-UNT-1.** A declared type has no unit. The store MUST NOT convert a value from one unit into a different unit, and it MUST NOT read a unit from a value or from a column name. A range clause compares its values only as values of the declared type.

**DAT-UNT-2.** A table that has values in more than one unit gives the unit of each row as a value in a column. To compare in one unit, a request has an exact `where` clause on that column and a range clause in that unit. The range clause then applies only to the rows that match `where` (QRY-DAT-5). A row with a different unit value does not match `where`, so the store does not compare it. The application owns the meaning of a unit value. The store compares it only for equality (DAT-TBL-2).

### Missing values

**DAT-MIS-1.** A missing value is the JSON value `null` in a column or a field that a range clause compares. A missing value is not a value of a declared type. The store MUST NOT replace it with a default, with zero, with an empty string or with a bound that has no limit. Thus a range with a missing upper bound is not a range that has no upper limit.

**DAT-MIS-2.** If a range clause must compare a missing value, the affected item has status `unresolved` and reason `missing_value`. The item MUST NOT have a result for the other values, because the store cannot tell if the range with the missing value matches. This profile has no value for a bound with no limit. An application that needs one writes a value of the declared type.

### Ranges

**DAT-RNG-1.** A range is a pair of values, `lower` and `upper`, of one declared type. A range clause is a JSON object that compares one range with the values of a request. It has these fields, and no other field:

| Field | Value |
|---|---|
| `lower` | A string. The name of the column or the field that gives the lower bound |
| `upper` | A string. The name of the column or the field that gives the upper bound |
| `type` | `decimal` or `date` |
| `match` | `contains` or `overlaps` |
| `value` | A value of the declared type. Only for `contains`, where it is required |
| `start`, `end` | Two values of the declared type. Only for `overlaps`, where both are required |

A clause with a different field, a missing field, a different `type` or `match`, or a request value that is not a value of the declared type does not agree with this form.

**DAT-RNG-2.** A range is half-open. It contains its lower bound, and it does not contain its upper bound. A clause with `match` `contains` matches a range if `lower` is less than or equal to `value`, and `value` is less than `upper`, in the order of the declared type (DAT-DEC-2, DAT-DAY-2). Thus the range of `100` and `500` contains `100` and `499.99`, and it does not contain `500`.

**DAT-RNG-3.** `start` and `end` are a half-open interval, in the same form as a range. A clause with `match` `overlaps` matches a range if the larger of `lower` and `start` is less than the smaller of `upper` and `end`. Thus the range of `100` and `500` overlaps the interval of `499` and `600`. It does not overlap the interval of `500` and `600`, because the two have no value in common.

**DAT-RNG-4.** A range whose `lower` is equal to or more than its `upper` is an empty range. It contains no value, and it overlaps no interval. An interval whose `start` is equal to or more than its `end` is an empty interval, and it overlaps no range. An empty range and an empty interval are valid. They do not give a status other than `ok`.

**DAT-RNG-5.** If the `lower` or the `upper` of a range is not missing (DAT-MIS-1) and is not a value of the declared type, the affected item has status `incompatible` and reason `value_type`. The store MUST NOT convert the value, and it MUST NOT skip the range. For example, the JSON number `100` under the type `decimal` gives `value_type`.

**DAT-RNG-6.** The ranges of two rows can overlap. Then a clause can match more than one row, and the store gives each of them in the table order (DAT-TBL-2). The store does not select one. A consumer that needs the first match reads with `limit` 1 (QRY-DAT-3).

### Profiles that the store does not support

**DAT-PRF-1.** If the store does not support the `schema_profile` of a `schema` or the `data_profile` of a `data-resource`, the store MUST keep the descriptor and its payload as opaque data. The `put` checks of STS-MAL-3 apply, and the store does not parse the payload (KND-OPQ-1).

**DAT-PRF-2.** If an operation must interpret a payload under a profile that the store does not support, the affected item MUST have status `unsupported` and reason `unsupported_profile`. If the operation has no items, the whole operation has this outcome (STS-ENV-3 rule 1). Otherwise STS-ENV-3 gives the envelope status. The store MUST NOT read the payload under a different profile, and MUST NOT return a default value.

**DAT-PRF-3.** The data profiles and the schema profiles whose names start with `aavanam/unsupported/` are reserved for conformance tests. A store MUST NOT support them. Thus DAT-PRF-1 and DAT-PRF-2 apply to each of them.
