"""The tables in src/aavanam agree with the tables of the spec documents."""
from __future__ import annotations

import unittest

import support  # noqa: F401  (paths)
import check_spec
from aavanam import kinds, statuses


class TablesTest(unittest.TestCase):
    def test_kind_table(self):
        schemas, targets = check_spec.schemas(), check_spec.targets()
        self.assertEqual({f: v[:2] for f, v in kinds.COMMON.items()}, schemas[''])
        self.assertEqual(set(kinds.KINDS), set(schemas) - {''})
        for kind, fields in kinds.KINDS.items():
            self.assertEqual({f: v[:2] for f, v in fields.items()}, schemas[kind], kind)
            for field, (vtype, _, permitted) in fields.items():
                self.assertEqual(set(permitted), targets.get((kind, field), set()), (kind, field))

    def test_reason_and_reserved_tables(self):
        self.assertEqual(statuses.REASON_STATUS, check_spec.reason_statuses())
        self.assertEqual(statuses.RESERVED, check_spec.reserved())


if __name__ == '__main__':
    unittest.main()
