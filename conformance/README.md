# Conformance vectors

A vector is a JSON file that tests one behavior of the store. It cites the clauses that give the expected outcome. The runner sends the calls of a vector to an implementation through an adapter, and compares each result with its expected envelope.

The vectors have authority over the claims (constitution). An expected outcome comes from the spec, a fixture definition or a planted fault. It never comes from an implementation run.

## Run

```sh
python3 -B tools/check_spec.py
python3 -B -m unittest discover -s conformance
python3 -B conformance/runner.py --adapter null_adapter:NullAdapter
python3 -B conformance/runner.py --adapter store_adapter:StoreAdapter
```

- `check_spec.py` checks each vector against the spec tables. It does not run an implementation.
- The unit tests check the runner.
- The runner prints `PASS` or `FAIL` for each vector. Before the first call, the runner runs the checks of `check_spec.py`. If the spec or a vector has a problem, the runner calls no vector. It exits with 0 if all vectors pass, 1 if a vector fails, and 2 if the spec or a vector has a problem.

The null implementation returns `unsupported` for each call. It must fail. It passes only the vectors that expect `unsupported` at each step.

The reference implementation (`src/aavanam/`) must pass all vectors. `store_adapter.py` gives each vector a new store in memory. If you set `AAVANAM_DEBUG=1`, the store raises an internal error in place of `storage_error`.

## Vector file

The ID of a vector is its path under `conformance/vectors/`, without `.json`, for example `kinds/put-source-ok`. The folder is the area of the spec. The folder `regressions/` holds the vectors for the defects that a pre-build prototype reproduced. Each of them has the provenance type `spike`. The folder `scenarios/` holds the multi-step consumer scenarios (see Scenarios). The folder `locks/` holds the refusals of `build_inputs` and their pair, and the vectors of document 10, topics `CAP` and `CMPL`. The folder `identities/` holds the vectors of document 01. The folder `bindings/` holds the vectors of document 05. The folder `lookup/` holds the vectors of document 09. The folder `coverage/` holds the vectors of document 07, topic `COV`. The folder `impact/` holds the vectors of document 12, topic `IMP`. The folder `publication/` holds the vectors of document 13. The folder `contexts/` holds the vectors of document 08, topics `VIW`, `CMP`, `SKW` and `PRJ`. The folder `rendering/` holds the vectors of document 14. The folder `reserved/` holds one vector for each reserved operation of document 16. The folder `devices/` holds the vectors of document 11, topics `CLS` and the store roles of `IMP`, over the three stores. The folder `acceptance/` holds the end-to-end acceptance vectors (see Acceptance). The folder `lifecycle/` holds the vectors of document 17. The folder `discovery/` holds the vectors of document 18. The folder `producer/` holds the vectors of document 19. The folder `redistribution/` holds the vectors of document 11, topic `RED`. The folder `reads/` holds the vectors of document 20.

| Field | Value |
|---|---|
| `format` | `aavanam-vector/v1` |
| `title` | One sentence that gives the tested behavior |
| `clauses` | The clause IDs that give the expected outcome. Each ID must be defined and not withdrawn |
| `provenance` | An object with `type` and `note`. A `spike` vector also has `source` |
| `profile` | `aavanam/base/v1` |
| `fixture` | Optional. The name of a folder in `fixtures/`, or a list of two or more different names (see Scenarios) |
| `payloads` | Optional. Named payload bytes (see below) |
| `steps` | A list of calls. Each step has `operation`, `request` and `expect`. A step can also have `as_context`, `as_bundle`, `as_ref` (see Handles), `fresh_store`, `store` and `lock_store` (see below) |
| `pair` | The ID of the positive pair. Only a refusal vector has it |
| `acceptance` | Optional. A test acceptance policy (see Acceptance policies) |
| `gates` | Optional. A list of collection names. Before the first step, the runner declares each name as gated (PUB-GAT-1) |
| `sections` | Optional. An object from a collection name to a section name. Before the first step, the runner declares each section (PUB-SEC-1) |

The provenance types:

| Type | The expected outcome comes from |
|---|---|
| `spec` | The cited clauses |
| `spike` | A defect that a pre-build prototype reproduced. `note` states the defect. `source` says where it was reproduced. The vector uses neutral data |
| `fault` | A fault that the runner plants, for example a changed digest. `note` names the fault |

A refusal vector is a vector whose last step expects a status other than `ok`. It must have a `pair`: a vector that ends with `ok` and shows the accepted form of the same input. The pair changes only the values that cause the refusal, for example a `logical_id` of type string in place of a number. A check-order vector (STS-MAL-3) has more than one fault, so its pair repairs each fault. For a refusal of the operation itself (STS-MAL-4), the store does not read the input. Thus the pair is a vector that calls a supported operation. Only the last step of a vector can expect a status other than `ok`. The earlier steps prepare the store.

