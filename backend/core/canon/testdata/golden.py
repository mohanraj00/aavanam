"""Write fixtures.json: each descriptor text of the fixture vectors, with the canonical form and the revision
that the reference implementation gives. The Go test of package canon compares its results with this file.

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


class Recorder(StoreAdapter):
    seen: dict = {}

    def call(self, operation, request, store='writer'):
        result = super().call(operation, request, store)
        if operation == 'put' and isinstance(request.get('descriptors'), list):
            for text, item in zip(request['descriptors'], result.get('items', [])):
                if item.get('status') == 'ok' and isinstance(text, str):
                    normal = descriptors.check(jsontext.parse(text))
                    assert jsontext.revision(normal) == item['ref']
                    self.seen[text] = {'text': text, 'canonical': jsontext.canonical(normal), 'ref': item['ref']}
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
