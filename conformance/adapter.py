"""The adapter interface between the conformance runner and an implementation."""
from __future__ import annotations

from typing import Callable, Protocol

PROFILE = 'aavanam/base/v1'


class Adapter(Protocol):
    """Connect one implementation to the runner.

    A vector has three stores: `writer`, `device` and `output`
    (conformance/README.md). The runner calls `reset` before each vector,
    and `reset` empties all three stores. A call request is a JSON object in
    the form that conformance/README.md gives. `call` sends it to the given
    store and returns the result envelope of STS-ENV-1 as a JSON object.
    `fingerprint` returns a text that changes if the content of the given
    store changes. `accept`
    registers an acceptance policy (PUB-RCP-1): a validator of a candidate
    identity, a receipt reference and an activation reference or None. The
    validator raises TimeoutError if it does not answer before its deadline
    (PUB-RCP-4).
    `gate` declares a collection as gated
    (PUB-GAT-1). `section` declares the storage section of a collection
    (PUB-SEC-1). `declare` makes each of the three stores declare the given
    profiles, for example the layer profile (LYR-PRF-1). The runner calls it
    after `reset`, only for a vector whose profile is not the base profile.
    After `reset`, a store declares no profile. If the `bundle` of an
    `import` request is a byte sequence, `call` gives it to the
    implementation as a raw bundle in place of the request (BUN-RAW-4).
    `raw_bundle` returns the raw form that the implementation of the given
    store makes from a bundle (BUN-RAW-3).
    """

    def reset(self) -> None: ...

    def call(self, operation: str, request: dict, store: str = 'writer') -> dict: ...

    def fingerprint(self, store: str = 'writer') -> str: ...

    def accept(self, policy: str, validate: Callable[..., bool]) -> None: ...

    def gate(self, collection: str) -> None: ...

    def section(self, collection: str, section: str) -> None: ...

    def declare(self, profiles: list[str]) -> None: ...

    def raw_bundle(self, bundle: dict, store: str = 'writer') -> bytes: ...


def envelope(operation: str, status: str, codes: list[str], items: list[dict] | None = None) -> dict:
    """Return a result envelope with one reason for each code in `codes`."""
    result = {
        'status': status,
        'reasons': [{'code': c, 'refs': [], 'capabilities': []} for c in codes],
        'operation': operation,
        'operation_profile': PROFILE,
        'context': None,
    }
    if items is not None:
        result['items'] = items
    return result
