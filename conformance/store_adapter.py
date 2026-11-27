"""The conformance adapter of the reference implementation in src/. Run: python3 -B conformance/runner.py --adapter store_adapter:StoreAdapter"""
from __future__ import annotations

import os
import sys
from datetime import timedelta
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / 'src'))

from aavanam import Store  # noqa: E402


class StoreAdapter:
    """The request forms of conformance/README.md are the JSON API of the store. Thus each call goes through.
    The writer store is a separate Store. The device store creates the local output store (BUN-IMP-5)."""

    def __init__(self, profiles=()):
        debug = bool(os.environ.get("AAVANAM_DEBUG"))
        device = Store(debug=debug, role='device', profiles=profiles)  # with its local output store (BUN-IMP-5)
        self.stores = {'writer': Store(debug=debug, profiles=profiles), 'device': device, 'output': device.output}

    def reset(self, profiles=()) -> None:
        for store in ('writer', 'device'):  # the device store closes its output store
            self.stores[store].close()
        self.__init__(profiles)

    def declare(self, profiles) -> None:
        self.reset(profiles)  # the runner calls it after reset(), so each store is empty (LYR-PRF-1)

    def call(self, operation: str, request: dict, store: str = 'writer') -> dict:
        return self.stores[store].call(operation, request)

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
