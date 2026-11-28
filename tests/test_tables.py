"""The tables in src/aavanam agree with the tables of the spec documents."""
from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import support  # noqa: F401  (paths)
import check_spec
from aavanam import kinds, statuses, store


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
        self.assertEqual(kinds_of_profile, {'layer', 'retrieval-index'})
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
            wanted = ('aavanam/layer/v1' if any(c.startswith('LYR-') for c in vector['clauses'])
                      else 'aavanam/semantic-index/v1' if any(c.startswith('IDX-') for c in vector['clauses']) else check_spec.BASE_PROFILE)
            self.assertEqual(vector['profile'], wanted, name)

    def test_a_vector_with_a_different_profile_is_a_problem(self):
        source = json.loads((check_spec.VECTORS / 'layers/open-ok.json').read_text())
        for profile, clauses in ((check_spec.BASE_PROFILE, source['clauses']), ('aavanam/layer/v1', ['CTX-OPN-1'])):
            with tempfile.TemporaryDirectory() as folder:
                (Path(folder) / 'one.json').write_text(json.dumps({**source, 'profile': profile, 'clauses': clauses}))
                problems = check_spec.check(Path(folder))
            self.assertEqual(len(problems), 1, problems)
            self.assertIn('profile is not', problems[0])

    def test_operation_and_capability_tables_of_a_profile(self):  # IDX-PRF-4, RSV-LST-4
        index = 'aavanam/semantic-index/v1'
        self.assertEqual(check_spec.profiles()['IDX'], index)
        self.assertEqual(check_spec.operations()['semantic_search'], {'unsupported'})  # the row of document 03 does not change
        self.assertEqual(check_spec.operations('aavanam/layer/v1'), check_spec.operations())
        with_profile = check_spec.operations(index)
        self.assertEqual(with_profile['semantic_search'], set(check_spec.STATUS_SET))
        self.assertEqual({op: s for op, s in with_profile.items() if op != 'semantic_search'},
                         {op: s for op, s in check_spec.operations().items() if op != 'semantic_search'})
        self.assertEqual(check_spec.capabilities(), store.CAPABILITIES)
        self.assertEqual(check_spec.capabilities(index), {**store.CAPABILITIES, **store.PROFILE_CAPABILITIES[index]})
        self.assertEqual(statuses.RESERVED['semantic_search'], index)  # RSV-LST-4: the row stays in the reserved profile table
        self.assertEqual(store.PARTIAL, {op for op, s in with_profile.items() if 'partial' in s})

    def test_a_wrong_profile_operation_table_is_a_problem(self):  # IDX-PRF-4, RSV-LST-3
        text = (check_spec.CONTRACTS / '24-retrieval-indexes.md').read_text()
        cases = (('| `semantic_search` | 24 |', '| `lease_acquire` | 24 |', 'not a reserved operation of the profile'),
                 ('| `semantic_search` | 24 |', '| `semantic_search` | 16 |', 'a different document'),
                 ('| `aavanam/semantic-index/v1` | `semantic_search` |', '| `aavanam/text/v1` | `semantic_search` |', 'is a base capability'),
                 ('| `aavanam/semantic-index/v1` | `semantic_search` |', '| `aavanam/semantic-index/v1` | `search` |', 'not in the operation table of the profile'))
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(check_spec, 'CONTRACTS', Path(folder)):
            for old, new, problem in cases:  # a copy of the document: the checkout does not change
                self.assertEqual(text.count(old), 1, old)
                (Path(folder) / '24-retrieval-indexes.md').write_text(text.replace(old, new))
                self.assertTrue(any(problem in e for e in check_spec.check_profile_tables()), problem)
            (Path(folder) / '24-retrieval-indexes.md').write_text(text)
            self.assertEqual(check_spec.check_profile_tables(), [])

    def test_reason_and_reserved_tables(self):
        self.assertEqual(statuses.REASON_STATUS, check_spec.reason_statuses())
        self.assertEqual(statuses.RESERVED, check_spec.reserved())



def documents() -> dict[str, tuple[str, str]]:
    """The prefix, the name and the text of each contract document of the checkout."""
    out = {}
    for path in sorted(check_spec.CONTRACTS.glob('*.md')):
        text = path.read_text()
        out[check_spec.HEADER.search(text)[2]] = (path.name, text)
    return out


class StatesTest(unittest.TestCase):  # SPEC.md, "Document states"
    LOCKS = '10-consumer-input-locks.md'
    ROW = '| `EXE` | Execution input locks |'

    def setUp(self):
        self.documents, self.readme = documents(), (check_spec.ROOT / 'README.md').read_text()

    def changed(self, prefix: str, old: str, new: str) -> dict[str, tuple[str, str]]:
        name, text = self.documents[prefix]
        self.assertEqual(text.count(old), 1, old)
        return {**self.documents, prefix: (name, text.replace(old, new))}

    def test_each_document_is_normative_and_no_topic_is_draft(self):
        self.assertEqual(len(self.documents), 25)
        self.assertEqual(check_spec.check_states(self.documents, self.readme), [])

    def test_an_unknown_state_is_a_problem(self):
        for state, problem in (('final', 'unknown state final'), ('draft', 'the state is draft, not normative'),
                               ('stub', 'the state is stub, not normative')):
            documents = self.changed('CRD', 'State: normative.', f'State: {state}.')
            self.assertEqual(check_spec.check_states(documents, self.readme), [f'20-content-reads-and-text-search.md: {problem}'])

    def test_a_draft_topic_needs_the_list_of_the_compatibility_statement(self):
        marked = self.changed('LCK', self.ROW, self.ROW[:-2] + ' (draft) |')
        self.assertEqual(check_spec.check_states(marked, self.readme),
                         ['LCK-EXE: a draft topic that the compatibility statement does not list'])
        listed = self.readme.replace('The topics in the `draft` state: none.', 'The topics in the `draft` state: `LCK-EXE`.')
        self.assertNotEqual(listed, self.readme)
        self.assertEqual(check_spec.check_states(marked, listed), [])
        self.assertEqual(check_spec.check_states(self.documents, listed),
                         ['README.md: LCK-EXE is listed as a draft topic, but its document does not mark it'])

    def test_a_readme_with_no_list_of_draft_topics_is_a_problem(self):
        for readme in (self.readme.replace('## 0.6 compatibility', '## 0.5 compatibility'),
                       self.readme.replace('The topics in the `draft` state: none.', '')):
            self.assertEqual(check_spec.check_states(self.documents, readme),
                             ['README.md: the 0.6 compatibility statement does not list the draft topics'])
        other = self.readme.replace('The topics in the `draft` state: none.', 'The topics in the `draft` state: some.')
        self.assertEqual(check_spec.check_states(self.documents, other),
                         ['README.md: the list of draft topics is not "none" or a list of topics'])


if __name__ == '__main__':
    unittest.main()
