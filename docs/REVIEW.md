# Review and merge rules

These rules apply to every change to `main`. [AGENTS.md](../AGENTS.md) owns the general workflow.

## Protected main

- Every change goes through a pull request. Direct pushes, force pushes and branch deletion are blocked.
- A PR needs one approval from a code owner. A new push removes earlier approvals.
- The required checks are `ci-ok`, `cross-review`, `security-review` and `acceptance`. The shared gates of `mohanraj00/.github` supply the last three. `.github/workflows/gates.yml` pins the gates action to a full commit SHA.
- The maintainer is the only code owner. GitHub does not let an author approve their own PR, so the maintainer merges their own PRs with the admin bypass. The cross-harness review below replaces the approval for these PRs.

## Human contributors

A contributor does not need Codex, Claude Code or agy. [CONTRIBUTING.md](../CONTRIBUTING.md) gives the steps. For a PR without a harness, the maintainer asks for the code review and the security review as the sections below show. If no harness is available, the maintainer does a manual code review, as the local fallback section says.

## Cross-harness review

A harness does not review its own change. The branch prefix names the harness that wrote the change: `claude/`, `codex/` or `agy/`. The reviews run on GitHub. No harness reviews a PR by default, so the PR author asks in a PR comment.

| Builder | Code review comment | Security review comment |
|---|---|---|
| Claude Code | `@codex review` | `@codex security review` |
| Codex | `@claude review this PR. Use the Review guidelines in AGENTS.md.` | `@claude do a security review of this PR. Use the Review guidelines in AGENTS.md.` |
| agy | `@claude review this PR. Use the Review guidelines in AGENTS.md.` | `@claude do a security review of this PR. Use the Review guidelines in AGENTS.md.` |

Follow these rules for each request:

1. Ask when CI is green and the PR is ready.
2. For a Codex code review request, write `Head: <sha>` on the next line of the comment, with the short SHA of the PR head commit. Do not add this line to a Claude request. The Claude review starts only for the exact text in the table, and its comment names the head that it reviewed.
3. Write a security review request as the exact text in the table, with no other text in the comment. If the comment has other text, for example a `Head:` line, Codex does a code review and no security review. The Codex result names the commit that it reviewed.
4. Ask one time for each head commit. Before you ask, read the PR comments. If a request names the current head, or comes after the last push, do not ask again.
5. If you push fixes after a review, ask again for the new head. Ask at most 3 times for each PR. After that, ask the maintainer to review.
6. Fix each real finding, or write on the PR why it is not a defect.

The `cross-review` check passes only for a review of the current head from the other harness. A Claude-built PR needs a completed Codex code review. A Codex-built or agy-built PR needs a Claude review marker with `findings=0`. If the review came from the agy fallback or from the maintainer, the maintainer adds the `cross-reviewed` label. The label counts only with an approval record for the current head and base.

Codex reviews through the Codex GitHub integration. Claude Code reviews through the [Claude review](../.github/workflows/claude-review.yml) workflow. The workflow starts only if all of these are true:

- The comment is on a PR.
- The comment body is exactly one of the two Claude request texts in the table. GitHub compares these texts with no regard to letter case.
- The commenter is an owner, a member or a collaborator.

A comment or a review with other text does not start the workflow. The comment text selects one of two fixed prompts, and it never goes into a prompt.

### Claude review design

An issue comment event reads the workflow from `main`, so a PR cannot change this workflow for its own review. The workflow has two jobs.

The `review` job has `contents: read` and `pull-requests: read` only. It uses the `claude-review` environment, which holds the `CLAUDE_CODE_OAUTH_TOKEN` secret. Only the CLI step gets the secret. The environment needs a deployment branch rule for `main`. The job does these steps:

1. It checks out `main` and fetches the PR commits as Git objects only. It never checks out PR files, so the PR cannot supply instructions, settings or code.
2. It reads the base branch of the PR from the API and fetches it. It writes the diff with `git diff --no-ext-diff --no-textconv -U20 pr/base...pr/head`. If the diff is empty, the job fails.
3. It installs one exact version of the Claude Code CLI and checks the SHA-512 value of the package.
4. It runs the CLI in restricted mode with only the `Read`, `Grep` and `Glob` tools, no MCP server, no slash command and a turn limit. These tools cannot read `/proc`. The prompt says that the diff is untrusted data.
5. It checks the JSON result: `type` is `result`, `subtype` is `success`, `is_error` is false, `result` is not empty and `permission_denials` is empty. If a check fails, the job fails.
6. It keeps the review text and the head SHA in an artifact.

The `post` job has `contents: read`, `issues: write` and `pull-requests: write` only, with no secret and no environment. A comment on a PR needs `pull-requests: write`. It runs [tools/claude_review.py](../tools/claude_review.py) from the `main` checkout. This file is a copy of `gates/claude_review.py` of `mohanraj00/.github`. The comment starts with a fixed marker: `<!-- claude-review:v1 kind=<code|security> head=<sha> findings=<count|unknown> run=<run id> -->`. The script writes the marker outside the model text. The count comes from the first line of the model text, `Findings: <number>`. If that line is not valid, the count is `unknown`. If the `review` job failed, the comment holds only `NO VALID CLAUDE REVIEW` and the head SHA.

The `post` job writes as the workflow bot, and the gates trust approval records and review markers from this bot. Thus the script treats the model text as untrusted. The model text cannot make a marker or an approval record. It escapes `&` and `<`, changes each `@` to a character that makes no mention, and limits the length.

### Use of a Claude result

The `cross-review` and `security-review` gates read the marker. They accept a marker only from an `issue_comment` run of this workflow on the default branch, for the current head, with `findings=0`. A person also reads the text. The review has no inline comments and no conversation. A push makes a new head, so ask again as the request rules say. The OAuth token is a static secret. If you suspect a leak, revoke it and make a new one.

### Local fallback: agy

If the GitHub reviewer is not available (no quota, an outage or no integration), run agy on your machine:

```sh
python3 -B tools/cross_review.py --work "$REVIEW_WORK"
```