## Expected envelope

`expect` has these fields:

- `status`: one status from the status table.
- `reasons`: the reason codes of the envelope. The runner compares them as a sorted list. It does not compare their order.
- `items`: optional. One object with `status` and `reasons` for each result item, in order. A `put` step must have one item for each descriptor. A `put` that expects `read_only_store` has no items (BUN-IMP-6). An item can also have `fields`, an object of result fields. The runner replaces the handles in `fields`. The returned item must have each field, with the same JSON type and value. Two numbers are equal if their values are equal, for example `1` and `1.0`. An expect or an item can also have `reason_details`, a list with one object for each reason of the envelope or the item. Each object has `code`, and `refs`, `capabilities` or both. Each object has the same fields. The runner then compares each reason code with the sorted values of these fields. It does not compare their order.
- `unordered`: optional. If `true`, the runner compares the items in any order. `check_spec.py` permits it only for operations whose clauses give no order: `evidence` (REL-EVD-4), `observations` (OBS-SUB-4), `coverage` (OBS-COV-3) and `execution_inputs` (LCK-EXE-4), `build_inputs` (LCK-BLD-3), `export` (BUN-EXP-3) and `import` (BUN-IMP-3).
- `store_unchanged`: optional. If `true`, the store fingerprint before and after the call must be equal (STS-MAL-2).

The runner also checks that the result has the five envelope fields of STS-ENV-1, that `operation` is the called operation, that the envelope field `context` of `snapshot_changes` and `item_history` is null (LIF-ITM-4), that each reason has `code`, `refs` and `capabilities` (STS-RSN-2), and that each returned item has `status` and `reasons` (STS-ENV-2). It checks the returned items and their reasons also if the vector expects no items: each item has a status other than `partial`, and it has reasons only if its status is not `ok` (STS-SET-1, STS-RSN-1). Each registered reason of an item has the status of the item in the reason table (STS-RSN-3). The runner does not check the status of an extension reason. If the request field `context` is a `$context` handle, the envelope `context` must be equal to the identity (CTX-OPN-3). An `ok` `open_context` must return a string in `context`, and any other `open_context` must return null (CTX-OPN-2). For `export`, an `ok` result must have a `bundle` with the fields and `format` of BUN-MAN-1, the request roots, the stored descriptor of each expected item and no other descriptor (in the canonical form of IDN-CAN-1), no duplicate key, and the bytes of each digest in the payload fields of its descriptors, with the correct digest and size (BUN-EXP-2, KND-OPQ-1). A withheld digest is in `withheld` and not in `payloads` (BUN-RED-5). If each expected item has the fields `ref` and `withheld`, the withheld digests must be the payload digests of the items with `withheld` `true`. The runner reads the payload fields from the kind table. It does not read the opaque JSON fields. A refused `export` must have no `bundle` (BUN-EXP-1). An `ok` `execution_inputs` or `build_inputs` must have the revision of its lock in the envelope field `lock`, and any other result must have no lock (LCK-EXE-4, LCK-EXE-6, LCK-BLD-3, LCK-BLD-5). The runner makes the lock descriptor from the request and computes its revision. Thus the check covers each field of the lock, with no read of the lock. If no earlier step of the vector wrote or returned the same revision, the store fingerprint must change. No base operation reads a stored lock. Thus the runner does not compare the stored content. If a reserved operation expects `reserved_profile`, the reason must have no `refs` and only the profile of the operation from the reserved profile table in `capabilities` (RSV-RSP-1). The result must have no lock and no bundle (STS-ENV-4). The runner does not check `context` if the vector expects `unsupported_operation` or `reserved_profile` (STS-MAL-4). The format compares `refs` and `capabilities` only for an envelope or an item with `reason_details`. It does not compare result fields that the vector does not list.

`check_spec.py` rejects an expected envelope that does not agree with the spec tables:

- An `ok` envelope or item has no reasons. Any other status has one or more reasons (STS-RSN-1).
- Each reason code has the status that the reason table gives for it (STS-RSN-3).
- An item cannot be `partial` (STS-SET-1).
- A `partial` envelope has one or more `ok` items. Its reasons are `bounds_reached`, `items_not_ok` or both. It has `items_not_ok` if an item is not `ok`, and only then (STS-ENV-3 rules 2 and 5).
- The envelope agrees with its items. An `ok` envelope has only `ok` items. A `put` that is not `ok` has no `ok` item (STS-ENV-5). If the envelope reasons come from the items, the status and reasons follow rule 6 of STS-ENV-3.
- An operation whose row gives only `unsupported` expects `reserved_profile` (STS-MAL-4).
- Each reserved operation is in the reserved profile table of document 16 one time, and has a row with document 16 and only `unsupported` (RSV-LST-2).
- The status is in the row of the operation. An operation with no row expects `unsupported` with `unsupported_operation` (STS-SET-3).

