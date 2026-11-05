# Contributing

Aavanam is spec first. The [constitution](CONSTITUTION.md) has authority over the [specification](SPEC.md). The specification has authority over the code.

## Layout

- `SPEC.md` and `docs/contracts/`: the specification, with clause IDs.
- `conformance/`: JSON vectors, the runner and the adapters.
- `fixtures/`: neutral synthetic collections.
- `src/aavanam/`: the reference implementation. It runs the conformance suite. It does not fix the language, the storage or the interface of a product implementation.
- `tools/check_spec.py`: the checks of the index, the clauses, the registry tables, the vectors and the fixtures.
- `tools/check_project.py`: the check of the harness layout, the local links and the source inventory.

Do not add an evaluator, emitter, rule-engine, rule-extension or policy-engine directory. Aavanam stores and resolves. It never executes.

## Workflow

1. Open an issue with the change and its completion criteria.
2. Change the spec before the code. Cite the clause IDs in vectors, code and PRs.
3. Write each expected outcome from the spec, a fixture definition or a planted fault. Never copy it from an implementation run.
4. Give each refusal vector a positive pair, so that an implementation that refuses all calls fails.
5. Open a PR from a topic branch. Fill in the PR template.

## Checks

You do not need Codex, Claude Code or agy. The checks use only Python 3 and its standard library.

After each change:

```bash
python3 -B tools/check_project.py
```

After an intended source edit, regenerate the inventory with `python3 -B tools/check_project.py --write-checksums`, and examine the diff of `checksums.json`.

After a spec or vector change:

```bash
python3 -B tools/check_spec.py
python3 -B -m unittest discover -s conformance
```

After a change in `src/`, also:

```bash
python3 -B -m unittest discover -s tests
python3 -B conformance/runner.py --adapter store_adapter:StoreAdapter
python3 -B conformance/runner.py --adapter null_adapter:NullAdapter
```

The store must pass all vectors. The null implementation must still fail.

## Review

[docs/REVIEW.md](docs/REVIEW.md) gives the review and merge rules. The CI runs the checks on Ubuntu and macOS. A reviewer flags these as high priority:

- A dependency that does not come from a typed reference field.
- A status that changes unknown, missing or unsupported into a value.
- A vector whose expected outcome came from an implementation run.
- A refusal vector with no positive pair.
- Code that loads or executes a stored artifact.
- Private material, credentials or generated binaries.
- A removed failure case.

## Writing

Normative clauses use RFC 2119 keywords. Write short sentences. Do not use em dashes or filler words.
