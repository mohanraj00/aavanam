"""A read-only page that browses a store through the network read API aavanam-read/v1 (document 22).

Run it on a store that has the read API, for example aavanam-server:

    python3 -B examples/browse_page/browse.py --read-url http://127.0.0.1:8437 --collection NAME

The page lists the given collections with their heads, the history of each head, the content of a snapshot,
and one item with its content and its citation. It calls only operations that do not write content. The page
uses only the Python standard library. The mugham UI kit gives its style, if the kit is available.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import html
import io
import json
import re
import sys
import tarfile
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

API = 'aavanam-read/v1'
# The operations that the page calls. Each one is in NET-SCP-1, so none of them writes content.
# `open_context` adds a read context, which is not content (CTX-OPN-2).
OPERATIONS = ('item_history', 'list_items', 'open_context', 'publications', 'read')
REF = re.compile(r'sha256:[0-9a-f]{64}')
PAGE_SIZE = 100  # the items of one snapshot page (DSC-LST-3)
SHOWN_CHARS = 65536  # the characters of text that one item page shows
HTTP_TIMEOUT = 30  # seconds, for one call of the read API

# The pinned mugham UI kit. The page uses only its two style files.
KIT_URL = 'https://github.com/mohanraj00/mugham/releases/download/v0.4.0/mugham.tar'
KIT_SHA256 = 'af966b8402e89ba711f8831b6d40ce67b54b2af60fe00e847ff25996adea7934'
KIT_FILES = {'tokens.css': 'mugham/src/tokens/tokens.css', 'page.css': 'mugham/src/styles/page.css'}
KIT_MAX_BYTES = 16 << 20


class ReadError(RuntimeError):
    """The read API did not give a result envelope."""


class KitError(RuntimeError):
    """The kit archive is not the pinned archive."""


# The read API client.

class ReadClient:
    """Call the operations of OPERATIONS on one read API (NET-TRN-1)."""

    def __init__(self, url: str):
        parts = urllib.parse.urlsplit(url)
        if parts.scheme not in ('http', 'https') or not parts.netloc or parts.path not in ('', '/'):
            raise ReadError(f'the read API address is not an HTTP origin: {url!r}')
        self.url = f'{parts.scheme}://{parts.netloc}'

    def about(self) -> dict:
        """The description of the API (NET-VER-1)."""
        return self._send(urllib.request.Request(f'{self.url}/v1/about', method='GET'), 'about')

    def call(self, operation: str, request: dict) -> dict:
        """Call one operation and return its envelope. Only the field `status` gives the outcome (NET-TRN-3)."""
        if operation not in OPERATIONS:
            raise ReadError(f'the page does not call {operation}')
        body = json.dumps(request).encode('utf-8')
        call = urllib.request.Request(f'{self.url}/v1/op/{operation}', data=body, method='POST',
                                      headers={'Content-Type': 'application/json'})
        envelope = self._send(call, operation)
        if not isinstance(envelope.get('status'), str):
            raise ReadError(f'the result of {operation} has no status')
        return envelope

    def _send(self, call: urllib.request.Request, name: str) -> dict:
        try:
            with urllib.request.urlopen(call, timeout=HTTP_TIMEOUT) as response:  # noqa: S310  the configured API
                status, text = response.status, response.read()
        except urllib.error.HTTPError as e:
            e.close()
            raise ReadError(f'the read API gave HTTP {e.code} for {name}') from None
        except (urllib.error.URLError, OSError) as e:
            raise ReadError(f'the read API did not answer for {name}: {e}') from None
        if status != 200:
            raise ReadError(f'the read API gave HTTP {status} for {name}')
        try:
            value = json.loads(text)
        except ValueError:
            raise ReadError(f'the read API gave a body that is not JSON for {name}') from None
        if not isinstance(value, dict):
            raise ReadError(f'the read API gave a body that is not a JSON object for {name}')
        return value


def check_about(about: dict) -> None:
    """Refuse a server that is not aavanam-read/v1 or that does not have an operation of the page (NET-CMP-2)."""
    if about.get('api') != API:
        raise ReadError(f'the server gives the API {about.get("api")!r}, not {API}')
    minor = about.get('api_minor')
    if not isinstance(minor, int) or isinstance(minor, bool) or minor < 0:
        raise ReadError('the description has no valid api_minor')
    offered = about.get('operations')
    if not isinstance(offered, list):
        raise ReadError('the description has no list of operations')
    missing = [name for name in OPERATIONS if name not in offered]
    if missing:
        raise ReadError(f'the server does not have the operations {", ".join(missing)}')


# The kit.

def verify_kit(data: bytes, digest: str = KIT_SHA256) -> dict[str, bytes]:
    """Check the SHA-256 digest of the kit archive. Return the bytes of the style files of KIT_FILES."""
    actual = hashlib.sha256(data).hexdigest()
    if actual != digest:
        raise KitError(f'the kit archive has the SHA-256 digest {actual}, not the pinned digest {digest}')
    files = {}
    try:
        with tarfile.open(fileobj=io.BytesIO(data), mode='r:') as archive:
            for name, member_name in KIT_FILES.items():
                member = archive.getmember(member_name)
                if not member.isfile():
                    raise KitError(f'{member_name} in the kit archive is not a file')
                files[name] = archive.extractfile(member).read()
    except (KeyError, tarfile.TarError) as e:
        raise KitError(f'the kit archive does not have the style files: {e}') from None
    return files


def fetch_kit(cache: Path, url: str = KIT_URL, digest: str = KIT_SHA256) -> dict[str, bytes]:
    """Get the pinned kit archive from the cache, or download it. Check its digest each time."""
    path = cache / f'mugham-{digest}.tar'
    if path.is_file():
        return verify_kit(path.read_bytes(), digest)
    try:
        with urllib.request.urlopen(url, timeout=HTTP_TIMEOUT) as response:  # noqa: S310  the pinned URL
            data = response.read(KIT_MAX_BYTES + 1)
    except (urllib.error.URLError, OSError) as e:
        raise KitError(f'the kit download failed: {e}') from None
    if len(data) > KIT_MAX_BYTES:
        raise KitError('the kit archive is larger than its limit')
    files = verify_kit(data, digest)  # check before the cache keeps the bytes
    cache.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=cache, delete=False) as part:
        part.write(data)
    Path(part.name).replace(path)
    return files


def kit_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument('--no-kit', action='store_true', help='show the page with no style')
    parser.add_argument('--kit-cache', type=Path, default=Path.home() / '.cache' / 'aavanam-browse',
                        help='the folder of the downloaded kit archive')


def styles(args: argparse.Namespace) -> dict[str, bytes]:
    if args.no_kit:
        return {}
    try:
        return fetch_kit(args.kit_cache)
    except KitError as e:
        raise SystemExit(f'{e}. Use --no-kit to show the page with no style.') from None


# The views. Each value from the store goes through esc() or attr(), so the page shows it only as text.

def esc(value) -> str:
    return html.escape(str(value), quote=True)


def link(path: str, text, **query) -> str:
    return f'<a href="{esc(path + "?" + urllib.parse.urlencode(query))}">{esc(text)}</a>'


def short(ref) -> str:
    return f'{ref[:19]}…' if isinstance(ref, str) and REF.fullmatch(ref) else str(ref)


def snapshot_link(ref) -> str:
    if not isinstance(ref, str) or not REF.fullmatch(ref):
        return '<em>no head</em>' if ref is None else esc(ref)
    return f'<code>{link("/snapshot", short(ref), ref=ref)}</code>'


def outcome(envelope: dict) -> str:
    """The status and the reasons of a result that is not ok. Such a result is never an empty result."""
    reasons = ''.join(f'<li><code>{esc(r.get("code"))}</code> {esc(", ".join(map(str, r.get("refs") or [])))}</li>'
                      for r in envelope.get('reasons') or [] if isinstance(r, dict))
    return (f'<p class="status">The read API gave the status <strong>{esc(envelope.get("status"))}</strong>.</p>'
            + (f'<ul>{reasons}</ul>' if reasons else ''))


def table(headers: list[str], rows: list[list[str]]) -> str:
    head = ''.join(f'<th scope="col">{esc(h)}</th>' for h in headers)
    body = ''.join('<tr>' + ''.join(f'<td>{cell}</td>' for cell in row) + '</tr>' for row in rows)
    return f'<table><thead><tr>{head}</tr></thead><tbody>{body}</tbody></table>'


def pretty(value) -> str:
    return f'<pre>{esc(json.dumps(value, indent=2, sort_keys=True, ensure_ascii=False))}</pre>'


class Views:
    """Build each view from read API calls."""

    def __init__(self, client: ReadClient, collections: list[str], about: dict):
        self.client, self.collections, self.about = client, collections, about

    def index(self) -> tuple[str, str]:
        rows = []
        for name in self.collections:
            result = self.client.call('publications', {'collection': name})
            if result['status'] != 'ok':
                rows.append([link('/collection', name, name=name), outcome(result), ''])
                continue
            items = result.get('items') or []
            # Only publish changes a head, and it records each change (PUB-LOG-2). The last record has the head.
            head = items[-1].get('head') if items else None
            rows.append([link('/collection', name, name=name), snapshot_link(head), esc(len(items))])
        body = ('<p>The read API has no operation that lists collections. The page shows the collections of its '
                'configuration.</p>' + table(['Collection', 'Head', 'Publications'], rows))
        return 'Collections', body

    def collection(self, name: str) -> tuple[str, str]:
        result = self.client.call('publications', {'collection': name})
        if result['status'] != 'ok':
            return f'Collection {name}', outcome(result)
        items = result.get('items') or []
        rows = [[esc(n), snapshot_link(item.get('previous_head')), snapshot_link(item.get('head')),
                 f'<code>{esc(short(item.get("candidate")))}</code>', f'<code>{esc(short(item.get("receipt")))}</code>']
                for n, item in enumerate(items, 1)]
        if not rows:
            return f'Collection {name}', '<p>The collection has no recorded publication, so it has no head.</p>'
        body = ('<h2>History of the head</h2><p>Each row is one publication, in the order of the head updates '
                '(PUB-LOG-3). The last row has the current head.</p>'
                + table(['#', 'Previous head', 'Head', 'Candidate', 'Receipt'], rows))
        return f'Collection {name}', body

    def _open(self, snapshot: str) -> tuple[str | None, dict | None, str]:
        """Open a read context on one snapshot, and read its descriptor. Give the context, the descriptor or an error."""
        opened = self.client.call('open_context', {'roots': [snapshot]})
        if opened['status'] != 'ok' or not isinstance(opened.get('context'), str):
            return None, None, outcome(opened)
        context = opened['context']
        result = self.client.call('read', {'context': context, 'ref': snapshot})
        if result['status'] != 'ok':
            return None, None, outcome(result)
        item = result['items'][0]
        if item.get('kind') != 'snapshot' or not isinstance(item.get('descriptor'), dict):
            return None, None, '<p class="status">The reference is not a snapshot.</p>'
        return context, item['descriptor'], ''

    def snapshot(self, ref: str, offset: int) -> tuple[str, str]:
        context, descriptor, error = self._open(ref)
        if error:
            return 'Snapshot', error
        collection = descriptor.get('collection')
        result = self.client.call('list_items', {'context': context, 'offset': offset, 'limit': PAGE_SIZE})
        header = (f'<dl><dt>Snapshot</dt><dd><code>{esc(ref)}</code></dd>'
                  f'<dt>Collection</dt><dd>{link("/collection", collection, name=collection)}</dd></dl>')
        items = result.get('items') or []
        if result['status'] not in ('ok', 'partial'):
            return 'Snapshot', header + outcome(result)
        rows = [[esc(item.get('kind')), esc(item.get('logical_id') or ''),
                 f'<code>{link("/item", short(item.get("ref")), snapshot=ref, ref=item.get("ref"))}</code>']
                for item in items if item.get('status') == 'ok']
        bad = [item for item in items if item.get('status') != 'ok']
        body = header + '<h2>Content</h2>'
        body += f'<p>Items {offset + 1} to {offset + len(rows)} of the read context on this snapshot (DSC-LST-3).</p>'
        body += table(['Kind', 'Logical ID', 'Reference'], rows)
        if bad:
            body += f'<p class="status">{len(bad)} items are not ok. The page does not show them as content.</p>'
        if result['status'] == 'partial':
            body += outcome(result)
            if len(items) == PAGE_SIZE:
                body += f'<p>{link("/snapshot", "Next items", ref=ref, offset=offset + PAGE_SIZE)}</p>'
        body += f'<h2>Descriptor</h2>{pretty(descriptor)}'
        return 'Snapshot', body

    def item(self, snapshot: str, ref: str, selector: dict | None) -> tuple[str, str]:
        context, descriptor, error = self._open(snapshot)
        if error:
            return 'Item', error
        collection = descriptor.get('collection')
        request = {'context': context, 'ref': ref}
        if selector is not None:
            request['selector'] = selector
        result = self.client.call('read', request)
        back = f'<p>{link("/snapshot", "Back to the snapshot", ref=snapshot)}</p>'
        if result['status'] != 'ok':
            return 'Item', back + outcome(result)
        item = result['items'][0]
        member = item.get('descriptor') or {}
        body = back + (f'<dl><dt>Kind</dt><dd>{esc(item.get("kind"))}</dd>'
                       f'<dt>Logical ID</dt><dd>{esc(member.get("logical_id") or "none")}</dd>'
                       f'<dt>Reference</dt><dd><code>{esc(ref)}</code></dd></dl>')
        body += '<h2>Content</h2>' + content(item, member)
        if member.get('payload'):
            start = selector['start'] if selector else 0
            end = selector['end'] if selector else member['payload'].get('size', 0)
            body += ('<form method="get" action="/item">'
                     f'<input type="hidden" name="snapshot" value="{esc(snapshot)}">'
                     f'<input type="hidden" name="ref" value="{esc(ref)}">'
                     f'<label>Start byte <input name="start" type="number" min="0" value="{esc(start)}"></label> '
                     f'<label>End byte <input name="end" type="number" min="1" value="{esc(end)}"></label> '
                     '<button type="submit">Cite these bytes</button></form>')
        # The citation names the collection, the snapshot, the item and the selector of the read.
        citation = {'collection': collection, 'snapshot': snapshot, 'ref': ref, 'kind': item.get('kind'),
                    'selector': item.get('selector')}
        body += f'<h2>Citation</h2>{pretty(citation)}'
        if member.get('logical_id'):
            body += self.history(collection, member['logical_id'], snapshot)
        body += f'<h2>Descriptor</h2>{pretty(member)}'
        return 'Item', body

    def history(self, collection: str, logical_id: str, snapshot: str) -> str:
        result = self.client.call('item_history', {'collection': collection, 'logical_id': logical_id})
        body = f'<h2>History of {esc(logical_id)}</h2>'
        if result['status'] != 'ok':
            return body + outcome(result)
        rows = []
        for n, entry in enumerate(result.get('items') or [], 1):
            head = entry.get('head')
            revisions = ', '.join(
                f'<code>{link("/item", short(rev.get("ref")), snapshot=head, ref=rev.get("ref"))}</code>'
                for rev in entry.get('revisions') or [] if isinstance(rev, dict))
            mark = ' (this snapshot)' if head == snapshot else ''
            rows.append([esc(n), snapshot_link(head) + esc(mark), esc(entry.get('change')), revisions or 'none'])
        if not rows:
            return body + '<p>The collection has no recorded publication.</p>'
        return body + ('<p>The revisions that each head contains (LIF-HST-3). The history does not state which '
                       'revision is authoritative (LIF-HST-5).</p>' + table(['#', 'Head', 'Change', 'Revisions'], rows))


def content(item: dict, member: dict) -> str:
    """The selected bytes as text, if the payload is text in UTF-8. The page does not interpret the bytes."""
    if item.get('data') is None:
        return '<p>The item has no payload bytes.</p>'
    try:
        data = base64.b64decode(item['data'], validate=True)
    except ValueError:
        return '<p class="status">The read API gave data that is not base64 text.</p>'
    media = str((member.get('payload') or {}).get('media_type', ''))
    note = f'<p><small>{len(data)} bytes, media type <code>{esc(media)}</code>.</small></p>'
    if not (media.startswith('text/') or media == 'application/json'):
        return note + '<p>The page shows only text content.</p>'
    try:
        text = data.decode('utf-8')
    except UnicodeDecodeError:
        return note + '<p>The bytes are not UTF-8 text.</p>'
    more = f'<p><small>The page shows the first {SHOWN_CHARS} characters only.</small></p>' if len(text) > SHOWN_CHARS else ''
    return note + f'<pre>{esc(text[:SHOWN_CHARS])}</pre>' + more


# The HTTP server of the page.

SECURITY_HEADERS = {
    'Content-Security-Policy': "default-src 'none'; style-src 'self'; form-action 'self'; base-uri 'none'; "
                               "frame-ancestors 'none'",
    'X-Content-Type-Options': 'nosniff',
    'Referrer-Policy': 'no-referrer',
    'Cache-Control': 'no-store',
}


class BadRequest(ValueError):
    pass


def one(query: dict[str, list[str]], name: str, required: bool = True) -> str | None:
    values = query.get(name, [])
    if len(values) > 1 or (required and not values):
        raise BadRequest(f'the query needs one value of {name}')
    return values[0] if values else None


def ref_value(query: dict[str, list[str]], name: str) -> str:
    value = one(query, name)
    if not REF.fullmatch(value):
        raise BadRequest(f'{name} is not a reference')
    return value


def integer(query: dict[str, list[str]], name: str) -> int | None:
    value = one(query, name, required=False)
    if value is None:
        return None
    if not re.fullmatch(r'[0-9]{1,12}', value):
        raise BadRequest(f'{name} is not an integer')
    return int(value)


class Handler(BaseHTTPRequestHandler):
    server_version = 'aavanam-browse'
    sys_version = ''

    def do_GET(self) -> None:  # noqa: N802  the name of the standard library
        if not self.server.host_ok(self.headers.get('Host')):
            return self.reply(HTTPStatus.MISDIRECTED_REQUEST, 'text/plain', b'The Host header is not this page.')
        parts = urllib.parse.urlsplit(self.path)
        if parts.path.startswith('/kit/'):
            data = self.server.styles.get(parts.path[len('/kit/'):])
            if data is None:
                return self.reply(HTTPStatus.NOT_FOUND, 'text/plain', b'Not found.')
            return self.reply(HTTPStatus.OK, 'text/css; charset=utf-8', data)
        query = urllib.parse.parse_qs(parts.query, keep_blank_values=True, max_num_fields=10)
        views = self.server.views
        try:
            if parts.path == '/':
                title, body = views.index()
            elif parts.path == '/collection':
                name = one(query, 'name')
                if name not in views.collections:
                    raise BadRequest('the collection is not in the configuration of the page')
                title, body = views.collection(name)
            elif parts.path == '/snapshot':
                title, body = views.snapshot(ref_value(query, 'ref'), integer(query, 'offset') or 0)
            elif parts.path == '/item':
                start, end = integer(query, 'start'), integer(query, 'end')
                if (start is None) != (end is None) or (start is not None and start >= end):
                    raise BadRequest('give start and end, with start less than end')
                selector = None if start is None else {'selector_profile': 'aavanam/bytes/v1', 'start': start, 'end': end}
                title, body = views.item(ref_value(query, 'snapshot'), ref_value(query, 'ref'), selector)
            else:
                return self.reply(HTTPStatus.NOT_FOUND, 'text/plain', b'Not found.')
        except BadRequest as e:
            return self.reply(HTTPStatus.BAD_REQUEST, 'text/plain', str(e).encode())
        except ReadError as e:
            return self.page(HTTPStatus.BAD_GATEWAY, 'Read API error', f'<p class="status">{esc(e)}</p>')
        self.page(HTTPStatus.OK, title, body)

    def do_HEAD(self) -> None:  # noqa: N802
        self.reply(HTTPStatus.METHOD_NOT_ALLOWED, 'text/plain', b'')

    def method_not_allowed(self) -> None:
        self.reply(HTTPStatus.METHOD_NOT_ALLOWED, 'text/plain', b'The page only reads. Use GET.')

    do_POST = do_PUT = do_PATCH = do_DELETE = do_OPTIONS = method_not_allowed

    def page(self, status: HTTPStatus, title: str, body: str) -> None:
        about = self.server.views.about
        links = ''.join(f'<link rel="stylesheet" href="/kit/{name}">' for name in ('tokens.css', 'page.css')
                        if name in self.server.styles)
        text = ('<!doctype html><html lang="en"><head><meta charset="utf-8">'
                '<meta name="viewport" content="width=device-width, initial-scale=1">'
                f'<title>{esc(title)} - aavanam browse</title>{links}</head><body>'
                '<header><h1>aavanam browse</h1><p>A read-only view of a store.</p>'
                '<nav><a href="/">Collections</a></nav></header>'
                f'<main><h1>{esc(title)}</h1>{body}</main>'
                f'<footer><p>Read API {esc(about.get("api"))}, minor version {esc(about.get("api_minor"))}, '
                f'profile minor version {esc(about.get("profile_minor"))}. The page writes no content.</p></footer>'
                '</body></html>')
        self.reply(status, 'text/html; charset=utf-8', text.encode('utf-8'))

    def reply(self, status: HTTPStatus, media: str, data: bytes) -> None:
        self.send_response(status)
        self.send_header('Content-Type', media)
        self.send_header('Content-Length', str(len(data)))
        for name, value in SECURITY_HEADERS.items():
            self.send_header(name, value)
        if status == HTTPStatus.METHOD_NOT_ALLOWED:
            self.send_header('Allow', 'GET')
        self.end_headers()
        if self.command != 'HEAD':
            self.wfile.write(data)

    def log_message(self, format: str, *args) -> None:  # noqa: A002  the name of the standard library
        if self.server.verbose:
            super().log_message(format, *args)


class PageServer(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, address, views: Views, styles: dict[str, bytes], verbose: bool):
        super().__init__(address, Handler)
        self.views, self.styles, self.verbose = views, styles, verbose
        port = self.server_port
        # Only the names of the loopback address. Thus a different site cannot read the page through a DNS name
        # that points to the loopback address.
        self.hosts = {f'127.0.0.1:{port}', f'localhost:{port}', f'[::1]:{port}'}

    def host_ok(self, host: str | None) -> bool:
        return host in self.hosts


def make_server(read_url: str, collections: list[str], host: str, port: int, styles: dict[str, bytes],
                verbose: bool = True) -> PageServer:
    """Check the read API (NET-VER-1), then make the page server. The caller calls serve_forever."""
    client = ReadClient(read_url)
    about = client.about()
    check_about(about)
    if not collections:
        raise ReadError('give one or more collections')
    return PageServer((host, port), Views(client, collections, about), styles, verbose)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description='Browse a store through its read API (aavanam-read/v1).')
    parser.add_argument('--read-url', required=True, help='the origin of the read API, for example http://127.0.0.1:8437')
    parser.add_argument('--collection', action='append', required=True, help='a collection to list; give it one or more times')
    parser.add_argument('--port', type=int, default=8440, help='the port of the page on the loopback address')
    kit_arguments(parser)
    args = parser.parse_args(argv)
    try:
        server = make_server(args.read_url, args.collection, '127.0.0.1', args.port, styles(args))
    except ReadError as e:
        print(f'browse: {e}', file=sys.stderr)
        return 2
    print(f'Open http://127.0.0.1:{server.server_port}/ to browse the store. Stop with Ctrl-C.', flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0


if __name__ == '__main__':
    sys.exit(main())