## Handles

A reference is the digest of the canonical form of a descriptor (IDN-CAN-2). It depends on the digests of the payloads and of the referenced descriptors. A vector does not contain these computed values. It uses handles, and the runner replaces each handle before the call:

| Handle | The runner replaces it with |
|---|---|
| `{"$ref": "h"}` | The reference that the store returned for the descriptor with `"as": "h"` in an earlier step |
| `{"$context": "c"}` | The context identity that the envelope of an `ok` step with `"as_context": "c"` returned |
| `{"$bundle": "x"}` | The bundle that the envelope of an `ok` step with `"as_bundle": "x"` returned |
| `{"$bundle": "x", "fault": "drop_payload"}` | That bundle without the payload that has the lowest digest |
| `{"$bundle": "x", "fault": "format"}` | That bundle with a different `format` |
| `{"$absent": "h"}` | A well-formed reference that is not in the store |
| `{"$payload": "p"}` | The payload object of the bytes `p`: `digest`, `media_type` and `size` |
| `{"$payload": "p", "fault": "digest"}` | The payload object with a digest of different bytes. The runner sends the bytes under that digest |
| `{"$payload": "p", "fault": "size"}` | The payload object with the size plus 1 |

A `create_snapshot` or `create_candidate` step can have `"as_ref": "h"`. If the step is `ok`, the runner binds `{"$ref": "h"}` to the snapshot revision or to the candidate identity. The runner computes this value from the request (PUB-SNP-1, PUB-CND-3), and the item must have it in the field `ref` or `candidate`. A candidate identity is not a reference (PUB-CND-4). The handle is only a test convention.

A step with `"fresh_store": true` runs on an empty store. Before the step, the runner calls `reset()` and removes the context handles. The reference and bundle handles stay. Thus a vector can export from one store and import into a second store, for example a device.

A vector has three stores: `writer`, `device` and `output`. They have the roles of BUN-IMP-5: `writer` is a writer store, `device` is a device store, and `output` is the local output store of `device`. The field `store` of a step gives the store that gets the call. The default is `writer`. A context handle is bound in the store of its step, and only a step on the same store can use it. `fresh_store` empties all three stores. If a step expects `store_unchanged`, no store can change.

An `execution_inputs` or `build_inputs` step can have `lock_store`. This is the store that keeps the lock, for example the output store of a device consumer. The default is the store of the step. The lock checks read the lock in this store (LCK-EXE-4, LCK-BLD-3). A step on `device` writes its lock to `output` (BUN-IMP-7), so it has `"lock_store": "output"`.

A payload declaration has `media_type` and one of `text` or `base64`. The bytes are the UTF-8 encoding of `text`, or the bytes that the base64 text (RFC 4648 section 4) gives. Use `base64` for bytes that are not valid UTF-8. If the declaration has `"stored": false`, the runner does not send the bytes.

## Scenarios

A scenario is a vector that names a `fixture`. Before the steps of the vector, the runner puts each descriptor of the fixture, one `put` step for each descriptor, in the fixture order. Each of these steps expects `ok`. The handle of a fixture descriptor is its `as` name. The payloads of the fixture become vector payloads with their names. The runner changes the fixture handles before the call:

- `{"$text": t, "media_type": m}` becomes a payload with the text `t`. Its name is `<handle>#<field>`, for example `ex-guard#payload`.
- `{"$ranges": p, "text": t}` becomes the `ranges` of BUN-EXC-1: one range that gives the bytes of `t` in the payload `p`. The text occurs one time in `p` (`check_fixtures`).

If `fixture` is a list, the runner puts the descriptors of each fixture in the list order. Each handle and each payload name of a fixture gets the prefix `<fixture>:`, for example `product-specification:snap`. Thus two fixtures can use the same handles in one vector.

A scenario must have its own steps. A vector payload must not have the name of a fixture payload. `check_spec.py` checks a scenario with the fixture steps included.

Each consumer is an input recorder. The scenario makes the calls of the consumer and checks what the store returns. It does not run an engine, an emitter or a rule. The expected items come from the spec and the fixture definition:

