"""Consumer mapping of a content record to PUB-ACT-1. This file uses only the standard library.

The consumer tables keep store objects and candidates by their declared byte references.
This example checks no signature and fetches no external receipt content.
"""
from __future__ import annotations

import hashlib
import json
import re

from aavanam.jsontext import canonical

BASE = 'aavanam/base/v1'
REF = r'sha256:[0-9a-f]{64}'
NAME = r'[A-Za-z0-9._-]{1,128}'
MEMBERS = ('units', 'schemas', 'relation_sets', 'overlay_sets', 'observations', 'native_mappings')


def digest(data: bytes) -> str:
    return 'sha256:' + hashlib.sha256(data).hexdigest()


def require(ok, reason: str) -> None:
    if not ok:
        raise ValueError(reason)


def matches(value, pattern: str) -> bool:
    return isinstance(value, str) and re.fullmatch(pattern, value) is not None


def json_object(text: str) -> dict:
    """Refuse duplicate member names, non-JSON numbers and a value that is not an object."""
    def pairs(items):
        out = {}
        for key, value in items:
            require(key not in out, 'json')
            out[key] = value
        return out

    def non_json(value):
        raise ValueError('json')

    try:
        out = json.loads(text, object_pairs_hook=pairs, parse_constant=non_json)
    except (ValueError, RecursionError) as error:
        raise ValueError('json') from error
    require(isinstance(out, dict), 'json')
    return out


def check_manifest(text: str) -> dict:
    """Apply the content manifest rules at commit 4d9d66e, without a schema package."""
    require(len(text.encode('utf-8')) <= 262144, 'too_large')
    obj = json_object(text)
    require(matches(obj.get('format'), r'entelos.content/[1-9][0-9]*'), 'format')
    require(obj['format'] == 'entelos.content/1', 'unsupported_content_format')
    require(obj.keys() <= {'format', 'collection', 'partition', 'base', 'items', 'metadata', 'evidence'}, 'unknown_field')
    require({'format', 'collection', 'partition', 'base', 'items'} <= obj.keys(), 'schema')
    require(all(matches(obj[k], NAME) for k in ('collection', 'partition')), 'schema')
    require(matches(obj['base'], '(' + REF + ')?'), 'schema')
    items = obj['items']
    require(isinstance(items, list) and len(items) >= 1, 'schema')
    for item in items:
        require(isinstance(item, dict), 'schema')
        require(item.keys() <= {'item_id', 'digest', 'size', 'media_type'}, 'unknown_field')
        require({'item_id', 'digest', 'size'} <= item.keys(), 'schema')
        require(matches(item['item_id'], NAME) and matches(item['digest'], REF), 'schema')
        require(type(item['size']) is int and item['size'] >= 0, 'schema')
        if 'media_type' in item:
            require(matches(item['media_type'], r'[a-z0-9][a-z0-9.+-]{0,63}/[a-z0-9][a-z0-9.+-]{0,63}'), 'schema')
    if 'evidence' in obj:
        evidence = obj['evidence']
        require(isinstance(evidence, dict), 'schema')
        require(evidence.keys() <= {'format', 'digest'}, 'unknown_field')
        require(set(evidence) == {'format', 'digest'} and evidence['format'] == 'entelos.evidence/1'
                and matches(evidence['digest'], REF), 'schema')
    metadata = obj.get('metadata', {})
    require(isinstance(metadata, dict), 'schema')
    require(all(isinstance(v, str) and len(v) <= 1024 for v in metadata.values()), 'schema')
    require(len(items) <= 1024, 'too_many_items')
    names = [item['item_id'] for item in items]
    require(len(names) == len(set(names)), 'duplicate_item')
    require(names == sorted(names), 'item_order')
    require(len(metadata) <= 64, 'too_many_metadata')
    require(all(matches(k, NAME) for k in metadata), 'metadata_name')
    return obj


def stored_item(store, ref: str, item_id: str, objects: dict) -> dict:
    """Read the descriptor in canonical UTF-8 form (IDN-CAN-1, IDN-CAN-2)."""
    descriptor = store.descriptor(ref)
    require(descriptor is not None, 'object_not_found')
    data = canonical(descriptor).encode('utf-8')
    key = digest(data)
    require(key == ref, 'object_mismatch')
    objects[key] = (ref, data)
    return {'item_id': item_id, 'digest': key, 'size': len(data)}


def manifest_text(store, snapshot: str, partition: str, base: str, lock: str, objects: dict) -> str:
    """List the snapshot members and the stored build lock in ascending item_id order."""
    snap = store.descriptor(snapshot)
    members = {ref for field in MEMBERS for ref in snap.get(field, [])}
    items = [stored_item(store, ref, 'member.' + ref.removeprefix('sha256:'), objects) for ref in sorted(members)]
    items.append(stored_item(store, lock, 'inputs.lock', objects))
    return json.dumps({'format': 'entelos.content/1', 'collection': snap['collection'], 'partition': partition,
                       'base': base, 'items': sorted(items, key=lambda item: item['item_id'])}, separators=(',', ':'))


def resolve(record: dict, manifests: dict, consumer: dict) -> dict:
    """Resolve each dependency from a typed record field. Refuse before any store call.

    candidate.ref -> manifest -> inputs.lock -> stored lock bytes and reference.
    candidate.ref -> consumer candidate; base -> consumer published snapshot.
    decision_ref -> approval wrapper; report_ref -> report wrapper.
    Receipt content stays in the producer. The wrappers contain only its record references.
    """
    require(record['candidate']['kind'] == 'content', 'unsupported_candidate_kind')
    ref = record['candidate']['ref']
    require(ref in manifests, 'candidate_unknown')
    text = manifests[ref]
    require(digest(text.encode('utf-8')) == ref, 'candidate_mismatch')
    obj = check_manifest(text)
    for key in ('collection', 'partition', 'base'):
        require(obj[key] == record[key], key + '_mismatch')
    locks = [item for item in obj['items'] if item['item_id'] == 'inputs.lock']
    require(len(locks) == 1, 'missing_lock')
    item = locks[0]
    stored = consumer['objects'].get(item['digest'])
    require(stored is not None, 'lock_not_found')
    lock, data = stored
    require(digest(data) == item['digest'] and len(data) == item['size'], 'lock_not_found')
    require(json_object(data.decode('utf-8')).get('kind') == 'build-lock', 'lock_kind')
    require(ref in consumer['candidates'], 'candidate_unknown')
    base = record['base']
    require(not base or base in consumer['published'], 'base_unknown')
    snapshot = consumer['published'][base] if base else None
    # A repeat of an earlier publication is safe, also after a later head (PUB-HED-5).
    require(ref in consumer['published'] or snapshot == consumer['head'], 'base_not_current')
    require(all(record[k] in consumer['receipts'] for k in ('decision_ref', 'report_ref')), 'receipt_unknown')
    out = {'descriptor_profile': BASE, 'kind': 'activation', 'candidate': consumer['candidates'][ref],
           'lock': lock, 'approval': consumer['receipts'][record['decision_ref']],
           'report': consumer['receipts'][record['report_ref']], 'partition': record['partition'],
           'effective_from': record['from'], 'effective_until': record['until'], 'request_key': record['request_key'],
           'producer': record['producer'], 'runner': record['runner'], 'approver': record['approver']}
    if base:
        out['base'] = snapshot
    return out
