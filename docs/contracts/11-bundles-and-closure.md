# Bundles and closure

State: draft. Prefix: `BUN`. Profile: `aavanam/base/v1`.

## Scope

This document defines capability closure, bundle manifests, export, license and redistribution status, read-only import and excerpt objects. A device corpus is read-only. Local outputs stay separate.

## Topics

| Topic | Subject |
|---|---|
| `CLS` | Capability closure obligations |
| `MAN` | Bundle manifests and identity |
| `EXP` | Export |
| `RED` | License and redistribution status in an export |
| `IMP` | Read-only import and integrity |
| `EXC` | Excerpt objects and parent validation |

## Clauses

### Capability closure

**BUN-CLS-1.** An operation in a read context reads only the members of the context (CTX-OPN-2, CTX-OPN-4) and the payload bytes of their byte closure (KND-DEP-3). Thus no capability of the capability table (CTX-PRJ-1) needs an object or bytes outside the closure of the roots of the context. The capability closure of a set of roots is this closure and its byte closure. A bundle has the capability closure of its roots (BUN-EXP-2). An implementation MUST NOT make an operation read an object or bytes outside the capability closure of the roots of its context.

**BUN-CLS-2.** A record refers to its subject, and the subject does not refer to the record (KND-REF-6). Thus a record about an object is in the capability closure only if it is in the closure of a root, for example as a member of a RelationSet of a unit or of a snapshot. If a consumer on a device store needs a record, the application exports a root that has the record in its closure. The store MUST NOT add a record to a bundle because its subject is in the closure.

**BUN-CLS-3.** A device store has an `ok` import of a bundle (BUN-IMP-3) with the roots R. Each root of R has kind `knowledge-unit` or `snapshot`. A read context on the device store with the roots R and the `capabilities` of a read context on the writer store has the same members (CTX-OPN-2). If a request names only members of the context, the operation gives the same status, reasons and items as in the writer context. A reference outside the closure of R can give a different reason. On the writer store, it gives `not_in_context` if the store has the object. On the device store, it gives `unknown_ref` (CTX-OPN-4). The envelope field `context` has the identity of the device context (CTX-OPN-3). An import does not copy the catalog of the writer store. Thus a name in `heads` on a device store gives `no_head` (CTX-VIW-2).

**BUN-CLS-4.** With the read contexts of BUN-CLS-3, `execution_inputs` and `build_inputs` give the same status, reasons, items and `lock` on the device store as on the writer store. The device store writes the lock to its local output store (BUN-IMP-7). The `requires` of the request names the capabilities that the consumer needs on the device (LCK-CAP-3). If the device context does not have one of them, the result is `capability_unavailable` (LCK-CAP-2). Thus two consumers that get the same `lock` declared the same inputs and the same covered closure. The lock does not show which objects a consumer read (LCK-EXE-5, LCK-CAP-3).

### Bundle manifests

**BUN-MAN-1.** A bundle is a JSON object with the fields `format`, `roots`, `descriptors` and `payloads`. It can also have the field `withheld` (BUN-RED-5). It has no other field. `format` is `aavanam-bundle/v1`. `roots` is a list of one or more references. `descriptors` is a list of descriptor JSON texts. `payloads` is an object. Each key is a payload digest, and each value is the base64 encoding (RFC 4648, section 4) of the bytes. `withheld` is a list of one or more payload digests, with no duplicate. A digest in `withheld` is not a key of `payloads`.

### Export

**BUN-EXP-1.** The request of `export` has the fields `context` (CTX-OPN-3) and `roots`, a list of one or more references. It can also have the field `permit` (BUN-RED-4). If a root is not in the store, the result has status `not_found` and reason `unknown_ref`. If a root is in the store but not in the read context, the result has status `not_found` and reason `not_in_context`. The result has a reason for each affected root. In these cases, the result has no items and no `bundle`.

