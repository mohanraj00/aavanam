# Migration between versions

State: normative. Prefix: `MIG`. Profile: `aavanam/base/v1`.

## Scope

This document defines what an implementation does with data that an earlier release wrote: a store file, the two files of a device store, a database of a server storage, and a bundle. It also gives the rule for a different major version.

A stored form is the data of a store that stays after the program ends. An earlier release is a release with the same major version of each profile, and with an equal or lower minor version. A later release is the other release of such a pair.

This document adds no operation, no status and no reason. Document 21 gives the rules for the files of the layout `aavanam/device-sqlite/v1` (DEV-MIG-1 to DEV-MIG-3). Document 22 gives the version rule of the network read binding (NET-VER-2, NET-CMP-1).

## Topics

| Topic | Subject |
|---|---|
| `VER` | A store of an earlier minor version |
| `STG` | The stored form of an implementation: changes and derived data |
| `TRF` | Bundles and the network read binding |
| `MAJ` | A different major version |

## Clauses

### A store of an earlier minor version

**MIG-VER-1.** A release of an implementation wrote a stored form at minor version N of the base profile. A later release of that implementation, at minor version M of the same major version, MUST open that stored form. It uses the stored form as it is, or it changes the stored form as MIG-STG-1 permits. It MUST NOT need an export and an import of the content.

**MIG-VER-2.** The kept data of a stored form is this data:

1. Each object and the bytes of each payload.
2. The catalog (PUB-HED-1), each candidate (PUB-CND-4) and each publication record (PUB-LOG-2).
3. Each gate declaration and each section declaration (PUB-GAT-2, PUB-SEC-2).
4. Each removal record (REL-RMV-5), and the references that the store keeps for a removed object (REL-RMV-8).
5. The producer section (PRD-SEC-1).
6. Each `deferred` record (BUN-IMP-10), and the roots of each `ok` import of a device store (BUN-RMV-1).
7. Each read context that has not ended (CTX-LIF-5).

After the open of MIG-VER-1, each operation of minor version N MUST give the result that the clauses of minor version N give for the kept data and the request: the same status, the same reasons and the same items. A reference does not change, because a revision is a function of the descriptor (IDN-CAN-2).

**MIG-VER-3.** An operation, a request field or a result field that minor version M adds MUST have a defined result on a stored form of minor version N. The result is the result that its clauses give for the kept data, or a status and a reason of the tables of document 03. If the addition needs a record that minor version N did not write, its clauses state the result for a stored form with no such record. For example, a read context of an earlier version has each capability (CTX-PRJ-2) and no declared life (CTX-LIF-1). The store MUST NOT give a value that the kept data does not give (constitution, principle 2).

**MIG-VER-4.** A read context that an earlier release opened stays open until it ends (CTX-LIF-5). Its identity and its members do not change (CTX-OPN-2). A later release can keep a new record for each read context that it opens, for example the removed objects of REL-RMV-13. A read context of the earlier release has no such record. For a case that needs the record, the store MAY give, in that context, the result that the earlier release gave. A read context that the later release opens has the record, and the clause of the record applies to it.

**MIG-VER-5.** This document gives no result for a stored form that a later release wrote and an earlier release opens. An implementation MAY refuse such a stored form (MIG-STG-3). A layout clause can permit it, for example DEV-LAY-6.

### The stored form of an implementation

**MIG-STG-1.** An implementation can change a stored form of an earlier release, for example to add a table or a column. Each change MUST be one transaction of the storage, and it MUST keep the kept data of MIG-VER-2. If the change does not complete, the stored form MUST stay in the form that the earlier release reads. Two programs that open one stored form at the same time MUST NOT apply one change two times.

**MIG-STG-2.** An implementation MUST find the changes that a stored form does not have. It finds them from a version record in the stored form, or from the tables of the stored form. It MUST NOT assume that a stored form has the newest form.

**MIG-STG-3.** If a stored form has a version record that the implementation does not know, the implementation MUST NOT write to the stored form, and it MUST refuse to open it. This failure is a failure to open the store. It is not the result of an operation, and it has no result envelope (DEV-PRF-2).

**MIG-STG-4.** Derived data is data that is a function of the objects and of the payload bytes, for example a closure (KND-DEP-3), the rows of a table payload or the tokens of a text index (DEV-LAY-4). A later release can add derived data. It can write the derived data of the kept data at the open, at the first use, or in a later step. Before, during and after that step, each operation MUST give the result of MIG-VER-2. An operation MUST NOT read absent derived data as absent content. It reads the objects and the bytes. For example, `search_text` scans the bytes of a payload that the index does not have (DEV-OUT-3).

**MIG-STG-5.** A step that writes derived data MUST NOT change an object, payload bytes or a record of the kept data. It MUST NOT write derived data of content that a `remove` or a `gc_collect` removed (REL-RMV-4, LIF-GC-5), also if the removal occurs at the same time as the step.

### Bundles and the network read binding

**MIG-TRF-1.** A bundle (BUN-MAN-1) or a raw bundle (BUN-RAW-1) that an earlier release exported MUST import on a later release, with the result that documents 11 and 20 give. This also applies to a delta bundle (BUN-DLT-3), also if an earlier release wrote its base to the stored form. After the import, each object has the reference that it had in the store of the export (IDN-CAN-2), and MIG-VER-2 applies to each read.

**MIG-TRF-2.** A later minor version can add an optional field to the bundle, as BUN-MAN-1 lists. A bundle with no such field keeps its meaning. An implementation of an earlier minor version does not know a field of a later minor version. Then BUN-IMP-1 gives `failed` with `field_type`, and the store does not change.

**MIG-TRF-3.** A server of a later release gives the results of MIG-VER-2 through the network read binding `aavanam-read/v1`, for a store that an earlier release wrote. NET-VER-2 and NET-CMP-1 give the rule for a client of an earlier `api_minor`.

### A different major version

**MIG-MAJ-1.** A different major version is a different identity, for example `aavanam/base/v2`, `aavanam/device-sqlite/v2`, `aavanam-bundle/v2` or `aavanam-read/v2` (SPEC.md, "Profiles and versions"). An implementation MUST NOT read a stored form or a bundle of a different major version as data of its own major version. Only a declared transformation makes the data of one major version from the data of a different one. A declared transformation is a step that the release notes of an implementation name. It names the two identities, it reads the old data, and it writes new objects. The old objects keep their old identity.

**MIG-MAJ-2.** With no declared transformation, an `import` of a bundle whose `format` is not the format of the major version of the store gives `failed` with `field_type` (BUN-IMP-1, BUN-RAW-4). The store does not change.

**MIG-MAJ-3.** With no declared transformation, an implementation MUST refuse to open a stored form of a different major version. The rule of MIG-STG-3 applies: this is a failure to open the store, and the stored form MUST NOT change. A file of a different layout identity is not a store of the layout (DEV-PRF-2). A path of a different major version of the network read binding gives the response of NET-TRN-4. An interface that reports a failure to open with a reason code MUST use `unsupported_profile`.
