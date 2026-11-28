"""Make the stored forms of a release store (fixtures/README.md, "Release stores").

The definition is fixtures/releases/<release>/release.json in this checkout. This tool sends its steps to the
implementations of a second checkout, at the tag of the release. Thus that release writes the stores:

- the reference store of `src/`, as three SQLite files;
- the backend on PostgreSQL, as a dump with the schema and the rows;
- the device store of `reference/`, as the two files of document 21.

Usage, with a database that only this run uses:

    git worktree add --detach <folder> v0.6.0
    python3 -B tools/make_release_store.py --release 0.6.0 --source <folder> \\
        --pg-dsn 'postgres://<user>:<password>@127.0.0.1:5432/<database>?sslmode=disable' \\
        --pg-dump 'pg_dump --dbname=<database>'

`--pg-dump` is the command that prints the dump of that database. The tool adds the options of the dump.
The tool needs `go` for the two drivers of the release. It uses the standard library only.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shlex
import shutil
import sqlite3
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DUMP_OPTIONS = ['--inserts', '--no-owner', '--no-privileges']


class Recorder:
    """Keep the result of each call, in the order of the steps."""

    results: list

    def call(self, operation, request, store='writer'):
        result = super().call(operation, request, store=store)
        self.results.append(result)
        return result


def run(runner, adapter, definition: dict) -> tuple[dict, dict]:
    """Send the steps to the adapter with the runner of the release. Return the context identities of each
    store and the exported bundles, by the handles of the steps."""
    vector = {'format': 'aavanam-vector/v1', 'title': definition['title'], 'clauses': [], 'profile': 'aavanam/base/v1',
              'provenance': {'type': 'spec', 'note': 'the definition of a release store'},
              **{f: definition[f] for f in ('payloads', 'acceptance', 'gates', 'sections', 'steps') if f in definition}}
    adapter.results = []
    problems = runner.run_vector(adapter, 'release', vector)
    if problems or len(adapter.results) != len(definition['steps']):
        raise SystemExit('the release did not give the expected result of each step:\n' + '\n'.join(problems))
    contexts: dict[str, dict] = {}
    bundles = {}
    for step, result in zip(definition['steps'], adapter.results):
        if 'as_context' in step:
            contexts.setdefault(step.get('store', 'writer'), {})[step['as_context']] = result['context']
        if 'as_bundle' in step:
            bundles[step['as_bundle']] = result['bundle']
    return contexts, bundles


def write_json(path: Path, value) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True, ensure_ascii=False) + '\n')


def reference(runner, source: Path, out: Path, definition: dict) -> dict:
    """The reference store of the release writes three files. Return the bundles."""
    from aavanam import Store  # the store of the release checkout
    import store_adapter
    folder = out / 'reference'
    folder.mkdir(parents=True, exist_ok=True)

    class FileAdapter(Recorder, store_adapter.StoreAdapter):
        def __init__(self, profiles=()):
            for name in ('writer', 'device', 'output'):
                (folder / f'{name}.sqlite').unlink(missing_ok=True)
            device = Store(str(folder / 'device.sqlite'), role='device', output_path=str(folder / 'output.sqlite'), profiles=profiles)
            self.stores = {'writer': Store(str(folder / 'writer.sqlite'), profiles=profiles), 'device': device, 'output': device.output}

    adapter = FileAdapter()
    contexts, bundles = run(runner, adapter, definition)
    for name in ('writer', 'device'):  # the device store closes its output store
        adapter.stores[name].close()
    write_json(folder / 'contexts.json', contexts)
    return bundles


def checkpoint(path: Path) -> None:
    """Move the content of the write-ahead log of a SQLite file into the file. The driver of the release
    does not close its files, so the log has the last transactions."""
    db = sqlite3.connect(path)
    try:
        busy, _, _ = db.execute('PRAGMA wal_checkpoint(TRUNCATE)').fetchone()
        if busy:
            raise SystemExit(f'{path.name}: a different program has the file open')
    finally:
        db.close()
    for suffix in ('-wal', '-shm'):
        Path(str(path) + suffix).unlink(missing_ok=True)


def go(runner, source: Path, out: Path, definition: dict, dsn: str, dump: list[str]) -> dict:
    """The backend of the release writes to PostgreSQL, and its device store writes the two files of
    document 21. Return the bundles."""
    import backend_adapter
    work = Path(tempfile.mkdtemp(prefix='aavanam-release-'))
    try:
        for module, name in (('backend', 'aavanam-conform'), ('reference', 'aavanam-device')):
            built = subprocess.run(['go', 'build', '-o', str(work / name), f'./cmd/{name}'], cwd=source / module,
                                   capture_output=True, text=True)
            if built.returncode != 0:
                raise SystemExit(f'go build of {name} failed:\n{built.stderr[-2000:]}')
        os.environ.update({'AAVANAM_WRITER': 'backend', 'AAVANAM_DEVICE': 'go', 'AAVANAM_STORAGE': 'pg', 'AAVANAM_PG_DSN': dsn,
                           'AAVANAM_CONFORM_BIN': shlex.join([str(work / 'aavanam-conform')]),
                           'AAVANAM_DEVICE_BIN': shlex.join([str(work / 'aavanam-device')])})

        class Adapter(Recorder, backend_adapter.BackendAdapter):
            pass

        adapter = Adapter()
        try:
            contexts, bundles = run(runner, adapter, definition)
            for driver in adapter.processes():
                driver.close()  # each driver ends, so no program has the files or the database open
            files = Path(adapter.folder) / 'device'
            (out / 'device').mkdir(parents=True, exist_ok=True)
            for name in ('device.sqlite', 'output.sqlite'):
                checkpoint(files / name)
                shutil.copyfile(files / name, out / 'device' / name)
        finally:
            adapter.close()
        dumped = subprocess.run(dump + DUMP_OPTIONS, capture_output=True, text=True)
        if dumped.returncode != 0:
            raise SystemExit(f'the dump command failed:\n{dumped.stderr[-2000:]}')
        # A line that starts with a backslash is a command of the client program, not SQL. The two comment
        # lines of the dump program name the system that made the dump, which is not content of the store.
        text = ''.join(line for line in dumped.stdout.splitlines(keepends=True) if not line.startswith(('\\', '-- Dumped ')))
        (out / 'backend').mkdir(parents=True, exist_ok=True)
        (out / 'backend' / 'postgresql.sql').write_text(text)
        write_json(out / 'backend' / 'contexts.json', {'writer': contexts.get('writer', {})})
        write_json(out / 'device' / 'contexts.json', {'device': contexts.get('device', {})})
        return bundles
    finally:
        shutil.rmtree(work, ignore_errors=True)


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--release', required=True, help='the release, for example 0.6.0')
    parser.add_argument('--source', required=True, type=Path, help='a checkout at the tag of the release')
    parser.add_argument('--pg-dsn', default=os.environ.get('AAVANAM_PG_DSN', ''), help='the database of this run (default: AAVANAM_PG_DSN)')
    parser.add_argument('--pg-dump', required=True, help='the command that prints the dump of that database')
    args = parser.parse_args(argv)
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', args.release):
        raise SystemExit('the release is not a version number')
    out = ROOT / 'fixtures' / 'releases' / args.release
    definition = json.loads((out / 'release.json').read_text())
    source = args.source.resolve()
    head = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=source, capture_output=True, text=True).stdout.strip()
    if head != definition['commit']:
        raise SystemExit(f'the checkout is at {head}, not at the commit {definition["commit"]} of the definition')
    if subprocess.run(['git', 'status', '--porcelain'], cwd=source, capture_output=True, text=True).stdout.strip():
        raise SystemExit('the checkout of the release has local changes, so it is not the release')
    if not args.pg_dsn:
        raise SystemExit('the backend needs --pg-dsn or AAVANAM_PG_DSN')
    for part in ('src', 'tools', 'conformance'):  # the modules of the release come before the modules of this checkout
        sys.path.insert(0, str(source / part))
    sys.dont_write_bytecode = True
    import runner  # the runner of the release checks the result of each step
    first = reference(runner, source, out, definition)
    second = go(runner, source, out, definition, args.pg_dsn, shlex.split(args.pg_dump))
    if first != second:
        raise SystemExit('the reference store and the backend of the release exported different bundles')
    for name, bundle in first.items():
        write_json(out / 'bundles' / f'{name}.json', bundle)
    for path in sorted(p for p in out.rglob('*') if p.is_file()):
        print(f'{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.stat().st_size:>8}  {path.relative_to(out)}')
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
