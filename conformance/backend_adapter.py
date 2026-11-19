"""The conformance adapter of the Go drivers. Run: python3 -B conformance/runner.py --adapter backend_adapter:BackendAdapter

The writer store is the driver `aavanam-conform` (backend/, PostgreSQL). The device store and its local output
store are the driver `aavanam-device` (reference/, SQLite). A driver reads one JSON object for each line on its
input and writes one JSON object for each line on its output (docs/design/backend.md, section 6).

The environment selects the stores:

- AAVANAM_WRITER: `backend` (the default) or `reference`, the Python store in src/.
- AAVANAM_DEVICE: `go` (the default) or `reference`.
- AAVANAM_READ_HTTP: `0` sends each call of the writer store over the line protocol. If not, a call that does
  not write content goes to the network read binding of the driver (NET-TRN-1), if the driver has one.
- AAVANAM_CONFORM_BIN and AAVANAM_DEVICE_BIN: the command of each driver. If one is not set, the adapter
  builds the driver with `go build` into a temporary directory.

The driver `aavanam-conform` reads AAVANAM_STORAGE and AAVANAM_PG_DSN.
"""
from __future__ import annotations

import atexit
import json
import os
import shlex
import shutil
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# The operations that write content. The network read binding does not have them (NET-SCP-3). Each other
# operation name goes to the read binding, also a name with no row in the operation table (NET-SCP-2).
WRITES = frozenset({'put', 'create_snapshot', 'create_candidate', 'publish', 'import', 'execution_inputs', 'build_inputs',
                    'producer_put', 'producer_get', 'producer_forget', 'producer_removals', 'producer_append', 'producer_read'})
DRIVERS = {'conform': ('backend', './cmd/aavanam-conform', 'AAVANAM_CONFORM_BIN'),
           'device': ('reference', './cmd/aavanam-device', 'AAVANAM_DEVICE_BIN')}
HTTP_TIMEOUT = 120  # seconds, for one call of the read binding


class DriverError(RuntimeError):
    """A driver stopped, gave an error line or gave a response that is not a result envelope."""


def choice(name: str, values: tuple[str, ...]) -> str:
    value = os.environ.get(name, values[0])
    if value not in values:
        raise DriverError(f'{name} must be one of {", ".join(values)}, not {value!r}')
    return value


def command(driver: str, folder: str) -> list[str]:
    """The command of a driver: the environment value, or a new build in `folder`."""
    module, package, variable = DRIVERS[driver]
    if os.environ.get(variable):
        return shlex.split(os.environ[variable])
    binary = str(Path(folder) / f'aavanam-{driver}')
    built = subprocess.run(['go', 'build', '-o', binary, package], cwd=ROOT / module, capture_output=True, text=True)
    if built.returncode != 0:
        raise DriverError(f'go build of {package} failed:\n{built.stderr[-2000:]}')
    return [binary]


