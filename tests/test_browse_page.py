"""The browse page of examples/browse_page/ (issue #62).

The store test needs the backend driver `aavanam-conform`, so it needs Go or AAVANAM_CONFORM_BIN. `make check-backend-mem`
sets AAVANAM_BROWSE_EXAMPLE=1, so a driver that does not start fails there. In other runs, the store test skips if
AAVANAM_CONFORM_BIN is not set. The expected texts come from fixture.json and from the contracts, not from a run.
"""
from __future__ import annotations

import hashlib
import io
import json
import os
import re
import shutil
import sys
import tarfile
import tempfile
import threading
import unittest
import urllib.error
import urllib.parse
import urllib.request

from support import ROOT

FOLDER = ROOT / 'examples' / 'browse_page'
sys.path.insert(0, str(FOLDER))
sys.path.insert(0, str(ROOT / 'conformance'))

import browse  # noqa: E402
import run  # noqa: E402

REQUIRED = os.environ.get('AAVANAM_BROWSE_EXAMPLE') == '1'
FIXTURE = json.loads((FOLDER / 'fixture.json').read_text(encoding='utf-8'))
GUIDE, NOTES = 'sample-field-guide', 'sample-site-notes'
PUMP_2 = 'Grease the bearing every two weeks.'


def kit_archive(files: dict[str, bytes]) -> bytes:
    out = io.BytesIO()
    with tarfile.open(fileobj=out, mode='w') as archive:
        for name, data in files.items():
            info = tarfile.TarInfo(name)
            info.size = len(data)
            archive.addfile(info, io.BytesIO(data))
    return out.getvalue()


class KitTest(unittest.TestCase):
    """The page uses the kit archive only if it has the pinned SHA-256 digest."""

    def setUp(self):
        self.data = kit_archive({'mugham/src/tokens/tokens.css': b':root {}', 'mugham/src/styles/page.css': b'body {}',
                                 'mugham/src/kit.js': b'export {}'})
        self.digest = hashlib.sha256(self.data).hexdigest()

    def test_matching_digest_gives_only_the_style_files(self):
        files = browse.verify_kit(self.data, self.digest)
        self.assertEqual(files, {'tokens.css': b':root {}', 'page.css': b'body {}'})

    def test_different_digest_is_refused(self):
        with self.assertRaisesRegex(browse.KitError, 'not the pinned digest'):
            browse.verify_kit(self.data + b'\0', self.digest)

    def test_archive_without_a_style_file_is_refused(self):
        data = kit_archive({'mugham/src/tokens/tokens.css': b':root {}'})
        with self.assertRaisesRegex(browse.KitError, 'style files'):
            browse.verify_kit(data, hashlib.sha256(data).hexdigest())

    def test_changed_cache_file_is_refused(self):
        folder = tempfile.mkdtemp(prefix='aavanam-kit-')
        self.addCleanup(shutil.rmtree, folder, ignore_errors=True)
        cache = browse.Path(folder)
        (cache / f'mugham-{self.digest}.tar').write_bytes(self.data[:-1] + b'x')
        with self.assertRaisesRegex(browse.KitError, 'not the pinned digest'):
            browse.fetch_kit(cache, url='http://127.0.0.1:9/never', digest=self.digest)

    def test_pin_is_the_release_artifact(self):
        self.assertEqual(browse.KIT_URL, 'https://github.com/mohanraj00/mugham/releases/download/v0.8.0/mugham.tar')
        readme = (FOLDER / 'README.md').read_text(encoding='utf-8')
        self.assertIn(browse.KIT_URL, readme)
        self.assertIn(browse.KIT_SHA256, readme)


