"""The descriptor checks that need no store: STS-MAL-3 steps 2 and 3, and the normal form (IDN-CAN-1)."""
from __future__ import annotations

import re
import unicodedata

from .jsontext import is_digest, is_ijson, valid_string
from .kinds import KINDS, REF_TYPES, SETS, schema

BYTES = 'aavanam/bytes/v1'
NFC_TEXT = 'aavanam/nfc-text/v1'  # ADP-SEL-4
LABEL = re.compile(r'[a-z0-9.-]+/[a-z0-9.-]+/v[1-9][0-9]*')  # a completeness label (LCK-CMPL-1)


class Refusal(Exception):
    """A check that fails. `codes` has one code for each affected value. `refs` names the affected references."""

    def __init__(self, codes: list[str], refs: list[str] | None = None):
        super().__init__(codes[0])
        self.codes = codes
        self.refs = refs or []


def is_int(value) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)


def is_selector(value) -> bool:
    """Check the form of a selector (REL-SEL-1, REL-SEL-3, ADP-SEL-4)."""
    if not (isinstance(value, dict) and isinstance(value.get('selector_profile'), str)):
        return False
    if value['selector_profile'] not in (BYTES, NFC_TEXT):
        return True
    return (set(value) == {'selector_profile', 'start', 'end'} and is_int(value['start']) and is_int(value['end'])
            and 0 <= value['start'] <= value['end'])


def decode_nfc(data: bytes) -> str:
    """Decode checked reading bytes without changing their coordinates (ADP-SEL-4)."""
    text = data.decode('utf-8')
    if not unicodedata.is_normalized('NFC', text):
        raise ValueError('the reading is not NFC')
    return text


def is_payload(value) -> bool:
    return (isinstance(value, dict) and set(value) == {'digest', 'media_type', 'size'} and is_digest(value['digest'])
            and isinstance(value['media_type'], str) and is_int(value['size']) and value['size'] >= 0)


def is_ranges(value) -> bool:
    """The ranges of an excerpt (BUN-EXC-1, BUN-EXC-2)."""
    if not (isinstance(value, list) and value):
        return False
    end = 0
    for r in value:
        if not (isinstance(r, dict) and set(r) == {'start', 'end'} and is_int(r['start']) and is_int(r['end'])
                and end <= r['start'] <= r['end']):
            return False
        end = r['end']
    return True


def agrees(vtype: str, value, field: str) -> bool:
    if vtype == 'ref':
        return is_digest(value)
    if vtype == 'ref-map':
        return isinstance(value, dict) and all(isinstance(k, str) and valid_string(k) and is_digest(v) for k, v in value.items())
    if vtype == 'payload':
        return is_payload(value)
    if vtype == 'string':
        if field == 'completeness':
            return isinstance(value, str) and LABEL.fullmatch(value) is not None  # LCK-CMPL-1
        return isinstance(value, str) and valid_string(value) and (field != 'logical_id' or value != '')  # IDN-LOG-1
    if vtype == 'selector':
        return is_selector(value) or (field == 'operand_selectors' and value is None)  # KND-SCH-7
    return is_ijson(value)  # json: a value that is not I-JSON is not of the type (KND-SCH-4)


def check(value) -> dict:
    """Return the normal form of a descriptor value, or raise Refusal (STS-MAL-3 steps 2 and 3)."""
    if not isinstance(value, dict):
        raise Refusal(['field_type'])
    kind = value.get('kind')
    if 'kind' not in value:
        raise Refusal(['field_count'])
    if not isinstance(kind, str):
        raise Refusal(['field_type'])
    if kind not in KINDS:
        raise Refusal(['unknown_kind'])  # KND-REG-3
    fields = schema(kind)
    unknown = [f for f in value if f not in fields]
    if unknown:
        raise Refusal(['unknown_field'] * len(unknown))  # KND-SCH-2
    normal, counts, types = {}, [], []
    for field, (vtype, count, _) in fields.items():
        if field not in value:
            if count in ('1', '1..n', '1..n list'):
                counts.append(field)
            continue
        v = value[field]
        many = count not in ('1', '0..1') and vtype != 'ref-map'
        if not many:
            if not agrees(vtype, v, field) or (field == 'ranges' and kind == 'excerpt' and not is_ranges(v)):
                types.append(field)
            normal[field] = v
            continue
        if not isinstance(v, list):
            types.append(field)
            continue
        if not v and count.startswith('1'):
            counts.append(field)
            continue
        types += [field for x in v if not agrees(vtype, x, field)]
        if count in SETS and len(set(map(repr, v))) != len(v):
            counts.append(field)  # a set has no duplicates (KND-SCH-5)
        if v:
            normal[field] = sorted(v) if count in SETS and all(isinstance(x, str) for x in v) else v
    selectors = normal.get('operand_selectors')
    if kind == 'derivation' and selectors is not None and isinstance(normal.get('operands'), list) \
            and len(selectors) != len(normal['operands']):
        counts.append('operand_selectors')  # KND-SCH-7
    if counts:
        raise Refusal(['field_count'] * len(counts))
    if types:
        raise Refusal(['field_type'] * len(types))
    return normal


def references(descriptor: dict) -> list[tuple[str, str]]:
    """Return each (field, reference) of the descriptor, the source of `refs` (KND-DEP-1)."""
    out = []
    for field, (vtype, _, _) in schema(descriptor['kind']).items():
        if vtype not in REF_TYPES or field not in descriptor:
            continue
        v = descriptor[field]
        values = v.values() if vtype == 'ref-map' else v if isinstance(v, list) else [v]
        out += [(field, r) for r in values]
    return out


def targets(kind: str, field: str) -> tuple[str, ...]:
    return schema(kind)[field][2]


def payloads(descriptor: dict) -> list[dict]:
    """Return the payload objects of the descriptor (KND-DEP-3)."""
    return [descriptor[f] for f, (vtype, _, _) in schema(descriptor['kind']).items() if vtype == 'payload' and f in descriptor]
