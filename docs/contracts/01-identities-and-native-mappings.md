# Identities and native mappings

State: draft. Prefix: `IDN`. Profile: `aavanam/base/v1`.

## Scope

This document defines logical IDs, payload digests, descriptor revisions and the canonical descriptor encoding. It also defines how the store keeps a producer's native identity with a checked mapping.

## Topics

| Topic | Subject |
|---|---|
| `LOG` | Logical identifiers and namespaces |
| `DIG` | Payload digests and digest schemes |
| `CAN` | Canonical descriptor encoding (RFC 8785) and duplicate-key rejection |
| `NAT` | Native producer identities and checked mappings |

## Clauses

### Logical identifiers

**IDN-LOG-1.** A `logical_id` is the name that a producer gives to the revisions of one object. Its value MUST be a string with one or more characters. If it is an empty string, the store MUST reject the write with status `failed` and reason `field_type`. The store compares two logical IDs as exact strings. It does not change case or Unicode normalization.

**IDN-LOG-2.** A logical ID is not a reference (KND-REF-1). Two descriptors with the same logical ID and different canonical forms are two objects, with two revisions. A `put` of the second descriptor MUST NOT change or remove the first.

**IDN-LOG-3.** Only a head in a read context selects one revision for a logical ID (document 13). The store MUST NOT select a revision for a logical ID by write order, by time or by a comparison of revisions. A namespace is the part of a logical ID that the producer controls. The base profile does not give a namespace syntax.

### Payload digests

**IDN-DIG-1.** The `digest` of a `payload` is `sha256:` and the 64 lowercase hexadecimal digits of the SHA-256 digest of the payload bytes. The `size` is the number of bytes, an integer of 0 or more. The `media_type` is a string. The base profile has only the digest scheme `sha256`. If a `digest` does not have this form, the store MUST reject the write with status `failed` and reason `field_type` (KND-SCH-4).

**IDN-DIG-2.** The store keeps one copy of the bytes of each digest. Two payloads with the same digest are the same bytes. A descriptor revision and a payload digest have the same form, but they identify different things. A payload digest is not a reference (KND-REF-5).

### Canonical encoding and revisions

**IDN-CAN-1.** The canonical form of a descriptor is its normal form encoded with the JSON Canonicalization Scheme (RFC 8785), in UTF-8. The normal form omits each empty set or list of KND-SCH-3 and sorts each set of KND-SCH-5. The encoding sorts the keys of each object, also the keys of a `ref-map` (KND-SCH-6) and of a `json` value. A `json` value keeps its other content.

**IDN-CAN-2.** The revision of a descriptor is `sha256:` and the 64 lowercase hexadecimal digits of the SHA-256 digest of its canonical form. A reference (KND-REF-1) is a revision. If a `put` is `ok`, each item MUST have the field `ref`, the revision of its descriptor.

**IDN-CAN-3.** Two descriptors with the same canonical form are one object. If a `put` is `ok` and has a descriptor whose revision is in the store, its item has the field `ref`, equal to that revision. The store does not change for that descriptor. If the `put` is not `ok`, STS-ENV-5 applies to that descriptor as to each other descriptor. The order of keys, the white space and the order of a set in the request do not change the revision.

**IDN-CAN-4.** The store MUST reject a descriptor that has a duplicate key in an object, at any level, with status `failed` and reason `duplicate_key` (STS-MAL-3 step 1). The encoding of RFC 8785 cannot keep a duplicate key.

### Native producer identities

**IDN-NAT-1.** A `native-mapping` keeps the identity that a producer gave to a subject. `native_scheme` names the identity scheme of the producer. `native_id` is the identity in that scheme. The store keeps both as exact strings. It does not convert a native identity to a revision. Two different schemes are never equal, also if their values have the same digits (constitution principle 6).

**IDN-NAT-2.** `checked` is the declaration of the checks that the producer of the mapping made, as opaque JSON. The store does not run or verify these checks. The store checks the `subject` reference (KND-REF-2, KND-REF-3), as for each other reference.

**IDN-NAT-3.** In one snapshot, one native identity MUST have one subject. If two `native_mappings` of a snapshot have the same `native_scheme` and `native_id` and different subjects, the store MUST reject the snapshot with status `failed` and reason `native_id_conflict` (STS-MAL-3 step 6). The result has one reason for each native identity that has more than one subject. Two mappings with the same native identity and the same subject do not conflict.
