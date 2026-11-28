"""The conformance adapter of the reference implementation in src/. Run: python3 -B conformance/runner.py --adapter store_adapter:StoreAdapter"""
from __future__ import annotations

import json
import os
import shutil
import sys
import tempfile
from datetime import timedelta
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / 'src'))

from aavanam import Store  # noqa: E402
from aavanam.store import raw_form  # noqa: E402


def release_stores(release: str, folder: str, stores: dict, roles=('writer', 'device')) -> tuple[dict, dict]:
    """Replace the stores of `roles` with stores on copies of the files of a release store, in `folder`.
    Return the stores and the context identities of each new store. The fixture files do not change."""
    source = ROOT / 'fixtures' / 'releases' / release / 'reference'
    recorded = json.loads((source / 'contexts.json').read_text())
    stores, contexts = dict(stores), {}
    debug, profiles = bool(os.environ.get('AAVANAM_DEBUG')), next(iter(stores.values())).profiles
    for role in roles:
        stores[role].close()  # the device store closes its output store
        names = ('device', 'output') if role == 'device' else (role,)
        paths = {n: os.path.join(folder, f'{n}.sqlite') for n in names}
        for name, path in paths.items():
            shutil.copyfile(source / f'{name}.sqlite', path)
            contexts[name] = recorded.get(name, {})
        if role == 'device':
            stores['device'] = Store(paths['device'], debug=debug, role='device', output_path=paths['output'], profiles=profiles)
            stores['output'] = stores['device'].output
        else:
            stores[role] = Store(paths[role], debug=debug, role=role, profiles=profiles)
    return stores, contexts


class StoreAdapter:
    """The request forms of conformance/README.md are the JSON API of the store. Thus each call goes through.
    The writer store is a separate Store. The device store creates the local output store (BUN-IMP-5)."""

    def __init__(self, profiles=()):
        debug = bool(os.environ.get("AAVANAM_DEBUG"))
        device = Store(debug=debug, role='device', profiles=profiles)  # with its local output store (BUN-IMP-5)
        self.stores = {'writer': Store(debug=debug, profiles=profiles), 'device': device, 'output': device.output}

    copies = None  # the folder with the copies of the files of a release store

    def reset(self, profiles=()) -> None:
        for store in ('writer', 'device'):  # the device store closes its output store
            self.stores[store].close()
        if self.copies:
            self.copies.cleanup()
            self.copies = None
        self.__init__(profiles)

    def restore(self, release: str) -> dict:
        """Open copies of the three files that the reference store of a release wrote (MIG-VER-1). Return the
        context identities of each store, by the handles of the release. The runner calls it after reset()."""
        self.reset(self.stores['writer'].profiles)
        self.copies = tempfile.TemporaryDirectory(prefix='aavanam-release-')
        self.stores, contexts = release_stores(release, self.copies.name, self.stores)
        return contexts

    def declare(self, profiles) -> None:
        self.reset(profiles)  # the runner calls it after reset(), so each store is empty (LYR-PRF-1)

    def call(self, operation: str, request: dict, store: str = 'writer') -> dict:
        if operation == 'import' and isinstance(request.get('bundle'), bytes):
            return self.stores[store].call(operation, request['bundle'])  # BUN-RAW-4: a raw bundle in place of the request
        return self.stores[store].call(operation, request)

    def raw_bundle(self, bundle: dict, store: str = 'writer') -> bytes:
        return raw_form(bundle)  # BUN-RAW-3: the form needs no store

    def fingerprint(self, store: str = 'writer') -> str:
        return self.stores[store].fingerprint()

    def accept(self, policy, validate) -> None:
        for store in self.stores.values():
            store.accept(policy, validate)

    def gate(self, collection) -> None:
        for store in self.stores.values():
            store.gate(collection)  # PUB-GAT-1

    def advance(self, seconds: int) -> None:
        for store in self.stores.values():  # CTX-LIF-2: each store clock moves forward
            store.clock = lambda base=store.clock: base() + timedelta(seconds=seconds)

    def section(self, collection, section) -> None:
        self.stores['writer'].section(collection, section)  # PUB-SEC-1: only a writer store
