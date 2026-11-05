"""JSON form checks, the canonical encoding and digests (STS-MAL-3 step 1, IDN-CAN-1, IDN-DIG-1)."""
from __future__ import annotations

import hashlib
import json
import math
import re
from decimal import Decimal, InvalidOperation

DIGEST = re.compile(r'sha256:[0-9a-f]{64}')


class FormError(Exception):
    """A JSON text that fails a check of STS-MAL-3 step 1. `codes` has one code for each affected value."""

    def __init__(self, codes: list[str]):
        super().__init__(codes[0])
        self.codes = codes


def digest(data: bytes) -> str:
    return 'sha256:' + hashlib.sha256(data).hexdigest()


def is_digest(value) -> bool:
    return isinstance(value, str) and DIGEST.fullmatch(value) is not None


def kept(text: str) -> bool:
    """A number that is finite as a double and that its canonical form keeps exactly (STS-MAL-3 step 1)."""
    x = float(text)
    if not math.isfinite(x):
        return False
    try:
        return Decimal(text) == Decimal(es_number(x))
    except InvalidOperation:  # an exponent outside the range of Decimal, so the double is 0
        return not any(c in '123456789' for c in text.lower().split('e')[0])


def number_value(text: str):
    """The value of a number with a fraction or an exponent. An integral value is an integer, so the canonical
    form `1e+21` of the integer 10**21 gives the same value (IDN-CAN-1)."""
    x = float(text)
    if not x.is_integer():
        return x
    try:
        return int(Decimal(text))  # exact: kept() compares this value with the canonical form
    except (InvalidOperation, OverflowError, ValueError):
        return int(x)


def valid_string(text: str) -> bool:
    """I-JSON (RFC 7493 section 2.1): no lone surrogates and no Unicode noncharacters."""
    try:
        text.encode('utf-8')
    except UnicodeEncodeError:
        return False
    return not any(0xFDD0 <= ord(c) <= 0xFDEF or ord(c) & 0xFFFE == 0xFFFE for c in text)


def parse(text: str):
    """Return the value of a JSON text. Raise FormError with the codes of the first check that fails."""
    duplicates, outside = [], []

    def pairs(items):
        keys = [k for k, _ in items]
        duplicates.extend(k for i, k in enumerate(keys) if k in keys[:i])
        return dict(items)

    def number(s):
        if not kept(s):
            outside.append(s)
            return 0
        return number_value(s)

    def integer(s):
        if not kept(s):
            outside.append(s)
            return 0  # the parse fails, so the value has no use, and a long token is not converted
        return int(s)

    def constant(s):
        raise ValueError(s)  # NaN and Infinity are not JSON

    try:
        value = json.loads(text, object_pairs_hook=pairs, parse_float=number, parse_int=integer, parse_constant=constant)
    except (ValueError, RecursionError):
        raise FormError(['malformed_json']) from None
    if duplicates:
        raise FormError(['duplicate_key'] * len(duplicates))
    outside += [s for s in strings(value) if not valid_string(s)]
    if outside:
        raise FormError(['non_ijson_value'] * len(outside))
    return value


def strings(value):
    """Yield each key and each string value, with no recursion."""
    todo = [value]
    while todo:
        value = todo.pop()
        if isinstance(value, str):
            yield value
        elif isinstance(value, dict):
            yield from value
            todo += value.values()
        elif isinstance(value, list):
            todo += value


def is_ijson(value) -> bool:
    """A JSON value that agrees with I-JSON (RFC 7493), with no recursion."""
    todo = [value]
    while todo:
        value = todo.pop()
        if isinstance(value, dict):
            if not all(isinstance(k, str) and valid_string(k) for k in value):
                return False
            todo += value.values()
        elif isinstance(value, list):
            todo += value
        elif isinstance(value, str):
            if not valid_string(value):
                return False
        elif isinstance(value, bool) or value is None:
            continue
        elif isinstance(value, int):
            if not kept(str(value)):
                return False
        elif not (isinstance(value, float) and math.isfinite(value)):  # a double keeps its canonical form
            return False
    return True


def detached(value):
    """Return a copy of a JSON value that shares no object or list with it, with no recursion."""
    top: list = [None]
    todo = [(top, 0, value)]
    while todo:
        parent, key, value = todo.pop()
        if isinstance(value, dict):
            parent[key] = {}
            todo += [(parent[key], k, v) for k, v in value.items()]
        elif isinstance(value, list):
            parent[key] = [None] * len(value)
            todo += [(parent[key], i, v) for i, v in enumerate(value)]
        else:
            parent[key] = value
    return top[0]


def es_number(x: float) -> str:
    """Return the ECMAScript form of a finite number (RFC 8785 section 3.2.2.3)."""
    if x == 0:
        return '0'
    if x < 0:
        return '-' + es_number(-x)
    _, digits, exp = Decimal(repr(x)).as_tuple()
    d = ''.join(map(str, digits)).rstrip('0')
    k, n = len(d), len(digits) + exp
    if k <= n <= 21:
        return d + '0' * (n - k)
    if 0 < n <= 21:
        return d[:n] + '.' + d[n:]
    if -6 < n <= 0:
        return '0.' + '0' * -n + d
    return (d if k == 1 else d[0] + '.' + d[1:]) + 'e' + f'{n - 1:+d}'


class _Token(str):
    """A part of the canonical text that is already encoded."""


def canonical(value) -> str:
    """Return the RFC 8785 form of a JSON value, with no recursion (IDN-CAN-1)."""
    out, todo = [], [value]
    while todo:
        value = todo.pop()
        if isinstance(value, _Token):
            out.append(value)
        elif isinstance(value, dict):
            parts = []
            for k in sorted(value, key=lambda k: k.encode('utf-16-be')):
                parts += [_Token(','), _Token(json.dumps(k, ensure_ascii=False) + ':'), value[k]]
            todo += [_Token('}'), *reversed(parts[1:]), _Token('{')]
        elif isinstance(value, list):
            parts = [x for v in value for x in (_Token(','), v)]
            todo += [_Token(']'), *reversed(parts[1:]), _Token('[')]
        elif isinstance(value, bool) or value is None or isinstance(value, str):
            out.append(json.dumps(value, ensure_ascii=False))
        else:
            out.append(es_number(float(value)))
    return ''.join(out)


def revision(value) -> str:
    """Return the digest of the canonical form in UTF-8 (IDN-CAN-2)."""
    return digest(canonical(value).encode('utf-8'))
