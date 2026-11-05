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
    identity, a receipt reference and an activation reference or None.
    `gate` declares a collection as gated
    (PUB-GAT-1).
    """

    def reset(self) -> None: ...

    def call(self, operation: str, request: dict, store: str = 'writer') -> dict: ...

    def fingerprint(self, store: str = 'writer') -> str: ...

    def accept(self, policy: str, validate: Callable[..., bool]) -> None: ...

    def gate(self, collection: str) -> None: ...


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
