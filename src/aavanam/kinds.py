"""The kind table of the base profile (document 02). tests/test_kinds.py checks it against document 02.

Each field has its value type, its count and its target kinds (KND-SCH-1, KND-REF-3). The groups of the
targets column are expanded.
"""
from __future__ import annotations

BASE_PROFILE = 'aavanam/base/v1'
REF_TYPES = ('ref', 'ref-map')
SETS = ('0..n', '1..n')  # sets of KND-SCH-5; a count with 'list' keeps its order

COMMON = {
    'descriptor_profile': ('string', '1', ()),
    'kind': ('string', '1', ()),
    'logical_id': ('string', '0..1', ()),
    'ext': ('json', '0..1', ()),
}

KINDS = {
    'source': {
        'payload': ('payload', '1', ()),
    },
    'reading': {
        'source': ('ref', '1', ('source',)),
        'producer': ('string', '1', ()),
        'producer_config': ('payload', '0..1', ()),
        'coverage_profile': ('string', '1', ()),
        'payload': ('payload', '1', ()),
    },
    'excerpt': {
        'parent': ('ref', '1', ('reading',)),
        'ranges': ('json', '1', ()),
        'payload': ('payload', '1', ()),
    },
    'schema': {
        'schema_profile': ('string', '1', ()),
        'annotations': ('ref', '0..1', ('artifact',)),
        'payload': ('payload', '1', ()),
    },
    'data-resource': {
        'data_profile': ('string', '1', ()),
        'schema': ('ref', '1', ('schema',)),
        'payload': ('payload', '1', ()),
    },
    'artifact': {
        'artifact_type': ('string', '1', ()),
        'schema': ('ref', '0..1', ('schema',)),
        'declared_roles': ('ref-map', '0..n', ('schema',)),
        'runtime_profile': ('ref', '0..1', ('runtime-profile',)),
        'payload': ('payload', '1', ()),
    },
    'runtime-profile': {
        'profile_id': ('string', '1', ()),
        'payload': ('payload', '0..1', ()),
    },
    'binding-set': {
        'bindings': ('ref-map', '0..n', ('artifact', 'data-resource', 'schema')),
        'runtime_profiles': ('ref', '0..n', ('runtime-profile',)),
        'parameters': ('json', '0..1', ()),
    },
    'derivation': {
        'operands': ('ref', '1..n list', ('data-resource', 'excerpt', 'reading')),
        'operand_selectors': ('selector', '0..n list', ()),
        'method': ('string', '1', ()),
        'result': ('json', '1', ()),
    },
    'evidence': {
        'subject': ('ref', '1', ('artifact', 'binding-set', 'data-resource')),
        'subject_selector': ('selector', '0..1', ()),
        'supports': ('ref', '1', ('derivation', 'excerpt', 'reading')),
        'support_selector': ('selector', '0..1', ()),
        'origin': ('string', '1', ()),
    },
    'receipt': {
        'producer': ('string', '1', ()),
        'subjects': ('ref', '0..n', ('artifact', 'binding-set', 'data-resource', 'excerpt', 'knowledge-unit', 'reading', 'runtime-profile', 'schema', 'source')),
        'payload': ('payload', '1', ()),
    },
    'check': {
        'subject': ('ref', '1', ('artifact', 'binding-set', 'data-resource', 'evidence', 'excerpt', 'knowledge-unit', 'reading', 'relation-set', 'runtime-profile', 'schema', 'source')),
        'check_profile': ('string', '1', ()),
        'outcome': ('string', '1', ()),
        'scope': ('json', '1', ()),
        'receipts': ('ref', '0..n', ('receipt',)),
        'origin': ('string', '1', ()),
    },
    'retraction': {
        'target': ('ref', '1', ('check', 'evidence')),
        'reason': ('string', '1', ()),
        'origin': ('string', '1', ()),
    },
    'supersession': {
        'target': ('ref', '1', ('check', 'evidence')),
        'replacement': ('ref', '1', ('check', 'evidence')),
        'reason': ('string', '1', ()),
        'origin': ('string', '1', ()),
    },
    'relation-set': {
        'members': ('ref', '0..n', ('check', 'derivation', 'evidence', 'retraction', 'supersession')),
    },
    'overlay': {
        'subject': ('ref', '1', ('artifact', 'binding-set', 'data-resource', 'excerpt', 'knowledge-unit', 'reading', 'runtime-profile', 'schema', 'source')),
        'origin': ('string', '1', ()),
        'precedence': ('string', '0..1', ()),
        'values': ('json', '1', ()),
    },
    'overlay-set': {
        'members': ('ref', '0..n', ('overlay',)),
    },
    'knowledge-unit': {
        'binding_set': ('ref', '1', ('binding-set',)),
        'narratives': ('ref', '0..n', ('artifact',)),
        'relation_sets': ('ref', '0..n', ('relation-set',)),
        'overlay_sets': ('ref', '0..n', ('overlay-set',)),
    },
    'snapshot': {
        'collection': ('string', '1', ()),
        'units': ('ref', '0..n', ('knowledge-unit',)),
        'schemas': ('ref', '0..n', ('schema',)),
        'relation_sets': ('ref', '0..n', ('relation-set',)),
        'overlay_sets': ('ref', '0..n', ('overlay-set',)),
        'observations': ('ref', '0..n', ('observation',)),
        'native_mappings': ('ref', '0..n', ('native-mapping',)),
    },
    'observation': {
        'subjects': ('ref', '1..n', ('artifact', 'binding-set', 'check', 'data-resource', 'derivation', 'evidence', 'excerpt', 'knowledge-unit', 'observation', 'overlay', 'reading', 'receipt', 'retraction', 'runtime-profile', 'schema', 'source', 'supersession')),
        'observation_type': ('string', '1', ()),
        'scope': ('json', '1', ()),
        'values': ('json', '1', ()),
    },
    'native-mapping': {
        'subject': ('ref', '1', ('artifact', 'binding-set', 'check', 'data-resource', 'derivation', 'evidence', 'excerpt', 'knowledge-unit', 'observation', 'overlay', 'reading', 'receipt', 'retraction', 'runtime-profile', 'schema', 'source', 'supersession')),
        'native_scheme': ('string', '1', ()),
        'native_id': ('string', '1', ()),
        'checked': ('json', '1', ()),
    },
    'execution-lock': {
        'binding_set': ('ref', '1', ('binding-set',)),
        'context_inputs': ('ref', '0..n', ('artifact', 'binding-set', 'check', 'data-resource', 'derivation', 'evidence', 'excerpt', 'knowledge-unit', 'observation', 'overlay', 'overlay-set', 'reading', 'receipt', 'relation-set', 'retraction', 'runtime-profile', 'schema', 'source', 'supersession')),
        'requires': ('string', '0..n', ()),
        'consumer': ('string', '1', ()),
        'completeness': ('string', '1', ()),
    },
    'build-lock': {
        'inputs': ('ref', '1..n', ('artifact', 'binding-set', 'check', 'data-resource', 'derivation', 'evidence', 'excerpt', 'knowledge-unit', 'observation', 'overlay', 'overlay-set', 'reading', 'receipt', 'relation-set', 'retraction', 'runtime-profile', 'schema', 'snapshot', 'source', 'supersession')),
        'requires': ('string', '0..n', ()),
        'consumer': ('string', '1', ()),
        'completeness': ('string', '1', ()),
    },
    'activation': {  # minor version 1 (KND-REG-5, PUB-ACT-1)
        'candidate': ('string', '1', ()),
        'base': ('ref', '0..1', ('snapshot',)),
        'lock': ('ref', '1', ('build-lock', 'execution-lock')),
        'approval': ('ref', '1', ('receipt',)),
        'report': ('ref', '1', ('receipt',)),
        'partition': ('string', '1', ()),
        'effective_from': ('string', '1', ()),
        'effective_until': ('string', '0..1', ()),
        'request_key': ('string', '1', ()),
        'producer': ('string', '1', ()),
        'runner': ('string', '1', ()),
        'approver': ('string', '1', ()),
        'removed': ('ref', '0..n', ('knowledge-unit', 'native-mapping', 'observation', 'overlay-set', 'relation-set', 'schema')),
    },
}


def schema(kind: str) -> dict[str, tuple[str, str, tuple[str, ...]]]:
    """Return the fields of a kind, with the common fields (KND-SCH-1)."""
    return {**COMMON, **KINDS[kind]}