**BUN-EXP-2.** If each root is in the read context, the result has status `ok`, and the envelope field `bundle` is a bundle (BUN-MAN-1). Its `roots` are the roots of the request. Its `descriptors` have the descriptor of each object in the closure of the roots (KND-DEP-3), one time each. Its `payloads` have the bytes of each digest in the byte closure, except a withheld digest (BUN-RED-5). The bundle MUST NOT have a descriptor or bytes outside the closure.

**BUN-EXP-3.** The result has one item for each object in the closure of the roots. Each item has status `ok` and the fields `ref`, `redistribution`, `license` and `withheld` (BUN-RED-6). The order of the items and of `descriptors` is not specified.

### License and redistribution status

**BUN-RED-1.** The base profile defines the observation type `aavanam/redistribution/v1`. An observation of this type is the statement of a producer or an application about the license and the redistribution status of the payload bytes of each of its subjects. Its `values` is an object with these fields, and no other field:

| Field | Value |
|---|---|
| `license` | A string with one or more characters, or null. Null states that the declarer does not know the license. The store compares the string only as a string |
| `redistribution` | One of the declared values `redistributable`, `no_redistribution` and `unknown` |

The store does not read the `scope` of the observation. It does not check that a statement is correct, and it does not interpret a license.

**BUN-RED-2.** The statements of an object in an export are the observations of type `aavanam/redistribution/v1` in the closure of the roots of the request that have the object in their `subjects`. The store gives the redistribution value of the object with the first rule that applies:

1. If the descriptor of the object has no `payload` value type field with a value (KND-DEP-3), the value is `no_payload`.
2. If the `values` of a statement does not agree with BUN-RED-1, the value is `invalid`.
3. If the object has no statement, the value is `undeclared`.
4. If two statements have a different `license` or a different `redistribution`, the value is `conflict`.
5. If not, the value is the `redistribution` of the statements.

The license value is the `license` of the statements for rule 5, and null for the other rules. The closed set of redistribution values is these seven values. No other value is permitted.

**BUN-RED-3.** The store MUST NOT give a declared value to an object with the value `undeclared`, `conflict` or `invalid`. It MUST NOT select one statement of a conflict. A statement applies only to its own subjects. Thus a statement about a `source` does not apply to a `reading` or an `excerpt` of that source, and a statement about a snapshot does not apply to its members. The store reports these values. It does not decide that bytes can be redistributed.

**BUN-RED-4.** The field `permit` of an `export` request is a set (KND-SCH-5) of zero or more declared values of BUN-RED-1. The application selects the values that its target permits. If a value is not a declared value, the result has status `failed` and reason `field_type`. A duplicate value gives status `failed` and reason `field_count`. In these cases, the result has no items and no `bundle`. If the request has `permit`, the store withholds each object whose redistribution value is not `no_payload` and is not in `permit`. Thus an object with the value `undeclared`, `conflict` or `invalid` is always withheld. If the request has no `permit`, the store withholds no object on the basis of its redistribution value. An empty `permit` is not the same as no `permit`. It withholds each object with a payload.

**BUN-RED-5.** The withheld digests are the payload digests of each withheld object. A digest is withheld also if a different object in the closure has the same digest and is not withheld. `payloads` has no key for a withheld digest. The field `withheld` of the bundle lists each withheld digest. If no digest is withheld, the bundle has no field `withheld`. The descriptor of a withheld object stays in `descriptors`. Thus each reference in the bundle keeps its target in the bundle, and the descriptors are the full closure of the roots (BUN-EXP-2).

**BUN-RED-6.** The field `redistribution` of an export item is the redistribution value of the object (BUN-RED-2). The field `license` is its license value. The field `withheld` is `true` if a payload digest of the object is withheld (BUN-RED-5), and `false` if not. The items with `withheld` `true` are the list of the objects that the export left out of `payloads`. A withheld object is not a failure of the export. The result status stays `ok` (STS-SET-4).

### Import

**BUN-IMP-1.** The request of `import` has the field `bundle`. If the value does not agree with BUN-MAN-1, the result has status `failed` and reason `field_type`. The import does not use the field `withheld` in its checks (BUN-IMP-2).

