# Agent workflow

Read [CONSTITUTION.md](CONSTITUTION.md), this file, then [docs/STATUS.md](docs/STATUS.md). GitHub issues own current work.

## Shared harness setup

Codex reads this entry point. `CLAUDE.md` and `GEMINI.md` import the constitution and this file, for Claude Code and agy (Antigravity). Shared skills live in `.agents/skills/`. `.claude/skills` is a relative link to that folder. If your harness does not find a skill, read its `SKILL.md` directly. Edit shared instructions only at their owning path.

For spec, vector or reference implementation work, read [.agents/skills/spec-workflow/SKILL.md](.agents/skills/spec-workflow/SKILL.md). The checks are deterministic and need no model account. This setup does not claim the same model performance in each harness.

## Layout

- `SPEC.md` and `docs/contracts/`: the normative specification, with clause IDs.
- `conformance/`: JSON vectors, the runner and the adapters.
- `fixtures/`: neutral synthetic collections.
- `src/`: the reference implementation.
- `tests/`: the tests of the reference implementation that the runner cannot express, for example two opens of one store.
- `tools/`: the spec check, the project check, change classification, the PR gates and the local cross review.
- `docs/`: the status and the review rules.

Do not add evaluator, emitter, rule-engine, rule-extension or policy-engine directories.

## Work

1. Start from a GitHub issue with a workflow and completion criteria.
2. Inspect the working tree. Keep unrelated edits.
3. Use a task branch and a PR. The branch prefix names the harness: `claude/`, `codex/` or `agy/`.
4. Change the spec before the code. Cite clause IDs in vectors, code and PRs.
5. Write expected outcomes before you run an implementation against them.

After each change, run:

```sh
make check
```

The `check` target in the [Makefile](Makefile) runs the spec check, the conformance tests, the tests in `tests/`, both runners and the project check. The store must pass all vectors. The null implementation must still fail with exit 1.

After an intended source edit, regenerate the inventory with `python3 -B tools/check_project.py --write-checksums`. Examine the diff of `checksums.json`, then run the project check again. The inventory records bytes. It does not validate behavior or approve a publication.

Keep scratch work, stores, archives and raw runs outside the checkout and outside synced folders, for example iCloud.

## Review

A harness does not review its own change. Ask the other harness for a review in a PR comment on GitHub: Claude Code writes `@codex review`. Codex and agy write `@claude review this PR. Use the Review guidelines in AGENTS.md.` If that reviewer is not available, run the local agy fallback, `python3 -B tools/cross_review.py`. A security-relevant change also needs a security review. [docs/REVIEW.md](docs/REVIEW.md) owns the request rules, the fallback, fork PR acceptance and the CI scope.

## Review guidelines

Flag these as high priority in a PR review:

- A dependency that does not come from a typed reference field.
- A status that changes unknown, missing or unsupported into a value.
- A vector whose expected outcome came from an implementation run.
- A refusal vector with no positive pair.
- Code that loads or executes a stored artifact.
- A name of a real product, brand, customer or person in the spec, the vectors or the fixtures.
- Private material, credentials or generated binaries.
- A removed failure case.

End a code review with one line. If `.github/gates.json` or the shared categories of the gates mark a changed path as security-relevant, write `Security review: needed`, with the reason. If not, write `Security review: not needed`. A security review does not end with this line.

## Records and publication

The governance block of the [constitution](CONSTITUTION.md#governance) owns the approval rule for each tag, release, package publication and outreach. An engineering task does not approve a promotion or a change to a different repository. Do not claim speed, retrieval quality or production guarantees without evidence. Report security concerns as [SECURITY.md](SECURITY.md) says.

## Writing

Docs, issues, commits and comments use ASD-STE100 Simplified Technical English. Normative clauses use RFC 2119 keywords. Do not use em dashes or filler words.
