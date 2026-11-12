"""Each example in examples/ runs and prints the output that examples/README.md shows.

make check runs the examples against src/. The package job of CI sets AAVANAM_INSTALLED=1 and runs this file with
the Python of a clean environment that has only the built wheel. Then the example must import aavanam from that
environment, not from src/.
"""
from __future__ import annotations

import os
import re
import subprocess
import sys
import tempfile
import unittest

from support import ROOT

INSTALLED = os.environ.get('AAVANAM_INSTALLED') == '1'


def expected_outputs() -> dict[str, str]:
    """The output block after each 'Output of <file>:' line in examples/README.md."""
    text = (ROOT / 'examples' / 'README.md').read_text(encoding='utf-8')
    return dict(re.findall(r'Output of `([a-z_]+\.py)`:\n\n```text\n(.*?)```', text, re.S))


class ExamplesTest(unittest.TestCase):
    def run_example(self, name: str) -> subprocess.CompletedProcess:
        env = {k: v for k, v in os.environ.items() if not k.startswith('PYTHON')}
        flags = ['-I', '-B']  # -I: no PYTHONPATH, no user site and no script folder on sys.path
        if not INSTALLED:
            flags, env['PYTHONPATH'] = ['-B'], str(ROOT / 'src')
        with tempfile.TemporaryDirectory() as cwd:  # a folder with no aavanam source in it
            return subprocess.run([sys.executable, *flags, str(ROOT / 'examples' / name)],
                                  cwd=cwd, env=env, capture_output=True, text=True, timeout=120)

    def test_each_example_has_an_output(self):
        files = sorted(p.name for p in (ROOT / 'examples').glob('*.py'))
        self.assertTrue(files)
        self.assertEqual(sorted(expected_outputs()), files)

    def test_examples(self):
        for name, expected in expected_outputs().items():
            with self.subTest(example=name):
                done = self.run_example(name)
                self.assertEqual(done.returncode, 0, done.stderr)
                self.assertEqual(done.stdout, expected)

    @unittest.skipUnless(INSTALLED, 'only in the package job of CI, with the wheel installed')
    def test_import_from_the_wheel(self):
        with tempfile.TemporaryDirectory() as cwd:
            done = subprocess.run([sys.executable, '-I', '-c', 'import aavanam; print(aavanam.__file__)'],
                                  cwd=cwd, capture_output=True, text=True, timeout=60)
        self.assertEqual(done.returncode, 0, done.stderr)
        path = done.stdout.strip()
        self.assertNotIn(str(ROOT / 'src'), path)
        self.assertIn('site-packages', path)


if __name__ == '__main__':
    unittest.main()
