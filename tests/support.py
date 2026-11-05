"""Helpers of the store tests. Run: python3 -B -m unittest discover -s tests"""
from __future__ import annotations

import base64
import hashlib
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path[:0] = [str(ROOT / 'src'), str(ROOT / 'tools')]

from aavanam import Store  # noqa: E402,F401
from aavanam.jsontext import canonical, parse, revision  # noqa: E402,F401

BASE = 'aavanam/base/v1'


def payload(text: str) -> tuple[dict, dict]:
    """The payload object of a text and the request payloads that carry its bytes."""
    data = text.encode('utf-8')
    key = 'sha256:' + hashlib.sha256(data).hexdigest()
    return {'digest': key, 'media_type': 'text/plain', 'size': len(data)}, {key: base64.b64encode(data).decode()}


def put(store, descriptor: dict, payloads: dict | None = None) -> dict:
    return store.call('put', {'descriptors': [json.dumps(descriptor)], 'payloads': payloads or {}})


def put_ref(store, descriptor: dict, payloads: dict | None = None) -> str:
    result = put(store, descriptor, payloads)
    assert result['status'] == 'ok', result
    return result['items'][0]['ref']


def receipt(store, text: str = 'accepted') -> str:
    obj, data = payload(text)
    return put_ref(store, {'descriptor_profile': BASE, 'kind': 'receipt', 'producer': 'example/reviewer', 'payload': obj}, data)


def snapshot(store, name: str, collection: str = 'sample-collection') -> str:
    result = store.call('create_snapshot', {'collection': collection, 'logical_id': name})
    assert result['status'] == 'ok', result
    return result['items'][0]['ref']


def candidate(store, snap: str, policy: str = 'sample-policy', expected_head=None) -> str:
    result = store.call('create_candidate', {'snapshot': snap, 'policy': policy, 'expected_head': expected_head})
    assert result['status'] == 'ok', result
    return result['items'][0]['candidate']
