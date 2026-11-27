"""Tests of the adapter of the Go drivers, with a driver in Python. Run: python3 -B -m unittest discover -s conformance"""
from __future__ import annotations

import os
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path
from unittest import mock

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import backend_adapter  # noqa: E402
import runner  # noqa: E402

# A driver that follows the line protocol. `publish` asks the validator and gives its answer in `decision`.
# With the argument `http`, it also has a read listener, and a read over HTTP has `"via": "http"`.
FAKE = textwrap.dedent('''
    import json, sys

    def envelope(op, **more):
        return {'status': 'ok', 'reasons': [], 'operation': op, 'operation_profile': 'aavanam/base/v1', 'context': None, **more}

    def listen():
        import threading
        from http.server import BaseHTTPRequestHandler, HTTPServer

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                op = self.path.rsplit('/', 1)[1]
                request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                status = 404 if op == 'put' else 200
                body = json.dumps(envelope(op, via='http', echo=request)).encode()
                self.send_response(status)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *args):
                pass

        server = HTTPServer(('127.0.0.1', 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        return 'http://127.0.0.1:%d' % server.server_port

    hello = {'ready': True}
    if 'http' in sys.argv:
        hello['read_url'] = listen()
    if 'mute' in sys.argv:
        sys.exit(3)
    print(json.dumps(hello), flush=True)
    state = {'resets': 0, 'gates': [], 'sections': [], 'policies': [], 'profiles': [], 'args': sys.argv[1:]}
    for line in sys.stdin:
        m = json.loads(line)
        if m['cmd'] == 'reset':
            state.update(resets=state['resets'] + 1, gates=[], sections=[], policies=[], profiles=[])
            reply = {'ok': True}
        elif m['cmd'] == 'declare':
            state['profiles'] = m['profiles']
            reply = {'ok': True}
        elif m['cmd'] == 'call' and m['op'] == 'stop':
            sys.stderr.write('the driver stops here')
            sys.exit(4)
        elif m['cmd'] == 'call' and m['op'] == 'broken':
            reply = {'error': 'no such store'}
        elif m['cmd'] == 'call' and m['op'] == 'publish':
            print(json.dumps({'validate': {'policy': m['request']['policy'], 'candidate': 'k', 'receipt': 'r', 'activation': None}}), flush=True)
            reply = {'result': envelope('publish', decision=json.loads(sys.stdin.readline()))}
        elif m['cmd'] == 'call' and 'raw' in m:
            reply = {'result': envelope(m['op'], via='line', store=m['store'], raw=m['raw'], request='request' in m)}
        elif m['cmd'] == 'raw_bundle':
            reply = {'raw': 'bGluZQoA/w=='} if m['bundle'] else {'error': 'not a bundle'}
        elif m['cmd'] == 'call':
            reply = {'result': envelope(m['op'], via='line', store=m['store'], echo=m['request'], state=state)}
        elif m['cmd'] == 'fingerprint':
            reply = {'fingerprint': 'print-' + m['store']}
        elif m['cmd'] == 'accept':
            state['policies'].append(m['policy'])
            reply = {'ok': True}
        elif m['cmd'] == 'gate':
            state['gates'].append(m['collection'])
            reply = {'ok': True}
        elif m['cmd'] == 'section':
            state['sections'].append([m['collection'], m['section']])
            reply = {'ok': True}
        print(json.dumps(reply), flush=True)
''')