**BUN-IMP-2.** The store checks each descriptor of the bundle with the steps of STS-MAL-3, with one change: a reference target or payload bytes can be in the bundle or in the store. If a reference target or the bytes of a payload are in neither, the result has status `failed` and reason `bundle_incomplete`. This applies also to the bytes of a digest in `withheld`. Thus an import of a bundle with `withheld` gives `bundle_incomplete` if the store does not have these bytes. If a root is not a descriptor of the bundle, the result also has status `failed` and reason `bundle_incomplete`. A different check that fails gives its own status and reason. If more than one descriptor fails a check, the store applies rule 6 of STS-ENV-3 to the outcomes of the descriptors, as if each descriptor were an item. A root that is not a descriptor of the bundle is one more `failed` outcome. Thus these outcomes give the status and the reasons of the result, but the result has no items (BUN-IMP-4).

**BUN-IMP-3.** If each check passes, the store adds each object of the bundle, and the result has status `ok`. The result has one item for each descriptor of the bundle. Each item has status `ok` and the field `ref`. The order of the items is not specified.

**BUN-IMP-4.** `import` is a complete operation (STS-ENV-4). If it is not `ok`, the result has no items, and the store does not change.

**BUN-IMP-5.** A store has one of three roles: a writer store, a device store or a local output store. The application selects the role when it creates the store. The role does not change, and a request does not name it. A device store has one local output store, and a local output store belongs to one device store. The application creates the two stores together.

**BUN-IMP-6.** A device store is read-only after its first `ok` import. `put`, `create_snapshot`, `create_candidate` and `publish` on a device store give status `unsupported` with reason `read_only_store`. If the device store has an object, `import` also gives this outcome. The store checks the role after the operation (STS-MAL-4) and before the request. The result has no items, and the store does not change (STS-ENV-4). An operation that only reads, for example `open_context`, `get` or `export`, is permitted on a device store.

**BUN-IMP-7.** `execution_inputs` and `build_inputs` on a device store read in a context of the device store. The store writes the lock to the local output store of the device store, and does not change the device store. Thus in LCK-EXE-4 and LCK-BLD-3, the store that has or does not have the lock is the local output store.

**BUN-IMP-8.** The objects of a local output store are the objects that it keeps and the objects of its device store. Its payload bytes are the bytes that it keeps and the bytes of its device store. Each rule that names an object or bytes in the store uses these sets for a local output store, for example KND-REF-2, KND-DEP-3, BUN-IMP-2 and CTX-OPN-1. A write to a local output store adds objects and bytes only to the local output store. If the device store has an object or bytes, a write of them does not change the local output store (IDN-CAN-3). The device store does not change after its import (BUN-IMP-6), so each target of an object in the local output store stays in one of the two stores.

### Excerpt objects

**BUN-EXC-1.** The `ranges` of an `excerpt` is a list of one or more objects. Each object has the fields `start` and `end` and no other field. `start` and `end` are integers, and 0 <= `start` <= `end`. A range gives the bytes of the parent payload from offset `start` up to offset `end`, without the byte at `end`. If `ranges` does not agree with this form, the store MUST reject the write with status `failed` and reason `field_type`.

**BUN-EXC-2.** The ranges MUST be in ascending order, and MUST NOT overlap. Thus the `start` of each range is equal to or more than the `end` of the range before it. If they are not, the store MUST reject the write with status `failed` and reason `field_type`.

**BUN-EXC-3.** The payload bytes of an `excerpt` MUST be equal to the bytes of the parent payload in each range, joined in the order of `ranges`. If a range ends after the parent payload, or the bytes are not equal, the store MUST reject the write with status `failed` and reason `excerpt_mismatch`. The store compares bytes only. It does not parse the parent payload.

**BUN-EXC-4.** The store checks BUN-EXC-3 after the payload bytes step of STS-MAL-3. If the store does not have the bytes of the parent payload, the result is status `not_found` and reason `payload_missing`, and the store does not keep the excerpt.
