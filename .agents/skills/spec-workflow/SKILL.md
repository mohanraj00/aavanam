---
name: spec-workflow
description: Change the Aavanam specification, its conformance vectors or the reference implementation, spec first.
---

# Spec workflow

Read the constitution, AGENTS.md and docs/STATUS.md. Select one tracked issue. Find the clause IDs that the issue touches in `docs/contracts/`.

1. Change the contract text first. Give each new rule a clause ID. Use RFC 2119 keywords.
2. Write the vectors from the clauses, a fixture definition or a planted fault. Never copy an expected outcome from an implementation run. Record the provenance of each vector.
3. Give each refusal vector a positive pair, so that an implementation that refuses all calls fails.
4. Change `src/` only after the spec and the vectors. Cite the clause IDs in the code.
5. Plant each fault that a new vector must catch, and confirm that the vector fails. Remove the fault.
6. Run the checks in AGENTS.md. The store must pass all vectors. The null implementation must still fail.

Aavanam stores and resolves. It never loads, evaluates or executes a stored artifact. The reference implementation uses only the Python standard library and SQLite. Keep fixtures synthetic and neutral. Keep a failed case with its vector, and record the reason for a changed vector.
