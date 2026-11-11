"""An adapter to a producer store protocol with five operations, on the producer section (PRD-PRO-1).

The protocol has `put`, `get`, `append`, `read` and `forget`, objects under a key of their kind and bytes, and logs
whose records make a hash chain. The producer supplies its rules in `rules`, so this module imports no producer
code and the reference implementation stays on the standard library (constitution principle 9). The adapter is an
application component. It does the checks of the producer before each call. The store keeps opaque bytes and does
not do these checks (PRD-SEC-4).

`rules` has these attributes:

- `key(kind, data)`: the key of the bytes as the kind. A bad kind raises `ValueError`.
- `check_bytes(kind, data)`: raise `ValueError` if the bytes are not a valid object of the kind.
- `jcs(value)`: the canonical JSON text of a value.
- `LOGS`: a dict from each log word to the number of keys in a log name of that word.
- `NotFound` and `StoreError`: the error classes of the protocol. `NotFound` is a subclass of `StoreError`.

The log word `deleted` is the removal log. Only `forget` writes it, through `producer_forget` (PRD-PRO-2).
"""
from __future__ import annotations

import base64
import binascii
import json
import re

KEY = re.compile(r'sha256:[0-9a-f]{64}')
CHAIN = ('sequence', 'previous', 'entry_sha256')
REMOVALS = 'deleted'
TRIES = 8  # the appends after a log_moved (PRD-PRO-3)


class ChainedLogStore:
    """The five operations of the protocol on the producer section of `store`, for the producer `producer`."""

    def __init__(self, store, producer: str, rules):
        self.store, self.producer, self.rules = store, producer, rules

    def call(self, operation: str, **fields) -> dict:
        """Call the store. A status other than ok raises an error of the protocol (PRD-PRO-3, STS-SET-2)."""
        result = self.store.call(operation, {'producer': self.producer, **fields})
        if result['status'] == 'ok':
            return result
        codes = [r['code'] for r in result['reasons']]
        error = self.rules.NotFound if result['status'] == 'not_found' else self.rules.StoreError
        raise error(f'{operation} gave {result["status"]}: {", ".join(codes)}')

    @staticmethod
    def check_key(key) -> str:
        if not isinstance(key, str) or not KEY.fullmatch(key):
            raise ValueError(f'{key!r:.80} is not a store key')
        return key

    def check_log(self, log) -> str:
        """The word of a log name: a tuple of a log word and its keys."""
        if not (isinstance(log, tuple) and log and isinstance(log[0], str) and log[0] in self.rules.LOGS):
            raise ValueError(f'{log!r:.80} is not a log name')
        if len(log) != 1 + self.rules.LOGS[log[0]]:
            raise ValueError(f'a {log[0]} log name has {self.rules.LOGS[log[0]]} keys')
        for key in log[1:]:
            self.check_key(key)
        return log[0]

    def put(self, kind: str, data: bytes) -> str:
        self.rules.check_bytes(kind, data)
        key = self.rules.key(kind, data)
        self.call('producer_put', key=key, data=base64.b64encode(data).decode('ascii'))
        return key

    def get(self, kind: str, key: str) -> bytes:
        self.check_key(key)
        data = self.decoded(self.call('producer_get', key=key)['items'][0]['data'])
        if self.rules.key(kind, data) != key:  # an object of a different kind, or bytes that do not match the key
            raise self.rules.StoreError(f'the object {key} does not match its key as {kind}')
        return data

    def forget(self, key: str, reason: str) -> None:
        self.check_key(key)
        if not isinstance(reason, str) or not reason:
            raise ValueError('forget needs a reason')
        self.call('producer_forget', key=key, reason=reason)

    def entry_key(self, word: str, body: dict) -> str:
        return self.rules.key(f'{word}-entry@1', self.rules.jcs(body).encode('utf-8'))

    def chained(self, word: str, bodies: list[dict]) -> list[dict]:
        """Records with the chain fields, from the record bodies in order."""
        records, previous = [], None
        for n, body in enumerate(bodies, 1):
            linked = {**body, 'sequence': n, 'previous': previous}
            previous = self.entry_key(word, linked)
            records.append({**linked, 'entry_sha256': previous})
        return records

    def append(self, log: tuple, record: dict) -> str:
        word = self.check_log(log)
        if word == REMOVALS:
            raise ValueError('only forget writes the removal log')
        if not isinstance(record, dict):
            raise ValueError('a log record must be a dict')
        taken = [m for m in CHAIN if m in record]
        if taken:
            raise ValueError(f'the store sets {", ".join(taken)}: remove it from the record')
        try:
            self.rules.jcs(record)
        except (TypeError, ValueError, RecursionError) as exc:
            raise ValueError(f'a log record has no canonical JSON form: {exc}') from exc
        for _ in range(TRIES):
            records = self.read(log)
            body = {**record, 'sequence': len(records) + 1, 'previous': records[-1]['entry_sha256'] if records else None}
            head = self.entry_key(word, body)
            entry = self.rules.jcs({**body, 'entry_sha256': head}).encode('utf-8')
            result = self.store.call('producer_append', {'producer': self.producer, 'log': list(log),
                                                         'sequence': body['sequence'],
                                                         'entry': base64.b64encode(entry).decode('ascii')})
            if result['status'] == 'ok':
                return head
            if [r['code'] for r in result['reasons']] != ['log_moved']:
                raise self.rules.StoreError(f'producer_append gave {result["status"]}')
        raise self.rules.StoreError(f'the log moved {TRIES} times in one append')

    def read(self, log: tuple) -> list[dict]:
        word = self.check_log(log)
        if word == REMOVALS:  # the removal records of the store, as a log of the protocol (PRD-PRO-2)
            items = self.call('producer_removals')['items']
            return self.chained(word, [{'key': i['key'], 'reason': i['reason'], 'time': i['time']} for i in items])
        records = []
        for item in self.call('producer_read', log=list(log))['items']:
            try:
                records.append(json.loads(self.decoded(item['entry']).decode('utf-8')))
            except (UnicodeDecodeError, ValueError) as exc:
                raise self.rules.StoreError(f'entry {item["sequence"]} of the log is not JSON') from exc
        bodies = [{m: v for m, v in r.items() if m not in CHAIN} if isinstance(r, dict) else None for r in records]
        if None in bodies or self.chained(word, bodies) != records:
            raise self.rules.StoreError('the log does not continue its chain')
        return records

    def decoded(self, text: str) -> bytes:
        try:
            return base64.b64decode(text, validate=True)
        except (binascii.Error, ValueError) as exc:
            raise self.rules.StoreError('the store gave bytes that are not base64') from exc
