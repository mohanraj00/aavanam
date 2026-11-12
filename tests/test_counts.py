"""The stated counts of README.md, docs/STATUS.md and CHANGELOG.md agree with the checkout (issue #70)."""
from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from support import ROOT

sys.path.insert(0, str(ROOT / 'tools'))
import counts  # noqa: E402


class CountsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.counts = counts.actual(ROOT)

    def test_the_stated_counts_agree_with_the_checkout(self):
        self.assertEqual(counts.stated_problems(ROOT, self.counts), [])

    def test_a_wrong_count_is_a_problem(self):
        for key in self.counts:
            with self.subTest(count=key):
                problems = counts.stated_problems(ROOT, {**self.counts, key: self.counts[key] + 1})
                self.assertTrue(problems)

    def test_a_changed_or_absent_statement_is_a_problem(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'docs').mkdir()
            for name in ('README.md', 'docs/STATUS.md'):
                shutil.copyfile(ROOT / name, root / name)
            readme = (root / 'README.md').read_text(encoding='utf-8')
            (root / 'README.md').write_text(readme.replace(f'| {self.counts["vectors"]}.', '| 1.'), encoding='utf-8')
            self.assertEqual(counts.stated_problems(root, self.counts),
                             [f'README.md: states 1 for vectors, but the count is {self.counts["vectors"]}'])
            (root / 'docs/STATUS.md').write_text('# Status\n', encoding='utf-8')
            self.assertEqual(len(counts.stated_problems(root, self.counts)), 2)

    def test_the_released_counts_agree_with_their_tags(self):
        problems = counts.released_problems(ROOT)
        if problems is None:
            self.skipTest('the checkout has no release tag')
        self.assertEqual(problems, [])

    def test_a_wrong_released_count_is_a_problem(self):
        if counts.released_problems(ROOT) is None:
            self.skipTest('the checkout has no release tag')
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder) / 'copy'
            subprocess.run(['git', 'clone', '-q', '--no-checkout', str(ROOT), str(root)], check=True)
            text = (ROOT / 'CHANGELOG.md').read_text(encoding='utf-8')
            (root / 'CHANGELOG.md').write_text(text.replace('passes 400 conformance', 'passes 401 conformance'),
                                               encoding='utf-8')
            self.assertEqual(counts.released_problems(root),
                             ['CHANGELOG.md: states 401 vectors for v0.1.0, but the tag has 400'])


if __name__ == '__main__':
    unittest.main()