class AboutTest(unittest.TestCase):
    """The page refuses a server that is not aavanam-read/v1 or that lacks an operation (NET-VER-1, NET-CMP-2)."""

    def about(self, **change) -> dict:
        value = {'api': 'aavanam-read/v1', 'api_minor': 0, 'profile': 'aavanam/base/v1', 'profile_minor': 0,
                 'operations': sorted(browse.OPERATIONS), 'max_body_bytes': 1, 'deadline_ms': 1}
        value.update(change)
        return value

    def test_v1_with_the_operations_is_accepted(self):
        browse.check_about(self.about())
        browse.check_about(self.about(api_minor=7, operations=sorted([*browse.OPERATIONS, 'later_operation'])))

    def test_other_api_or_missing_operation_is_refused(self):
        for about in (self.about(api='aavanam-read/v2'), self.about(api_minor=None), self.about(api_minor=True),
                      self.about(operations=['read'])):
            with self.subTest(about=about), self.assertRaises(browse.ReadError):
                browse.check_about(about)

    def test_page_calls_only_operations_that_do_not_write(self):
        # NET-SCP-1 lists the operations that do not write content.
        binding = {'open_context', 'get', 'query_data', 'search', 'context_bundle', 'evidence', 'coverage',
                   'observations', 'render', 'compare', 'impact', 'list_items', 'read', 'search_text',
                   'snapshot_changes', 'item_history', 'publications', 'export'}
        self.assertLessEqual(set(browse.OPERATIONS), binding)
        with self.assertRaises(browse.ReadError):
            browse.ReadClient('http://127.0.0.1:9').call('publish', {})


@unittest.skipUnless(REQUIRED or os.environ.get('AAVANAM_CONFORM_BIN'),
                     'set AAVANAM_CONFORM_BIN or AAVANAM_BROWSE_EXAMPLE=1 to run the page on the fixture store')
