"""The tables in src/aavanam agree with the tables of the spec documents."""
from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

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

    def test_profile_tables(self):  # LYR-PRF-2, LYR-PRF-3, KND-REG-2
        self.assertEqual(check_spec.check_profile_tables(), [])
        self.assertEqual(check_spec.profiles()['LYR'], 'aavanam/layer/v1')
        self.assertEqual(check_spec.profiles()['KND'], check_spec.BASE_PROFILE)
        kinds_of_profile = {row[0] for _, row in check_spec.profile_rows(check_spec.PROFILE_KINDS, 3)}
        self.assertEqual(kinds_of_profile, {'layer'})
        self.assertEqual(check_spec.schemas()['build-lock']['view'], ('ref', '0..n'))
        self.assertEqual(check_spec.targets()[('execution-lock', 'view')], {'snapshot', 'layer'})
        # The tables of document 02 and document 03 do not have the kind, the field or the reasons of the profile.
        base = check_spec.table(check_spec.REGISTRY.read_text(), check_spec.FIELD_LEAD, 5, 'registry', [])
        self.assertFalse([row for row in base if row[0] == 'layer' or row[1] == 'view'])
        reasons = check_spec.table(check_spec.STATUSES.read_text(), 'The reason table:', 3, 'statuses', [])
        self.assertFalse([row for row in reasons if row[0].startswith('layer_')])

    def test_a_vector_has_the_profile_of_its_clauses(self):
        vectors = {v.relative_to(check_spec.VECTORS).as_posix(): json.loads(v.read_text()) for v in check_spec.VECTORS.rglob('*.json')}
        for name, vector in vectors.items():
            wanted = 'aavanam/layer/v1' if any(c.startswith('LYR-') for c in vector['clauses']) else check_spec.BASE_PROFILE
            self.assertEqual(vector['profile'], wanted, name)

    def test_a_vector_with_a_different_profile_is_a_problem(self):
        source = json.loads((check_spec.VECTORS / 'layers/open-ok.json').read_text())
        for profile, clauses in ((check_spec.BASE_PROFILE, source['clauses']), ('aavanam/layer/v1', ['CTX-OPN-1'])):
            with tempfile.TemporaryDirectory() as folder:
                (Path(folder) / 'one.json').write_text(json.dumps({**source, 'profile': profile, 'clauses': clauses}))
                problems = check_spec.check(Path(folder))
            self.assertEqual(len(problems), 1, problems)
            self.assertIn('profile is not', problems[0])

    def test_reason_and_reserved_tables(self):
        self.assertEqual(statuses.REASON_STATUS, check_spec.reason_statuses())
        self.assertEqual(statuses.RESERVED, check_spec.reserved())


if __name__ == '__main__':
    unittest.main()
