"""The worked example in README.md runs and prints the output that the README shows."""
from __future__ import annotations

import contextlib
import io
import re
import unittest

from support import ROOT


class ReadmeTest(unittest.TestCase):
    def test_worked_example(self):
        text = (ROOT / 'README.md').read_text(encoding='utf-8')
        code = re.search(r'```python\n(.*?)```', text, re.S).group(1)
        expected = re.search(r'Output:\n\n```text\n(.*?)```', text, re.S).group(1)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            exec(compile(code, 'README.md', 'exec'), {'__name__': 'readme'})
        self.assertEqual(out.getvalue(), expected)


if __name__ == '__main__':
    unittest.main()
