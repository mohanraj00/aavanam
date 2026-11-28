"""Each page in docs/operations/ has an example that runs after the README quickstart and prints the output that the page shows (issue #162)."""
from __future__ import annotations

import contextlib
import io
import re
import unittest

from support import ROOT

OPERATIONS = ROOT / 'docs/operations'
CODE = re.compile(r'```python\n(.*?)```\n\nOutput:\n\n```text\n(.*?)```', re.S)


def quickstart() -> str:
    text = (ROOT / 'README.md').read_text(encoding='utf-8')
    return re.search(r'```python\n(.*?)```', text, re.S).group(1)


class OperationDocsTest(unittest.TestCase):
    def test_each_page_has_an_example_that_runs(self):
        setup = compile(quickstart(), 'README.md', 'exec')
        pages = sorted(OPERATIONS.glob('*.md'))
        self.assertGreater(len(pages), 1)
        for page in pages:
            with self.subTest(page=page.name):
                examples = CODE.findall(page.read_text(encoding='utf-8'))
                self.assertTrue(examples, 'the page has no example with an output')
                for code, expected in examples:
                    scope = {'__name__': 'operation_docs'}
                    with contextlib.redirect_stdout(io.StringIO()):
                        exec(setup, scope)
                    out = io.StringIO()
                    with contextlib.redirect_stdout(out):
                        exec(compile(code, page.name, 'exec'), scope)
                    self.assertEqual(out.getvalue(), expected)

    def test_each_operation_of_the_store_has_a_page(self):
        source = (ROOT / 'src/aavanam/store.py').read_text(encoding='utf-8')
        operations = set(re.findall(r'\n    def op_(\w+)\(', source))
        pages = {page.stem for page in OPERATIONS.glob('*.md')} - {'README'}
        self.assertEqual(operations, pages)
        index = (OPERATIONS / 'README.md').read_text(encoding='utf-8')
        for name in operations:
            self.assertIn(f'[`{name}`]({name}.md)', index)

    def test_each_page_states_its_release(self):
        for page in sorted(OPERATIONS.glob('*.md')):
            with self.subTest(page=page.name):
                self.assertRegex(page.read_text(encoding='utf-8'), r'This page describes release \d+\.\d+\.\d+\.')


if __name__ == '__main__':
    unittest.main()
