# Consumer input locks

State: draft. Prefix: `LCK`. Profile: `aavanam/base/v1`.

## Scope

This document defines execution and build input locks for application-owned consumers. A lock covers the declared reference closure. It states that semantic completeness is application-declared and not proven.

## Topics

| Topic | Subject |
|---|---|
| `EXE` | Execution input locks |
| `BLD` | Build input locks and consumed evidence |
| `CAP` | Runtime capability requirements |
| `CMPL` | Declared completeness labels |

## Clauses

### Execution input locks

**LCK-EXE-1.** The request of `execution_inputs` has the fields `context` (CTX-OPN-3), `binding_set`, one reference, `consumer`, a string, and `completeness`, a string. It can also have `context_inputs`, a list of references, and `requires` (LCK-CAP-1). `context_inputs` is a set (KND-SCH-5). The store checks the references first. If `binding_set` or a context input is not in the store, the result has status `not_found` and reason `unknown_ref`. If it is in the store but not in the read context, the result has status `not_found` and reason `not_in_context` (CTX-OPN-4). The result has a reason for each affected reference.

**LCK-EXE-2.** If each reference is in the read context, the store checks the kinds. If `binding_set` does not have kind `binding-set`, or a context input has a kind that the field `context_inputs` of `execution-lock` does not permit (KND-REF-3), the result has status `unsupported` and reason `unsupported_target_kind`.

**LCK-EXE-3.** Each `artifact` that the BindingSet binds can declare roles in `declared_roles`. If no outcome of LCK-EXE-1, LCK-EXE-2 or LCK-CAP-2 applies, and a declared role has no binding in the `bindings` of the BindingSet, the result has status `unresolved` and reason `unbound_role`.

**LCK-EXE-4.** If no rule of LCK-EXE-1, LCK-EXE-2, LCK-CAP-2, LCK-EXE-3, BND-ROL-2 or BND-RUL-2 applies, the store writes an `execution-lock` with the `binding_set`, `context_inputs`, `requires`, `consumer` and `completeness` of the request, and the result has status `ok`. The lock has these fields, `kind` `execution-lock` and `descriptor_profile` `aavanam/base/v1`, and no other field. The envelope field `lock` is the reference of the lock, its revision (IDN-CAN-2). If the store does not have this lock, the store changes. The result has one item for each object in the closure of the BindingSet and of each context input (KND-DEP-3). Each item has status `ok` and the field `ref`. The order of the items is not specified.

**LCK-EXE-5.** The items of LCK-EXE-4 MUST be the closure that the lock covers, and no other set. The store MUST NOT add an object outside the closure of the BindingSet and of the context inputs, and MUST NOT omit an object in it. A consumer that reads an object outside this closure does not have it in its lock.

**LCK-EXE-6.** In each case other than LCK-EXE-4, the result has no items and no `lock`, and the store does not change (STS-ENV-4). `completeness` is the declaration of the consumer. The store checks only its form (LCK-CMPL-1).

### Build input locks and consumed evidence

**LCK-BLD-1.** The request of `build_inputs` has the fields `context` (CTX-OPN-3), `inputs`, a list of one or more references, `consumer`, a string, and `completeness`, a string. It can also have `requires` (LCK-CAP-1). `inputs` is a set (KND-SCH-5), so an empty or a duplicate input gives `field_count`. The store checks the references first. If an input is not in the store, the result has status `not_found` and reason `unknown_ref`. If it is in the store but not in the read context, the result has status `not_found` and reason `not_in_context` (CTX-OPN-4). The result has a reason for each affected reference.

**LCK-BLD-2.** If each input is in the read context, the store checks the kinds. If an input has a kind that the field `inputs` of `build-lock` does not permit (KND-REF-3), the result has status `unsupported` and reason `unsupported_target_kind`.

**LCK-BLD-3.** If no rule of LCK-BLD-1, LCK-BLD-2 or LCK-CAP-2 applies, the store writes a `build-lock` with the `inputs`, `requires`, `consumer` and `completeness` of the request, and the result has status `ok`. The lock has these fields, `kind` `build-lock` and `descriptor_profile` `aavanam/base/v1`, and no other field. The envelope field `lock` is the reference of the lock, its revision (IDN-CAN-2). If the store does not have this lock, the store changes. The result has one item for each object in the closure of each input (KND-DEP-3), and no other item. Each item has status `ok` and the field `ref`. The order of the items is not specified.

**LCK-BLD-4.** The consumed evidence of a build lock is the set of `evidence` records in its items. An `evidence` record refers to its subject, and the subject does not refer to the record. Thus the store MUST NOT add an `evidence` record to the items because its subject is in the closure. An emitter that needs evidence names an input that has the evidence in its closure, for example a `knowledge-unit` with a RelationSet.

**LCK-BLD-5.** In each case other than LCK-BLD-3, the result has no items and no `lock`, and the store does not change (STS-ENV-4). `completeness` is the declaration of the consumer. The store checks only its form (LCK-CMPL-1). The store does not read or run an artifact payload to make the lock.

### Runtime capability requirements

**LCK-CAP-1.** The request of `execution_inputs` or `build_inputs` can have the field `requires`, a set of capability names (KND-SCH-5, CTX-PRJ-1). A list that is not a list of strings gives `field_type`. A name that occurs again gives `field_count`. The lock has the names in its field `requires`. An empty list gives a lock with no `requires` (KND-SCH-3).

**LCK-CAP-2.** If no outcome of LCK-EXE-1 and LCK-EXE-2, or of LCK-BLD-1 and LCK-BLD-2, applies, the store checks each name of `requires`. If a name is not in the capability table, the result has status `unsupported` with reason `unsupported_profile`. If a name is in the table but the read context does not have it (CTX-PRJ-2), the result has status `unsupported` with reason `capability_unavailable`. The result has one reason for each such name, and the `capabilities` of the reason name it.

**LCK-CAP-3.** `requires` states the capabilities that the consumer needs when it reads the inputs of the lock, for example on a device store. The store records the names. It does not run the consumer, and it does not check that the consumer uses only these capabilities. Document 11 uses the names for capability closure.

### Declared completeness labels

**LCK-CMPL-1.** The value of `completeness` is a completeness label. It MUST have the form `<owner>/<name>/v<N>`. `<owner>` and `<name>` each have one or more characters from `a` to `z`, `0` to `9`, `-` and `.`. `<N>` is a positive integer with no leading zero. A value with a different form gives `field_type`. This also applies to the field `completeness` of a lock descriptor in `put` or `import` (KND-SCH-4).

**LCK-CMPL-2.** The base profile gives one label, `aavanam/closure-only/v1`. With this label, the consumer declares only that the lock covers its reference closure (LCK-EXE-5). Each other label is application-owned. The store MUST NOT give a meaning to an application-owned label.

**LCK-CMPL-3.** A completeness label is a declaration, not a proof. The store does not check that the inputs are semantically complete for the consumer. A lock covers its reference closure with each label. A consumer MUST NOT use the label as a result of the store.
