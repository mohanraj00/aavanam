"""Run the browse page on a fixture store with one command.

Run it from the root of a checkout:

    python3 -B examples/browse_page/run.py

The command starts the backend driver `aavanam-conform` with the storage in memory, and writes the fixture of
fixture.json to it over the line protocol of the driver. Then it starts the browse page on the read API of the
driver (aavanam-read/v1, document 22). The page itself only reads. The driver stops when the command stops.

The command builds the driver with `go build`, unless AAVANAM_CONFORM_BIN gives its command.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
sys.path.insert(0, str(ROOT / 'conformance'))
sys.path.insert(0, str(HERE))

from backend_adapter import Driver, command  # noqa: E402  the driver client of the conformance adapter
import browse  # noqa: E402

FIXTURE = HERE / 'fixture.json'


class FixtureError(RuntimeError):
    """The fixture store did not load."""


def resolve(value, refs: dict[str, str], payloads: dict[str, str]):
    """Replace each handle of a fixture value: {"$ref": h} and {"$text": t, "media_type": m}."""
    if isinstance(value, dict):
        if set(value) == {'$ref'}:
            return refs[value['$ref']]
        if set(value) == {'$text', 'media_type'}:
            raw = value['$text'].encode('utf-8')
            digest = 'sha256:' + hashlib.sha256(raw).hexdigest()
            payloads[digest] = base64.b64encode(raw).decode('ascii')
            return {'digest': digest, 'media_type': value['media_type'], 'size': len(raw)}
        return {key: resolve(item, refs, payloads) for key, item in value.items()}
    if isinstance(value, list):
        return [resolve(item, refs, payloads) for item in value]
    return value


def call(driver: Driver, operation: str, request: dict, validators: dict | None = None) -> dict:
    reply = driver.request({'cmd': 'call', 'store': 'writer', 'op': operation, 'request': request}, validators)
    result = reply.get('result')
    if not isinstance(result, dict) or result.get('status') != 'ok':
        raise FixtureError(f'{operation} is not ok: {json.dumps(result)[:500]}')
    return result


def load(driver: Driver, fixture: dict) -> dict[str, str]:
    """Write the fixture to the writer store of the driver. Return the reference of each handle."""
    if fixture.get('format') != 'aavanam-browse-fixture/v1':
        raise FixtureError('the fixture format is not aavanam-browse-fixture/v1')
    # The sections and the policy are store configuration, before the first call (PUB-SEC-1, PUB-RCP-1).
    for collection, section in fixture.get('sections', {}).items():
        driver.request({'cmd': 'section', 'collection': collection, 'section': section})
    policy = fixture['policy']
    driver.request({'cmd': 'accept', 'policy': policy})
    refs: dict[str, str] = {}
    for entry in fixture['descriptors']:
        payloads: dict[str, str] = {}
        descriptor = resolve(entry['value'], refs, payloads)
        result = call(driver, 'put', {'descriptors': [json.dumps(descriptor)], 'payloads': payloads})
        refs[entry['as']] = result['items'][0]['ref']
    heads: dict[str, str | None] = {}
    for entry in fixture['publications']:
        request = resolve(entry['snapshot'], refs, {})
        snapshot = call(driver, 'create_snapshot', request)['items'][0]['ref']
        refs[entry['as']] = snapshot
        collection = request['collection']
        candidate = call(driver, 'create_candidate', {'snapshot': snapshot, 'policy': policy,
                                                      'expected_head': heads.get(collection)})['items'][0]['candidate']
        receipt = refs[entry['receipt']]
        # The application owns the acceptance decision (PUB-RCP-2). This one accepts only its own receipt.
        validators = {policy: lambda _candidate, given, _activation, expected=receipt: given == expected}
        published = call(driver, 'publish', {'candidate': candidate, 'receipt': receipt}, validators)
        heads[collection] = published['items'][0]['head']
    return refs


def collections(fixture: dict) -> list[str]:
    """The collection names of the fixture, in the order of their first publication."""
    return list(dict.fromkeys(entry['snapshot']['collection'] for entry in fixture['publications']))


def start_store(folder: str) -> Driver:
    """Start the driver with the storage in memory. Its first line gives the address of its read API."""
    driver = Driver(command('conform', folder) + ['-storage', 'mem'])
    if not driver.read_url:
        driver.close()
        raise FixtureError('the driver has no read API')
    return driver


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description='Run the browse page on a fixture store.')
    parser.add_argument('--port', type=int, default=8440, help='the port of the page on the loopback address')
    browse.kit_arguments(parser)
    args = parser.parse_args(argv)
    fixture = json.loads(FIXTURE.read_text(encoding='utf-8'))
    styles = browse.styles(args)
    with tempfile.TemporaryDirectory(prefix='aavanam-browse-') as folder:
        driver = start_store(folder)
        try:
            load(driver, fixture)
            server = browse.make_server(driver.read_url, collections(fixture), '127.0.0.1', args.port, styles)
            print(f'The fixture store has the read API at {driver.read_url}.')
            print(f'Open http://127.0.0.1:{server.server_port}/ to browse it. Stop with Ctrl-C.', flush=True)
            try:
                server.serve_forever()
            except KeyboardInterrupt:
                pass
            finally:
                server.server_close()
        finally:
            driver.close()
    return 0


if __name__ == '__main__':
    sys.exit(main())
