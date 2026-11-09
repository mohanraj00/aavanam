# Aavanam

A versioned knowledge store for agents, scripts and code emitters. Spec first. Pre-release.

I built Aavanam because the consumers of one body of knowledge read different copies of it. A response agent reads a text chunk. An execution script reads a table. An emitter reads a schema. No one can say which exact inputs produced an answer or a build. Aavanam keeps structured documents, typed data tables, opaque rule companions and evidence in immutable snapshots. Each consumer reads the same snapshot, and each run can lock the exact inputs that it used.

Aavanam stores and resolves. It never executes. Applications own their engines, emitters and rule semantics. The store keeps their artifacts as opaque bytes.

## Where it stands

| Fact | Value |
|---|---|
| Contract clauses | 248, in 16 documents under `docs/contracts/` |
| Conformance vectors | 400. The reference implementation passes all of them |
| Null implementation | Passes 1 vector and fails 399. The runner exits with 1 |
| End-to-end acceptance | 9 vectors. Four consumers on two neutral collections, through an export, a device import and a read on the device, plus 1 composed context |
| Reference implementation | Python over SQLite. The standard library only |

Each refusal vector has a positive pair, so an implementation that refuses all calls fails. No expected value comes from an implementation run. Each vector states its provenance: the spec, a defect reproduced in a pre-build prototype, or a planted fault.

## What it is not

- It is not a rule engine, an evaluator or a policy engine.
- It is not access control. A capability states what a store can do, not who can call it.
- It does not prove semantic completeness. A lock is complete over declared references, and its result says so.
- Semantic search and cross-catalog contexts are reserved. They are not implemented.
- The code in `src/aavanam/` is not the product runtime. It is the reference implementation: it proves that the spec can be implemented and that each vector is exact. The spec does not fix the language, the storage or the interface of a product implementation.

All 16 contract documents are drafts. Next: lifecycle and discovery, then integrations with producers and consumers. [docs/STATUS.md](docs/STATUS.md) gives the status.

## 0.1 compatibility

Within the 0.1 series, the reference store preserves the documented `aavanam/base/v1` minor version 3 descriptor meanings, canonical reference digests, `aavanam-bundle/v1` transfer format and the outcomes of the 0.1.0 conformance vectors. A later 0.1 version can add compatible behavior and vectors.

The Python reference API, the conformance runner interface, the SQLite layout and the producer adapter interface can change. Use a versioned bundle to transfer a snapshot between store versions. The producer seam test is pending, and reserved operations return their specified unsupported status. The source version identifies the draft specification, conformance vectors and reference store together.

See [the changelog](CHANGELOG.md) for the contents of 0.1.0.

## Run the checks

```bash
python3 -B tools/check_spec.py
python3 -B -m unittest discover -s conformance
python3 -B -W ignore -m unittest discover -s tests
python3 -B conformance/runner.py --adapter store_adapter:StoreAdapter
python3 -B conformance/runner.py --adapter null_adapter:NullAdapter
```

The last command must fail. Start at [SPEC.md](SPEC.md) for the specification and [CONTRIBUTING.md](CONTRIBUTING.md) for the workflow. The CI runs these checks on Ubuntu and macOS for each PR.

Licence: Apache-2.0.