| Consumer | Calls |
|---|---|
| Response agent | `open_context`, `get` of a table and its excerpt, `evidence` of the table |
| Research agent | One added observation, `open_context`, `get`, `evidence` and `observations` |
| Execution script | `open_context`, `execution_inputs` of the BindingSet with the RelationSet |
| Emitter | `open_context`, `build_inputs` of the BindingSet, then of the unit (LCK-BLD-4) |

Each consumer has one scenario on each fixture collection.

## Acceptance

An acceptance vector moves a consumer scenario to a device store. It opens the context of the scenario on the writer store, exports the roots of the context and imports the bundle into the device store. The expected items of the export and the import are the closure of the roots in the fixture (BUN-EXP-2). The steps of the scenario then run on the writer store, and the same steps run on the device store in a device context with the same roots. Each device step expects the same items as the writer step (BUN-CLS-3). A lock step on the device writes to the output store. The runner computes each lock from the request, so the device lock has the revision of the writer lock (BUN-CLS-4).

The folder has one vector for each consumer on each fixture collection. The vector `acceptance/composed` composes both collections in one context (CTX-CMP-1). It runs the response agent, the execution script and the emitter on it.

## Rendered documents

For `render`, the runner makes the document from the request and the returned items (RND-DOC-3). The `document` and the `document_id` of an `ok` or `partial` result must be equal to it. Each other result must have the fields `document` and `document_id` with the value null. The envelope must have no other added field. Each `ok` item must have exactly the fields of its entry type, and each gap must be a gap of RND-SCP-4. Thus a store that adds a label, for example a field `verified`, fails (RND-SCP-5). Each item that is not `ok` must be a subject item with only `entry` and `ref` (RND-DOC-2).

## Acceptance policies

The store does not decide acceptance (PUB-RCP-1). Thus a vector that publishes has the field `acceptance`, with `policy`, a policy name, and `accept`, a list of `[candidate, receipt]` pairs with handles. Before the first step, the runner registers a validator under that name. The validator accepts a candidate and a receipt only if the pair is in `accept`. This validator is a part of the test, not of the store.

The runner records the validator calls of each step. A `publish` that expects the disposition `published`, or the status `incompatible` with no reason of the activation checks, must call the validator with the candidate, the receipt and the activation of the request (PUB-RCP-2). The activation is null if the request has none. The decision does not depend on the activation. If the validator does not accept, the result must not be `published`. Each other step must not call it, for example a repeat `publish` (PUB-HED-5). A `publish` with a reason of the activation checks must also not call it, because these checks come before the validator (PUB-ACT-3). These reasons are `activation_required`, `activation_mismatch`, `lock_has_snapshot`, `lock_outside_snapshot`, `change_outside_lock` and `removal_refused`. A pair with a handle that a later step binds is not accepted before that step.

An `ok` call of `create_snapshot` or `create_candidate` that returns a new value must change the fingerprint (PUB-SNP-3, PUB-CND-4). A repeat call must not change it. A `publish` with the disposition `published` must also change the fingerprint (PUB-HED-3).

The vectors run one call at a time. Thus they do not test two concurrent calls (PUB-RCP-3). The tests of a store must cover them.

## Calls to the adapter

The adapter interface is in `adapter.py`. It has five methods:

- `reset()` makes the three stores empty. The runner calls it before each vector.
- `call(operation, request, store)` sends the request to the store and returns the result envelope as a JSON object. The default store is `writer`.
- `fingerprint(store)` returns a text that changes if the content of the store changes.
- `accept(policy, validate)` registers an acceptance policy. `validate(candidate, receipt, activation)` returns true if it accepts the receipt reference for the candidate identity (PUB-RCP-2). `activation` is the activation reference of the request, or None.
- `gate(collection)` declares the collection as gated in each of the three stores (PUB-GAT-1). A step with `fresh_store` makes new stores, and a new store has no gate.

The request of a `put` step is a list of descriptor entries. Each entry has `value` (a JSON descriptor) or `raw` (the descriptor text, for malformed JSON). It can also have `as`, a handle name. The runner sends this request to the adapter:

```json
{"descriptors": ["<descriptor JSON text>", "..."], "payloads": {"sha256:<hex>": "<base64 bytes>"}}
```

If a `put` step is `ok`, each item must have a field `ref`, the revision of its descriptor (IDN-CAN-2). The runner computes the revision of each sent descriptor: it omits the empty sets, sorts the reference sets (KND-SCH-3, KND-SCH-5) and encodes the result with RFC 8785. It compares the revision with the returned `ref`, and binds the handle to it. For other operations, the runner replaces the handles and sends the request without other changes.

These request forms are a test convention, not an API. The adapter maps them to the API of its implementation.