Post the report on the PR. It starts with `Review (agy <version>):` and names the base and head commits. If agy is not available, ask the maintainer for a manual review. If the PR needs a security review, the maintainer also does a manual security review, as the [Security review](#security-review) section says.

The PR is untrusted input to agy:

- agy runs headless and with `--sandbox` in an empty directory. It gets a new HOME that holds only its login token and an empty allow list. Thus, your global agy allow rules and MCP servers do not apply, and the PR cannot supply settings or instruction files.
- The prompt quotes `CONSTITUTION.md` and `AGENTS.md` from the base revision, not from the PR.
- agy gets only basic environment variables and its own login key. The script also sets `SSH_CONNECTION`. If agy finds an SSH session, it uses only the file token and does not open the macOS keychain. agy does not document this behavior, so the script checks the agy log (`<work>/<kind>/agy.log`). The script needs a file token at `~/.gemini/antigravity-cli/antigravity-oauth-token` or an API key. If it has neither, it stops before agy starts. The report gives a warning if agy did not use file-only storage, or if agy changed the refresh token. agy output never changes your token file. Read the report before you post it, and remove anything that is not a review finding.
- The tool resolves the base and head to commit SHAs once. If HEAD changes during the review, the report says so and the tool exits 1.
- If the diff is larger than 90000 bytes, the tool cuts it and says so in the report. Get a manual review of the rest.
- The full output and exit status stay in the work directory, which must be outside the checkout.

## Security review

The gates action owns the shared security categories. [.github/gates.json](../.github/gates.json) adds the project paths. The list includes the workflows, the tools, the reference implementation, the conformance runner and adapters, and the agent instruction files.

If a PR changes one of these paths, the `security-review` check fails until the current head has a security review with no open finding. A move counts both its old and new path. Agent instruction files and settings count at any directory depth. To pass the check:

1. Ask for the security review in a PR comment, as the table above shows.
2. Fix each finding, push, and ask again for the new head.
3. If the Codex security review or the Claude security review of the current head finds no security issues, the check passes. You do not add a label.

The [Gates rerun](../.github/workflows/gates-rerun.yml) workflow starts the failed gate again when Codex posts a result, or when the Claude review workflow completes. If the gate run is in progress, the workflow waits for it to complete. GitHub keeps the failed attempt in the run history. The gate trusts only results that the Codex app posts, and only for the commit that the result names. A push makes a new head, so the gate needs a new result. A result names a commit, not a base. Thus, the gate ignores results posted before the last change of the PR base branch. A clean result is a model output, not a proof. A PR can contain text that tries to steer the reviewer. If a change looks wrong to you, do not merge it.

The `security-reviewed` label is only for the cases that a clean Codex result cannot show:

- Codex is not available, and the review came from the agy fallback: `python3 -B tools/cross_review.py --security yes --work "$REVIEW_WORK"`. Post the report.
- A finding is not a defect, and the PR has an answer for it.
- Codex and agy are both not available, and the maintainer did a manual security review. The maintainer posts the review on the PR. The review names the head commit and the base commit, and gives each finding with its fix or its answer.

In these cases, the maintainer adds the `security-reviewed` label. The label records a fact: the current head has a security review, and each finding has a fix or an answer. It is not a separate review. When the maintainer adds the label, the workflow posts an approval record with the PR head and base at that time. The label counts only while the PR has that head and base. After a push or a base change, the next run removes the label.

The `Gates` workflow uses the `pull_request_target` event. GitHub runs that workflow from the base branch. It checks out the base SHA, so `.github/gates.json` and the PR template come from the base branch, and a PR cannot change its own gate. The gates action comes from a pinned SHA. The workflow reads only the PR file list, the labels, its own approval records and the review results. It never checks out or runs the PR head. Do not change this.

The workflow posts the approval records with the workflow token. A fork PR gets a read-only token, so it cannot write a record. A user with write access can change a workflow to write one, so the gates trust writers.

## PRs from forks

GitHub does not run workflows for a PR from an outside contributor until a maintainer approves the run. Before you approve the run, read the workflow and tool changes. Do not approve a run that changes `.github/` or `tools/` until you understand the change.

The `acceptance` check fails for a fork PR until the PR has the `accepted` label. The label counts only for the head and base in its approval record, as for the security label. Before you add the label, make sure that:

- the PR links an issue with a concrete workflow and completion criteria;
- the spec, the vectors and the fixtures name no real product, brand, customer or person;
- the change contains no private application material, credentials, customer data or generated binaries;
- `src/` still imports only the Python standard library and SQLite;
- the PR keeps the Apache-2.0 terms;
- the cross-harness review and, if needed, the security review are posted.

If the contributor pushes again, read the new commits before you approve the run and add the label again.

## CI scope

The `CI` workflow uses the `ci-ok` skeleton of the shared gates. It runs the `shared-project` job on every PR and every push to `main`. That job runs the project check, the spec check and the tests of the conformance runner. Thus each contract or vector change gets the spec check.

The `suite` job runs for each change that is not docs-only. The `changes` job reads `.github/gates.json` from the base branch. A code, security-relevant or level 1 path is not docs. The code paths are `src/`, `conformance/`, `fixtures/`, `tests/`, `tools/` and the CI workflow. It runs on two runners, `ubuntu-24.04` and `macos-15`. On each runner, it runs the tests of the reference implementation and all vectors against the store. It also runs the null implementation, which must exit with 1. The two runs are independent: a failure on one runner does not stop the other run. The `ci-ok` job passes if each needed job passed or was skipped. Thus `ci-ok` fails if the suite fails on either runner.

The `CI` workflow uses the `pull_request` event and runs the PR code without secrets. Thus a PR can change its own CI scope. The trusted `security-review` gate covers this: a change to `.github/` or `tools/` needs a security review.
