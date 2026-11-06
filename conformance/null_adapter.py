"""The null implementation: each call returns `unsupported`.

The null implementation supports no operation. It must fail the vectors.
If it passes a vector that expects a status other than `unsupported`, the
runner or the vector has a defect.
"""
from __future__ import annotations

from adapter import envelope


class NullAdapter:
    def reset(self) -> None:
        pass

    def call(self, operation: str, request: dict, store: str = 'writer') -> dict:
        return envelope(operation, 'unsupported', ['unsupported_operation'])

    def fingerprint(self, store: str = 'writer') -> str:
        return ''

    def accept(self, policy, validate) -> None:
        pass

    def gate(self, collection) -> None:
        pass

    def section(self, collection, section) -> None:
        pass