class Driver:
    """One driver process and its line protocol."""

    def __init__(self, argv: list[str]):
        self.errors = tempfile.TemporaryFile()  # a file, so a driver that writes many errors does not block
        self.process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.errors)
        try:
            hello = self.read()
            if hello.get('ready') is not True:
                raise DriverError(f'the first line of the driver is not a ready line: {hello!r}')
        except BaseException:
            self.close()
            raise
        self.read_url = hello.get('read_url') or None

    def send(self, message: dict) -> None:
        try:
            self.process.stdin.write(json.dumps(message).encode() + b'\n')
            self.process.stdin.flush()
        except (BrokenPipeError, OSError) as e:
            raise DriverError(f'the driver stopped: {self.stderr()}') from e

    def read(self) -> dict:
        line = self.process.stdout.readline()
        if not line:
            raise DriverError(f'the driver stopped: {self.stderr()}')
        try:
            value = json.loads(line)
        except ValueError as e:
            raise DriverError(f'the driver wrote a line that is not JSON: {line[:200]!r}') from e
        if not isinstance(value, dict):
            raise DriverError(f'the driver wrote a line that is not a JSON object: {line[:200]!r}')
        return value

    def stderr(self) -> str:
        try:
            self.process.wait(timeout=5)  # a driver that stops writes its error first
        except subprocess.TimeoutExpired:
            pass
        self.errors.seek(0)
        return self.errors.read()[-2000:].decode(errors='replace').strip() or 'no error text'

    def request(self, message: dict, validators: dict | None = None) -> dict:
        """Send one command and return its reply. Answer each validator call that comes before the reply."""
        self.send(message)
        while True:
            reply = self.read()
            if 'validate' in reply:
                self.send(decide(reply['validate'], validators or {}))
                continue
            if 'error' in reply:
                raise DriverError(f'the driver gave an error for {message.get("cmd")}: {reply["error"]}')
            return reply

    def post(self, operation: str, request: dict) -> dict:
        """Call one operation over the network read binding (NET-TRN-1, NET-TRN-3)."""
        url = f'{self.read_url}/v1/op/{urllib.parse.quote(operation, safe="")}'
        body = json.dumps(request).encode()
        call = urllib.request.Request(url, data=body, method='POST', headers={'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(call, timeout=HTTP_TIMEOUT) as response:  # noqa: S310  the URL of the driver
                status, text = response.status, response.read()
        except urllib.error.HTTPError as e:
            e.close()
            raise DriverError(f'the read binding gave HTTP {e.code} for {operation}') from None
        except (urllib.error.URLError, OSError) as e:
            raise DriverError(f'the read binding did not answer for {operation}: {e}') from e
        if status != 200:
            raise DriverError(f'the read binding gave HTTP {status} for {operation}, not 200 (NET-TRN-3)')
        try:
            return json.loads(text)
        except ValueError as e:
            raise DriverError(f'the read binding gave a body that is not JSON for {operation}') from e

    def close(self) -> None:
        if self.process.poll() is None:
            try:
                self.process.stdin.close()
                self.process.wait(timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                self.process.kill()
                self.process.wait()
        for stream in (self.process.stdin, self.process.stdout, self.errors):
            try:
                stream.close()
            except OSError:
                pass


def decide(call, validators: dict) -> dict:
    """The answer line of one validator call (PUB-RCP-2). A validator with no answer gives `no_answer` (PUB-RCP-4)."""
    validate = validators.get(call.get('policy')) if isinstance(call, dict) else None
    if validate is None:
        return {'accepted': False}  # the store has no decision of an unknown policy, so it does not accept
    try:
        return {'accepted': validate(call.get('candidate'), call.get('receipt'), call.get('activation')) is True}
    except TimeoutError:
        return {'no_answer': True}


class BackendAdapter:
    """Connect the runner to the Go drivers, or to the Python store for a role that the environment gives it."""

    def __init__(self):
        self.writer = choice('AAVANAM_WRITER', ('backend', 'reference'))
        self.device = choice('AAVANAM_DEVICE', ('go', 'reference'))
        self.http = os.environ.get('AAVANAM_READ_HTTP', '1') != '0'
        self.validators: dict = {}
        self.local: dict = {}  # the roles that the Python store has
        self.drivers: dict[str, Driver] = {}  # role -> driver
        self.folder = tempfile.mkdtemp(prefix='aavanam-drivers-')
        atexit.register(self.close)
        try:
            if self.writer == 'backend':
                self.drivers['writer'] = Driver(command('conform', self.folder))
            if self.device == 'go':
                files = Path(self.folder) / 'device'
                files.mkdir()
                device = Driver(command('device', self.folder) + ['--dir', str(files)])
                self.drivers['device'] = self.drivers['output'] = device
            self.open_local()
        except BaseException:
            self.close()
            raise

    def open_local(self) -> None:
        """Make new Python stores for the roles of the reference implementation."""
        if self.writer == 'backend' and self.device == 'go':
            return
        sys.path.insert(0, str(ROOT / 'src'))
        from aavanam import Store  # the standard library only
        debug = bool(os.environ.get('AAVANAM_DEBUG'))
        for store in {id(s): s for s in self.local.values()}.values():
            if store.role != 'output':  # the device store closes its output store
                store.close()
        self.local = {}
        if self.writer == 'reference':
            self.local['writer'] = Store(debug=debug)
        if self.device == 'reference':
            device = Store(debug=debug, role='device')  # with its local output store (BUN-IMP-5)
            self.local['device'], self.local['output'] = device, device.output

    def processes(self) -> list[Driver]:
        return list({id(d): d for d in self.drivers.values()}.values())

    def reset(self) -> None:
        self.validators.clear()
        for driver in self.processes():
            driver.request({'cmd': 'reset'})
        self.open_local()

    def call(self, operation: str, request: dict, store: str = 'writer') -> dict:
        if store in self.local:
            return self.local[store].call(operation, request)
        driver = self.drivers[store]
        if store == 'writer' and self.http and driver.read_url and operation not in WRITES:
            return driver.post(operation, request)
        reply = driver.request({'cmd': 'call', 'store': store, 'op': operation, 'request': request}, self.validators)
        if not isinstance(reply.get('result'), dict):
            raise DriverError(f'the driver gave no result for {operation}')
        return reply['result']

    def fingerprint(self, store: str = 'writer') -> str:
        if store in self.local:
            return self.local[store].fingerprint()
        return self.drivers[store].request({'cmd': 'fingerprint', 'store': store})['fingerprint']

    def accept(self, policy, validate) -> None:
        self.validators[policy] = validate
        for store in {id(s): s for s in self.local.values()}.values():
            store.accept(policy, validate)
        for driver in self.processes():
            driver.request({'cmd': 'accept', 'policy': policy})

    def gate(self, collection) -> None:
        for store in {id(s): s for s in self.local.values()}.values():
            store.gate(collection)  # PUB-GAT-1
        for driver in self.processes():
            driver.request({'cmd': 'gate', 'collection': collection})

    def section(self, collection, section) -> None:
        if 'writer' in self.local:
            self.local['writer'].section(collection, section)  # PUB-SEC-1: only a writer store
        else:
            self.drivers['writer'].request({'cmd': 'section', 'collection': collection, 'section': section})

    def close(self) -> None:
        for driver in self.processes():
            driver.close()
        self.drivers = {}
        for store in self.local.values():
            if store.role != 'output':
                store.close()
        self.local = {}
        shutil.rmtree(self.folder, ignore_errors=True)
