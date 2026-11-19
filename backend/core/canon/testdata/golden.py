"""Write fixtures.json: each descriptor text of the fixture vectors, with its canonical form and its revision.
The Go test of package canon compares its results with this file.

The expected values come from the conformance runner. The runner computes the normal form, the canonical
form and the revision by itself, from the tables of the specification (IDN-CAN-1, IDN-CAN-2, KND-SCH-3,
KND-SCH-5), and no implementation gives them. The reference implementation only runs the vectors, so that
each reference in a descriptor has its value. This script also stops if the reference implementation gives
a different canonical form or revision.

Run from the root of the repository: python3 -B backend/core/canon/testdata/golden.py
"""
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[4]
sys.path[:0] = [str(ROOT / 'conformance'), str(ROOT / 'src')]

import runner  # noqa: E402
from aavanam import descriptors, jsontext  # noqa: E402
from store_adapter import StoreAdapter  # noqa: E402


SCHEMAS = runner.check_spec.schemas()


class Recorder(StoreAdapter):
    seen: dict = {}

    def call(self, operation, request, store='writer'):
        result = super().call(operation, request, store)
        if operation == 'put' and isinstance(request.get('descriptors'), list):
            for text, item in zip(request['descriptors'], result.get('items', [])):
                if item.get('status') == 'ok' and isinstance(text, str):
                    value = json.loads(text)
                    row = {'text': text, 'canonical': runner.canonical(runner.normal(value, SCHEMAS)),
                           'ref': runner.revision(value, SCHEMAS)}
                    assert jsontext.canonical(descriptors.check(jsontext.parse(text))) == row['canonical'], text
                    assert item['ref'] == row['ref'], text
                    self.seen[text] = row
        return result


adapter = Recorder()
for vid, vector in sorted(runner.load().items()):
    if 'fixture' in vector:
        problems = runner.run_vector(adapter, vid, vector)
        assert not problems, (vid, problems)
rows = sorted(Recorder.seen.values(), key=lambda row: row['ref'])
out = Path(__file__).with_name('fixtures.json')
out.write_text(json.dumps(rows, ensure_ascii=False, indent=0) + '\n', encoding='utf-8')
print(len(rows), 'descriptors')
