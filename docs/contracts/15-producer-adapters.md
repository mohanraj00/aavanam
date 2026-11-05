# Producer adapters

State: draft. Prefix: `ADP`. Profile: `aavanam/base/v1`.

## Scope

This document defines the producer adapter contract: the records that an adapter writes, native identities, the selector profiles that the adapter maps, and the response for other selectors. It gives one neutral example.

A producer adapter is an application component. It reads the output of one producer and writes descriptors to the store. The store does not supply, call or run an adapter, and it does not read producer output. A mapping for a named producer is not in the scope of this version.

## Topics

| Topic | Subject |
|---|---|
| `CTR` | Adapter contract |
| `SEL` | Selector profiles and native identity mapping |
| `EXM` | A neutral example |

## Clauses

### Adapter contract

**ADP-CTR-1.** An adapter writes its records only with `put` or `import`. Each record is a descriptor of a kind of the base profile or of a supported extension profile (KND-REG-4). The store checks each descriptor as for each other write. An adapter has no other write path, and the store gives it no other permission.

**ADP-CTR-2.** An adapter declares, in its own documentation, these values:

1. The producer name, a string. The adapter writes it in `producer` of each `reading` and `receipt`, and in `origin` of each `evidence` and `check`.
2. The kinds that it writes.
3. Each `native_scheme` that it writes (ADP-SEL-1).
4. Each selector profile that it writes (ADP-SEL-2).

The store compares these values only as strings (BND-RUL-3). A declaration does not show that the producer is correct.

**ADP-CTR-3.** An adapter MUST NOT add a field that the schema of a kind does not have (KND-SCH-2). It puts producer data that has no field in an opaque payload or in `ext` (KND-OPQ-3). It MUST NOT put a role, a runtime profile or a dependency in a payload (BND-RUL-4).

**ADP-CTR-4.** An adapter writes an external result as a `receipt` with an opaque payload (KND-OPQ-2). It writes a check result as a `check`, with the outcome of the producer as a string. The store does not read either value (PUB-CND-5).

**ADP-CTR-5.** If the producer states the coverage of its output, the adapter writes an observation of type `aavanam/coverage/v1` (OBS-COV-1). It writes each omission that the producer states. If the producer does not state a denominator, `total` is null. The adapter MUST NOT write a `total` or a `covered` value that the producer did not state.

### Selector profiles and native identities

**ADP-SEL-1.** If the producer gives an identity to a node of its output, the adapter writes a `native-mapping` with its `native_scheme`, the identity as `native_id`, and the subject that the node became (IDN-NAT-1). `checked` declares the checks of the producer as opaque JSON (IDN-NAT-2). In one snapshot, one native identity has one subject (IDN-NAT-3).

**ADP-SEL-2.** If the producer gives a location in a payload, the adapter writes a selector (REL-SEL-1). It writes the selector profile `aavanam/bytes/v1` only if the location is an exact byte range of that payload (REL-SEL-3). It MUST NOT change a different location into an approximate byte range.

**ADP-SEL-3.** If a location is not an exact byte range, the adapter writes a selector with a profile that the producer or the application names. If the store does not support that profile, it keeps the selector as opaque data, and an operation that must apply it gives `unsupported` with reason `unsupported_selector` (REL-SEL-2). This is the correct response. The adapter MUST NOT replace the selector to prevent it.

### A neutral example

**ADP-EXM-1.** This example is informative. The producer `example/page-reader` reads one text source and gives one reading with three pages. It cannot read page 3. The adapter writes these records:

| Record | Kind | Values |
|---|---|---|
| The source | `source` | The bytes of the text |
| The reading | `reading` | `source` the source, `producer` `example/page-reader`, `coverage_profile` `aavanam/coverage/v1` |
| A page node | `native-mapping` | `subject` the reading, `native_scheme` `example/page-reader/pages`, `native_id` `p-2` |
| The coverage | `observation` | `subjects` the reading, `observation_type` `aavanam/coverage/v1`, `values` with `unit` `pages`, `covered` 2, `total` 3 and one omission with `reason` `unreadable page` |
| A page location | `evidence` | `subject_selector` or `support_selector` with the profile `example/page-reader/page`. The store keeps it opaque (ADP-SEL-3) |

A `render` of a root in a context with this coverage observation gives the gap `omissions` (RND-SCP-4).
