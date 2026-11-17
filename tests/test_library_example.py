"""The library example of examples/library_hours/ (issue #83).

The example needs groundgate 0.15.0, from examples/library_hours/requirements.txt. Without it, the run test skips,
because the reference implementation uses only the standard library (constitution principle 9). The package job of CI
installs the requirements in the clean environment of the wheel and sets AAVANAM_LIBRARY_EXAMPLE=1, so an absent
package fails there. AAVANAM_INSTALLED=1 makes the example import aavanam from that environment.
"""
from __future__ import annotations

import importlib.util
import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from importlib import metadata

from support import ROOT

FOLDER = ROOT / 'examples' / 'library_hours'
INSTALLED = os.environ.get('AAVANAM_INSTALLED') == '1'
REQUIRED = os.environ.get('AAVANAM_LIBRARY_EXAMPLE') == '1'
PINNED = '0.15.0'


def expected_output() -> str:
    text = (FOLDER / 'README.md').read_text(encoding='utf-8')
    return re.search(r'Output of `library_hours\.py`:\n\n```text\n(.*?)```', text, re.S).group(1)


def stand_in(path) -> dict:
    return json.loads(path.read_text(encoding='utf-8'))


class StandInTest(unittest.TestCase):
    def test_the_record_has_the_layout_of_the_seam_stand_in(self):
        example, seam = stand_in(FOLDER / 'activation_record_stand_in.json'), stand_in(ROOT / 'tests/activation_record_stand_in.json')
        self.assertIs(example['stand_in'], True)  # change this test when a pinned release replaces the stand-in
        self.assertEqual(example['format'], seam['format'])
        fields = {frozenset(record) for record in seam['records'].values()}
        self.assertEqual(len(fields), 1)
        record = example['records']['first']
        self.assertEqual(frozenset(record), fields.pop())
        self.assertEqual(record['partition'], 'sample-library-hours')  # PUB-ACT-3 check 2
        self.assertLess(record['from'], record['until'])  # a half-open interval

    def test_the_requirements_pin_no_unreleased_version(self):
        lines = [line for line in (FOLDER / 'requirements.txt').read_text(encoding='utf-8').splitlines()
                 if line and not line.startswith('#')]
        self.assertEqual(lines, [line for line in (ROOT / 'tests/requirements-seam.txt').read_text(encoding='utf-8')
                                 .splitlines() if line and not line.startswith('#')])


class LibraryExampleTest(unittest.TestCase):
    def setUp(self):
        if importlib.util.find_spec('groundgate') is None:
            if REQUIRED:
                self.fail('AAVANAM_LIBRARY_EXAMPLE=1 needs examples/library_hours/requirements.txt')
            self.skipTest('groundgate is not installed')
        self.assertEqual(metadata.version('groundgate'), PINNED)

    def test_the_example_shows_the_refused_and_the_accepted_publish(self):
        env = {k: v for k, v in os.environ.items() if not k.startswith('PYTHON')}
        flags = ['-I', '-B']  # -I: no PYTHONPATH, no user site and no script folder on sys.path
        if not INSTALLED:
            flags, env['PYTHONPATH'] = ['-B'], str(ROOT / 'src')
        with tempfile.TemporaryDirectory() as cwd:  # a folder with no aavanam source in it
            done = subprocess.run([sys.executable, *flags, str(FOLDER / 'library_hours.py')],
                                  cwd=cwd, env=env, capture_output=True, text=True, timeout=300)
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertIn("publish with no activation: incompatible ['activation_required'] head None\n", done.stdout)
        self.assertIn('publish with the activation: ok published head is the candidate snapshot True\n', done.stdout)
        self.assertEqual(done.stdout, expected_output())


if __name__ == '__main__':
    unittest.main()
