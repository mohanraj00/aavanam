"""The PDF example of examples/pdf_facts/ (issue #71).

The example needs groundgate 0.14.0 with its extra `pdf`, from examples/pdf_facts/requirements.txt. Without it, the
run test skips, because the reference implementation uses only the standard library (constitution principle 9).
The package job of CI installs the requirements in the clean environment of the wheel and sets AAVANAM_PDF_EXAMPLE=1,
so an absent package fails there. AAVANAM_INSTALLED=1 makes the example import aavanam from that environment.
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
from pathlib import Path

from support import ROOT

FOLDER = ROOT / 'examples' / 'pdf_facts'
INSTALLED = os.environ.get('AAVANAM_INSTALLED') == '1'
REQUIRED = os.environ.get('AAVANAM_PDF_EXAMPLE') == '1'
PINNED = '0.14.0'
CITATION = {'page', 'span', 'text', 'source', 'text_digest'}
DIGEST = re.compile(r'^sha256:[0-9a-f]{64}$')


def expected_output() -> str:
    text = (FOLDER / 'README.md').read_text(encoding='utf-8')
    return re.search(r'Output of `pdf_facts\.py`:\n\n```text\n(.*?)```', text, re.S).group(1)


def check_citations(stdout: str) -> int:
    """Raise AssertionError if a printed fact has no full citation. Return the number of facts."""
    count = 0
    for line in stdout.splitlines():
        if not line.startswith('fact '):
            continue
        count += 1
        match = re.match(r"^fact (\w+) '([^']*)' (\{.*\})$", line)
        if match is None:
            raise AssertionError(f'a fact line has no citation: {line!r}')
        citation = json.loads(match.group(3))
        if set(citation) != CITATION:
            raise AssertionError(f'a citation has different members: {line!r}')
        start, end = citation['span']
        if not (isinstance(citation['page'], int) and citation['page'] >= 1 and 0 <= start < end):
            raise AssertionError(f'a citation has no page or no span: {line!r}')
        if not citation['text'] or match.group(2) not in citation['text']:
            raise AssertionError(f'the cited text does not contain the value: {line!r}')
        if not (DIGEST.match(citation['source'] or '') and DIGEST.match(citation['text_digest'] or '')):
            raise AssertionError(f'a citation has no source digest or no text digest: {line!r}')
    return count


class PdfFixtureTest(unittest.TestCase):
    def test_the_script_gives_the_committed_pdf_files(self):
        with tempfile.TemporaryDirectory() as folder:
            done = subprocess.run([sys.executable, '-I', '-B', str(FOLDER / 'make_pdf.py'), folder],
                                  capture_output=True, text=True, timeout=60)
            self.assertEqual(done.returncode, 0, done.stderr)
            for name in ('datasheet-v1.pdf', 'datasheet-v2.pdf'):
                self.assertEqual((Path(folder) / name).read_bytes(), (FOLDER / name).read_bytes(), name)

    def test_a_fact_with_no_citation_fails_the_check(self):
        good = expected_output()
        self.assertEqual(check_citations(good), 6)
        line = next(x for x in good.splitlines() if x.startswith('fact '))
        citation = json.loads(line[line.index('{'):])
        planted = [
            line[:line.index(' {')],
            line[:line.index('{')] + json.dumps({k: v for k, v in citation.items() if k != 'page'}),
            line[:line.index('{')] + json.dumps({**citation, 'source': None}),
            line[:line.index('{')] + json.dumps({**citation, 'text_digest': None}),
            line[:line.index('{')] + json.dumps({**citation, 'span': [5, 5]}),
            line[:line.index('{')] + json.dumps({**citation, 'text': ''}),
        ]
        for bad in planted:
            with self.subTest(line=bad), self.assertRaises(AssertionError):
                check_citations(bad + '\n')


class PdfExampleTest(unittest.TestCase):
    def setUp(self):
        if importlib.util.find_spec('groundgate') is None or importlib.util.find_spec('pdfminer') is None:
            if REQUIRED:
                self.fail('AAVANAM_PDF_EXAMPLE=1 needs examples/pdf_facts/requirements.txt')
            self.skipTest('groundgate with its extra pdf is not installed')
        self.assertEqual(metadata.version('groundgate'), PINNED)

    def test_the_example_prints_each_fact_with_its_citation(self):
        env = {k: v for k, v in os.environ.items() if not k.startswith('PYTHON')}
        flags = ['-I', '-B']  # -I: no PYTHONPATH, no user site and no script folder on sys.path
        if not INSTALLED:
            flags, env['PYTHONPATH'] = ['-B'], str(ROOT / 'src')
        with tempfile.TemporaryDirectory() as cwd:  # a folder with no aavanam source in it
            done = subprocess.run([sys.executable, *flags, str(FOLDER / 'pdf_facts.py')],
                                  cwd=cwd, env=env, capture_output=True, text=True, timeout=300)
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertEqual(check_citations(done.stdout), 6)
        self.assertEqual(done.stdout, expected_output())


if __name__ == '__main__':
    unittest.main()