class BackendAdapterTest(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.script = Path(self.folder.name) / 'fake_driver.py'
        self.script.write_text(FAKE)

    def adapter(self, *args, **env):
        fake = f'{sys.executable} -B {self.script} ' + ' '.join(args)
        values = {'AAVANAM_CONFORM_BIN': fake, 'AAVANAM_DEVICE_BIN': fake, **env}
        with mock.patch.dict(os.environ, values):
            for name in ('AAVANAM_WRITER', 'AAVANAM_DEVICE', 'AAVANAM_READ_HTTP'):
                if name not in env:
                    os.environ.pop(name, None)
            adapter = backend_adapter.BackendAdapter()
        self.addCleanup(adapter.close)
        return adapter

    def test_calls_go_to_the_driver_of_the_store(self):
        adapter = self.adapter()
        adapter.reset()
        request = {'refs': ['sha256:' + '0' * 64], 'text': 'a\nb \ud800'}
        for store in ('writer', 'device', 'output'):
            result = adapter.call('get', request, store=store)
            self.assertEqual((result['store'], result['via'], result['echo']), (store, 'line', request))
            self.assertEqual(adapter.fingerprint(store), f'print-{store}')
        self.assertEqual(adapter.call('get', {})['state']['resets'], 1)
        self.assertEqual(adapter.call('get', {}, store='device')['state']['resets'], 1)
        self.assertEqual(adapter.call('get', {}, store='device')['state']['args'][:1], ['--dir'])  # DEV-LAY-1
        self.assertEqual(adapter.call('get', {})['state']['args'], [])

    def test_gates_sections_and_policies(self):
        adapter = self.adapter()
        adapter.gate('c')
        adapter.section('c', 'serving')
        adapter.accept('p', lambda *_: True)
        writer, device = adapter.call('get', {})['state'], adapter.call('get', {}, store='output')['state']
        self.assertEqual((writer['gates'], writer['sections'], writer['policies']), (['c'], [['c', 'serving']], ['p']))
        self.assertEqual((device['gates'], device['sections'], device['policies']), (['c'], [], ['p']))  # PUB-SEC-1
        adapter.reset()
        self.assertEqual(adapter.call('get', {})['state']['policies'], [])
        self.assertEqual(adapter.validators, {})

    def test_validator_answers(self):
        adapter = self.adapter()
        seen = []

        def late(*args):
            raise TimeoutError

        adapter.accept('yes', lambda *args: seen.append(args) or True)
        adapter.accept('no', lambda *_: False)
        adapter.accept('truthy', lambda *_: 1)  # only True accepts (PUB-RCP-2)
        adapter.accept('late', late)
        self.assertEqual(adapter.call('publish', {'policy': 'yes'})['decision'], {'accepted': True})
        self.assertEqual(seen, [('k', 'r', None)])
        self.assertEqual(adapter.call('publish', {'policy': 'no'})['decision'], {'accepted': False})
        self.assertEqual(adapter.call('publish', {'policy': 'truthy'})['decision'], {'accepted': False})
        self.assertEqual(adapter.call('publish', {'policy': 'late'})['decision'], {'no_answer': True})  # PUB-RCP-4
        self.assertEqual(adapter.call('publish', {'policy': 'unknown'})['decision'], {'accepted': False})

    def test_reads_of_the_writer_go_over_http(self):
        adapter = self.adapter('http')
        self.assertEqual(adapter.call('get', {'refs': []})['via'], 'http')
        self.assertEqual(adapter.call('no_such_operation', {})['via'], 'http')  # NET-SCP-2
        self.assertEqual(adapter.call('a/b c', {})['operation'], 'a%2Fb%20c')  # one path segment
        self.assertEqual(adapter.call('get', {}, store='device')['via'], 'line')
        for operation in sorted(backend_adapter.WRITES - {'publish'}):
            self.assertEqual(adapter.call(operation, {})['via'], 'line', operation)  # NET-SCP-3
        self.assertEqual(self.adapter('http', AAVANAM_READ_HTTP='0').call('get', {})['via'], 'line')

    def test_declare_goes_to_each_driver(self):
        index = 'aavanam/semantic-index/v1'
        adapter = self.adapter('http')
        self.assertEqual(adapter.call('semantic_search', {})['via'], 'http')  # no profile: the envelope of NET-SCP-2
        adapter.declare([index])
        self.assertEqual(adapter.call('put', {})['state']['profiles'], [index])
        self.assertEqual(adapter.call('get', {}, store='device')['state']['profiles'], [index])
        self.assertEqual(adapter.call('semantic_search', {})['via'], 'line')  # NET-SCP-3: not in the read binding
        self.assertEqual(adapter.call('get', {})['via'], 'http')
        adapter.reset()  # after a reset, a store declares no profile
        self.assertEqual(adapter.call('put', {})['state']['profiles'], [])
        self.assertEqual(adapter.call('semantic_search', {})['via'], 'http')
        mixed = self.adapter(AAVANAM_WRITER='reference')
        mixed.declare(['aavanam/layer/v1'])
        self.assertEqual(mixed.local['writer'].profiles, ('aavanam/layer/v1',))
        self.assertEqual(mixed.call('get', {}, store='device')['state']['profiles'], ['aavanam/layer/v1'])
        mixed.reset()
        self.assertEqual(mixed.local['writer'].profiles, ())

    def test_profile_operations_are_the_profile_rows(self):
        cs = runner.check_spec
        base = cs.operations()
        for profile in ('aavanam/layer/v1', 'aavanam/semantic-index/v1'):
            added = {op for op, statuses in cs.operations(profile).items()
                     if statuses != {'unsupported'} and base.get(op, {'unsupported'}) == {'unsupported'}}
            self.assertEqual(backend_adapter.PROFILE_OPERATIONS.get(profile, frozenset()), added, profile)  # IDX-PRF-4

    def test_writes_are_the_operations_outside_the_read_binding(self):
        cs = runner.check_spec
        rows = cs.table(cs.STATUSES.read_text(), 'The operation table:', 3, cs.STATUSES.name, [])
        table = {row[0]: {s.strip() for s in row[2].split(',')} for row in rows}
        reads = {'open_context', 'get', 'query_data', 'search', 'context_bundle', 'evidence', 'coverage', 'observations', 'render',
                 'compare', 'impact', 'list_items', 'read', 'search_text', 'snapshot_changes', 'item_history', 'publications',
                 'export'}  # NET-SCP-1
        reserved = {op for op, statuses in table.items() if statuses == {'unsupported'}}
        self.assertEqual(backend_adapter.WRITES, set(table) - reads - reserved)

    def test_a_raw_bundle_goes_to_the_driver_as_base64_text(self):  # BUN-RAW-3, BUN-RAW-4
        adapter = self.adapter('http')
        adapter.reset()
        raw = b'line\n\x00\xff'
        for store in ('writer', 'device'):
            result = adapter.call('import', {'bundle': raw}, store=store)
            self.assertEqual((result['store'], result['via'], result['raw'], result['request']), (store, 'line', 'bGluZQoA/w==', False))
            self.assertEqual(adapter.raw_bundle({'format': 'x'}, store=store), raw)
        self.assertEqual(adapter.call('import', {'bundle': {}}, store='device')['echo'], {'bundle': {}})  # a bundle stays a request
        with self.assertRaisesRegex(backend_adapter.DriverError, 'not a bundle'):
            adapter.raw_bundle({})

    def test_driver_failures_raise(self):
        adapter = self.adapter()
        with self.assertRaisesRegex(backend_adapter.DriverError, 'no such store'):
            adapter.call('broken', {})
        self.assertEqual(adapter.call('get', {})['via'], 'line')  # the driver continues after an error line
        with self.assertRaisesRegex(backend_adapter.DriverError, 'the driver stops here'):
            adapter.call('stop', {})
        with self.assertRaisesRegex(backend_adapter.DriverError, 'the driver stopped'):
            self.adapter('mute')
        with self.assertRaisesRegex(backend_adapter.DriverError, 'HTTP 404'):
            self.adapter('http').drivers['writer'].post('put', {})  # NET-TRN-3: only 200 has an envelope

    def test_environment_values_are_checked(self):
        with self.assertRaisesRegex(backend_adapter.DriverError, 'AAVANAM_WRITER'):
            self.adapter(AAVANAM_WRITER='postgres')
        with self.assertRaisesRegex(backend_adapter.DriverError, 'AAVANAM_DEVICE'):
            self.adapter(AAVANAM_DEVICE='sqlite')

    def test_reference_roles_use_the_python_store(self):
        adapter = self.adapter(AAVANAM_WRITER='reference', AAVANAM_DEVICE='reference')
        self.assertEqual(adapter.drivers, {})
        vectors = runner.load()
        for vid in ('devices/device-update-ok', 'publication/publish-validator-deadline', 'publication/sec-serving-ok'):
            self.assertEqual(runner.run_vector(adapter, vid, vectors[vid]), [], vid)
        mixed = self.adapter(AAVANAM_WRITER='reference')
        self.assertEqual(set(mixed.local), {'writer'})
        self.assertEqual(mixed.call('get', {}, store='device')['via'], 'line')
        self.assertEqual(mixed.call('no_such_operation', {})['status'], 'unsupported')
        # A writer vector passes on the Python writer with a Go device driver. The writer declares no profile (RSV-LST-4).
        for vid in ('publication/publish-ok', 'reserved/semantic-search'):
            self.assertEqual(runner.run_vector(mixed, vid, vectors[vid]), [], vid)
        self.assertEqual({store.profiles for store in (*adapter.local.values(), *mixed.local.values())}, {()})


if __name__ == '__main__':
    unittest.main()