class StoreTest(unittest.TestCase):
    """The page on the fixture store: a list, a snapshot view and a content view."""

    @classmethod
    def setUpClass(cls):
        cls.folder = tempfile.mkdtemp(prefix='aavanam-browse-')
        cls.driver = run.start_store(cls.folder)
        try:
            cls.refs = run.load(cls.driver, FIXTURE)
            cls.before = cls.fingerprint()
            cls.server = browse.make_server(cls.driver.read_url, run.collections(FIXTURE), '127.0.0.1', 0, {},
                                            verbose=False)
        except BaseException:
            cls.driver.close()
            raise
        cls.base = f'http://127.0.0.1:{cls.server.server_port}'
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.driver.close()
        shutil.rmtree(cls.folder, ignore_errors=True)

    @classmethod
    def fingerprint(cls) -> str:
        return cls.driver.request({'cmd': 'fingerprint', 'store': 'writer'})['fingerprint']

    def get(self, path: str, **query) -> str:
        url = self.base + path + ('?' + urllib.parse.urlencode(query) if query else '')
        with urllib.request.urlopen(url, timeout=30) as response:  # noqa: S310  the local page
            self.assertEqual(response.status, 200)
            self.assertIn("default-src 'none'", response.headers['Content-Security-Policy'])
            return response.read().decode('utf-8')

    def status(self, path: str, method: str = 'GET', host: str | None = None) -> int:
        call = urllib.request.Request(self.base + path, method=method, data=b'{}' if method == 'POST' else None)
        if host:
            call.add_header('Host', host)
        try:
            with urllib.request.urlopen(call, timeout=30) as response:  # noqa: S310  the local page
                return response.status
        except urllib.error.HTTPError as e:
            e.close()
            return e.code

    def test_list_of_collections_gives_each_head(self):
        page = self.get('/')
        # The head of a collection is the snapshot of its last publication (PUB-HED-3, PUB-LOG-3).
        for collection, head, count in ((GUIDE, 's-guide-2', 2), (NOTES, 's-notes-1', 1)):
            with self.subTest(collection=collection):
                self.assertIn(f'href="/collection?name={collection}"', page)
                self.assertIn(urllib.parse.quote(self.refs[head]), page)
                self.assertIn(f'<td>{count}</td>', page)
        self.assertIn('aavanam-read/v1', page)

    def test_each_table_is_in_a_named_scroll_box(self):
        # The kit page style keeps a wide table in the box data-mu-scroll (mugham 0.8.0). The box takes keyboard
        # focus and has a name, so a phone user can scroll the table and a screen reader names the region.
        pages = (self.get('/'), self.get('/collection', name=GUIDE))
        box = re.compile(r'<div data-mu-scroll tabindex="0" role="region" aria-label="([^"]+)"><table>.*?</table></div>')
        for page in pages:
            with self.subTest(page=page[:40]):
                boxes = box.findall(page)
                self.assertTrue(boxes)
                self.assertTrue(all(label.strip() for label in boxes))
                self.assertEqual(page.count('<table>'), len(boxes))

    def test_history_of_a_head(self):
        page = self.get('/collection', name=GUIDE)
        first, second = urllib.parse.quote(self.refs['s-guide-1']), urllib.parse.quote(self.refs['s-guide-2'])
        self.assertIn('no head', page)  # the first publication has no previous head (PUB-LOG-2)
        self.assertLess(page.index(first), page.index(second))
        self.assertEqual(page.count('<tr>'), 3)  # the header row and two publications

    def test_snapshot_view_lists_its_content(self):
        page = self.get('/snapshot', ref=self.refs['s-guide-2'])
        for logical_id in ('guide/pump', 'guide/valve', 'guide/seal', 'unit/pump', 'unit/valve', 'unit/seal'):
            self.assertIn(f'<td>{logical_id}</td>', page)
        self.assertNotIn('guide/hours', page)
        self.assertIn(urllib.parse.quote(self.refs['a-pump-2']), page)
        self.assertNotIn(urllib.parse.quote(self.refs['a-pump-1']), page)  # the earlier revision is not a member
        self.assertIn(f'name={GUIDE}', page)

    def test_content_view_gives_the_text_and_the_citation(self):
        page = self.get('/item', snapshot=self.refs['s-guide-2'], ref=self.refs['a-pump-2'])
        self.assertIn(PUMP_2, page)
        self.assertIn('text/markdown', page)
        citation = {'collection': GUIDE, 'kind': 'artifact', 'ref': self.refs['a-pump-2'], 'selector': None,
                    'snapshot': self.refs['s-guide-2']}
        self.assertIn(browse.esc(json.dumps(citation, indent=2, sort_keys=True)), page)
        # The history of the logical ID: the first head added a revision, the second replaced it (LIF-HST-4).
        self.assertIn('<td>added</td>', page)
        self.assertIn('<td>replaced</td>', page)

    def test_content_view_with_a_byte_selector(self):
        start = len('# Pump\n\n'.encode('utf-8'))
        end = start + len(PUMP_2.encode('utf-8'))
        page = self.get('/item', snapshot=self.refs['s-guide-2'], ref=self.refs['a-pump-2'], start=start, end=end)
        self.assertIn(f'<pre>{PUMP_2}</pre>', page)
        self.assertIn('&quot;selector_profile&quot;: &quot;aavanam/bytes/v1&quot;', page)

    def test_result_that_is_not_ok_is_shown_with_its_status(self):
        unknown = 'sha256:' + '0' * 64
        page = self.get('/snapshot', ref=unknown)
        self.assertIn('<strong>not_found</strong>', page)  # CTX-OPN-1: a root that is not in the store
        self.assertNotIn('<table>', page)
        # A member of a different snapshot is not in the read context (CTX-OPN-4).
        page = self.get('/item', snapshot=self.refs['s-notes-1'], ref=self.refs['a-pump-2'])
        self.assertIn('<strong>not_found</strong>', page)
        self.assertNotIn(PUMP_2, page)
        # A byte range after the end of the payload (CRD-SEL-3).
        page = self.get('/item', snapshot=self.refs['s-guide-2'], ref=self.refs['a-pump-2'], start=0, end=10000)
        self.assertIn('<strong>failed</strong>', page)

    def test_refusals_of_the_page(self):
        self.assertEqual(self.status('/', 'POST'), 405)
        self.assertEqual(self.status('/', host='other.test'), 421)
        self.assertEqual(self.status('/snapshot?ref=x'), 400)
        self.assertEqual(self.status('/item?snapshot=x'), 400)
        self.assertEqual(self.status('/snapshot?' + '&'.join(f'f{n}=1' for n in range(11))), 400)
        self.assertEqual(self.status('/snapshot?ref=sha256:' + '0' * 64 + '&ref=sha256:' + '1' * 64), 400)
        self.assertEqual(self.status('/collection?name=not-configured'), 400)
        self.assertEqual(self.status('/v1/op/publish'), 404)
        self.assertEqual(self.status('/kit/page.css'), 404)  # this server has no kit

    def test_zz_page_writes_nothing(self):
        # The test names sort, so this test runs after the views. The fingerprint has the content and the catalog.
        self.get('/')
        self.assertEqual(self.fingerprint(), self.before)


if __name__ == '__main__':
    unittest.main()
