# Aavanam constitution

Version 2, 2026-11-09.

This document has authority over the specification.

## Purpose

Aavanam is a versioned knowledge-store library. It keeps structured documents, typed data, opaque application rule companions, evidence and observations in immutable, citable snapshots. Each consumer reads the same exact inputs from one context.

### Consumers

Primary consumers:

- Response agents that retrieve context for an answer.
- Research agents that need grounded, citable content.
- Execution scripts that consume typed data tables and rule inputs.
- Code emitters that consume structured knowledge, data, rules and evidence.

Human-readable documentation is a secondary consumer. It is a rendered view of the same context. No primary consumer is a later attachment to a text store.

## Boundaries

Applications own emitters, execution engines, rule extensions, rule semantics, source authority, jurisdiction interpretation, effective-date policy, applicability and acceptance decisions.

Aavanam stores and resolves. It keeps opaque artifacts, schemas, runtime and profile declarations, dependency bindings, input locks, provenance and external receipts. It never loads, imports, interprets or executes a stored rule body, host operation or emitter artifact. It reports facts. It does not decide authority, eligibility or applicability.

Applications own their rule languages. Aavanam does not add a rule language. It makes no claim that the primitive callable profile of an application supports numeric, structured or document-publication contracts.

Access control and security policy are outside the current functional scope. A capability declaration states functional availability. It is not authentication.

## Principles

1. **Typed references are the only dependency source.** Each reference field has a permitted set of target kinds. Dependency indexes come from these fields. A second dependency list is not permitted.
2. **Closed statuses.** Each result uses the closed status set in the spec. Unknown, missing, partial and unsupported never become values, defaults or success.
3. **Exact before discovery.** Deterministic lookup returns exact typed values and selectors. Search and models never supply a fallback value.
4. **Immutability.** A changed byte is a new object. Bindings, evidence, metadata, observations and generated outputs stay separate from source content.
5. **Declared completeness is labeled.** An input lock is complete over declared references. Aavanam does not prove semantic completeness, and its results say so.
6. **Native identities stay.** The identities that a producer gives are kept with a checked mapping. Aavanam does not claim that different hash schemes are equal.
7. **Uncertainty stays visible.** Coverage, omissions, unknown denominators and freshness are explicit observations in a stated scope.
8. **Existing storage.** Do not build a database engine. An implementation uses existing storage. The reference implementation uses SQLite and immutable objects.
9. **The reference implementation proves the spec.** The reference implementation in `src/` uses only the Python standard library and SQLite. Its purpose is to show that the spec can be built and that the conformance vectors pass. An implementation that integrates with a host application uses the tech spec of that application. It passes the same conformance vectors.

## Evidence

### Specification and conformance

The specification has authority over the implementation. The conformance vectors have authority over the claims. An expected outcome comes from the spec, a fixture definition or a planted fault, never from an implementation run. A change of scope needs a spec change first.

Keep failures with their vectors. If a vector changes, record the reason and keep the old one in history. Do not claim speed, retrieval quality, maintenance or interoperability advantages without appropriate evidence.

## Contributions

Do not add customer material, private product code, domain packs, private interpretations or evaluation corpora. Fixtures are synthetic and neutral. Review each fixture before its first commit.

## Governance

<!-- governance:start v1 -->
This project follows the project standard in `docs/STANDARD.md`. That file is an exact copy from `mohanraj00/.github`. The standard owns the rules for builds, reviews, approvals and CI. A project exception to the standard is valid only when the standard records it.

The documents own these topics:

- `CONSTITUTION.md`: the purpose, the boundaries, the principles, the evidence rules, the contribution rules and the version log.
- `docs/STANDARD.md`: the builds, the reviews, the approvals and CI.
- `AGENTS.md`: the layout, the check command and the review guidelines.
- `CLAUDE.md` and `GEMINI.md`: imports only.

These rules apply to each change:

1. A harness does not review its own change. The Codex security review is the one exception, as the standard says.
2. The agents use the owner login. An agent never approves a PR and never merges a level 1 PR. An agent never adds the labels `accepted`, `security-reviewed`, `cross-reviewed` or `ready-for-review`.
3. The owner merges each level 1 PR.
4. Each tag, release, package publication, visibility change and outreach step needs a separate owner approval.
<!-- governance:end v1 -->

## Amendment

Change this constitution when the owner changes direction or evidence changes a decision. A change is a level 1 PR. Record the reason in the change, and add an entry to the version log.

Change the governance block only with a new version of the template in `mohanraj00/.github`.

## Version log

### Version 2, 2026-11-09

Align the sections with the shared constitution template `v1`, and add the governance block, the amendment text and this version log. Reason: align with the shared template.

### Version 1, 2026-11-05

The first version of the constitution. Reason: the start of the project.
