"""Make the clause, document and vector counts from the spec and the vector folder, and check each stated count.

tools/check_project.py calls problems(). A stated count is a count in README.md, docs/STATUS.md or CHANGELOG.md.
The count of a released version in CHANGELOG.md comes from its tag. Without the tag in the checkout, the check
of that line does not run. The check does not run the null implementation of a tag. tests/test_counts.py checks it when the tag is present (issue #70).
"""
from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path

CLAUSE = re.compile(r'^\*\*([A-Z]{3})-([A-Z]{2,4})-([1-9][0-9]*)\.\*\*', re.M)  # the CLAUSE of tools/check_spec.py
STATE = re.compile(r'^State: (\w+)\.', re.M)
RESULT = re.compile(r'^(\d+) passed, (\d+) failed$', re.M)


def vector_files(root: Path, tree: str | None = None) -> list[str]:
    """The vector files under conformance/vectors, of the checkout or of the git tree `tree`."""
    if tree is None:
        return sorted(p.relative_to(root).as_posix() for p in (root / 'conformance/vectors').rglob('*.json'))
    names = subprocess.run(['git', '-C', str(root), 'ls-tree', '-r', '--name-only', tree, 'conformance/vectors'],
                           capture_output=True, text=True, check=True).stdout.split()
    return sorted(n for n in names if n.endswith('.json'))


def null_result(root: Path) -> tuple[int, int]:
    """The passed and failed counts of the null implementation."""
    done = subprocess.run([sys.executable, '-B', str(root / 'conformance/runner.py'), '--adapter', 'null_adapter:NullAdapter'],
                          cwd=root, capture_output=True, text=True, timeout=600)
    found = RESULT.findall(done.stdout)
    if not found:
        raise ValueError('the runner gave no result line for the null implementation')
    passed, failed = found[-1]
    return int(passed), int(failed)


def actual(root: Path) -> dict[str, int]:
    """The counts of the checkout."""
    documents = sorted((root / 'docs/contracts').glob('*.md'))
    texts = [d.read_text(encoding='utf-8') for d in documents]
    vectors = vector_files(root)
    passed, failed = null_result(root)
    return {
        'documents': len(documents),
        'drafts': sum(STATE.search(t) is not None and STATE.search(t).group(1) == 'draft' for t in texts),
        'clauses': sum(len(CLAUSE.findall(t)) for t in texts),
        'vectors': len(vectors),
        'acceptance': sum(v.startswith('conformance/vectors/acceptance/') for v in vectors),
        'null_passed': passed,
        'null_failed': failed,
    }


# Each stated count: the file, a pattern with one group for each count, and the names of the counts.
STATED = [
    ('README.md', r'All (\d+) contract documents are drafts\.', ['documents']),
    ('README.md', r'All \d+ contract documents are drafts\.', ['drafts']),
    ('README.md', r'\| Contract clauses \| (\d+), in (\d+) documents under', ['clauses', 'documents']),
    ('README.md', r'\| Conformance vectors \| (\d+)\.', ['vectors']),
    ('README.md', r'\| Null implementation \| Passes (\d+) vectors? and fails (\d+)\.', ['null_passed', 'null_failed']),
    ('README.md', r'\| End-to-end acceptance \| (\d+) vectors\.', ['acceptance']),
    ('docs/STATUS.md', r'The specification has (\d+) contract documents in the `draft` state\.', ['documents']),
]
# A count of a released version: the file, the pattern, and the tag of the version.
RELEASED = [
    ('CHANGELOG.md', r'## 0\.1\.0\n(?:.*\n)*?- The reference store passes (\d+) conformance vectors\. '
                     r'The null adapter fails \d+ and exits with 1\.', 'v0.1.0'),
]


def stated_problems(root: Path, counts: dict[str, int]) -> list[str]:
    """A problem for each stated count that is absent or different from `counts`."""
    problems = []
    for name, pattern, keys in STATED:
        text = (root / name).read_text(encoding='utf-8')
        match = re.search(pattern, text)
        if match is None:
            problems.append(f'{name}: no stated count matches {pattern!r}')
            continue
        if keys == ['drafts']:
            if counts['drafts'] != counts['documents']:
                problems.append(f'{name}: states that all documents are drafts, but {counts["drafts"]} of '
                                f'{counts["documents"]} are drafts')
            continue
        for key, value in zip(keys, match.groups()):
            if int(value) != counts[key]:
                problems.append(f'{name}: states {value} for {key}, but the count is {counts[key]}')
    return problems


def released_problems(root: Path) -> list[str] | None:
    """A problem for each released count that differs from its tag, or None if a tag is not in the checkout."""
    problems = []
    for name, pattern, tag in RELEASED:
        if subprocess.run(['git', '-C', str(root), 'rev-parse', '-q', '--verify', f'refs/tags/{tag}'],
                          capture_output=True).returncode != 0:
            return None
        match = re.search(pattern, (root / name).read_text(encoding='utf-8'))
        if match is None:
            problems.append(f'{name}: no stated count of {tag}')
            continue
        total = len(vector_files(root, tag))
        if int(match.group(1)) != total:
            problems.append(f'{name}: states {match.group(1)} vectors for {tag}, but the tag has {total}')
    return problems


def problems(root: Path) -> list[str]:
    """The problems of the stated counts of the checkout. The released counts need their tags."""
    return stated_problems(root, actual(root)) + (released_problems(root) or [])
