"""Application-side projection of admitted facts (ADP-FACT-1 to ADP-FACT-5)."""
from __future__ import annotations

import base64

from .descriptors import NFC_TEXT, check, decode_nfc, is_selector
from .jsontext import FormError, canonical, digest, is_digest, parse, revision, valid_string
from .kinds import BASE_PROFILE
from .store import COVERAGE, Store

PROFILE = 'aavanam/admitted-facts/v1'
ITEM_FIELDS = {'field', 'value', 'document_digest', 'text_digest', 'span', 'receipt_digest'}


def put_admitted_facts(store: Store, items_json: str, *, source: bytes, reading: bytes, source_media_type: str,
                       source_locator: str, producer: str, native_scheme: str,
                       serving: str, evidence: str) -> tuple[str, str] | None:
    """Write one document and reading pair in a single put. Return its serving and evidence roots.

    Pass a JSON list of admitted items with the fields in ITEM_FIELDS. The caller reads the
    producer version and supplies its exact original and NFC reading bytes. Declare the two
    collection sections before the first call. An empty list returns None without a write.
    """
    try:
        items = parse(items_json)
    except FormError as error:
        raise ValueError('the admitted items must be I-JSON') from error
    if not isinstance(items, list):
        raise ValueError('the admitted items must be a list')
    if not items:
        return None
    labels = (serving, evidence, source_media_type, source_locator, producer, native_scheme)
    if not all(isinstance(label, str) and label and valid_string(label) for label in labels):
        raise ValueError('the collections, media type, locator, producer and native scheme need names')
    if store.section_of(serving) != 'serving' or store.section_of(evidence) != 'evidence':
        raise ValueError('declare serving and evidence collections before the import')

    # ADP-FACT-2: validate the complete pair before sending any records to the store.
    text = decode_nfc(reading)
    source_digest, text_digest = digest(source), digest(reading)
    citations = []
    for item in items:
        if not isinstance(item, dict) or set(item) != ITEM_FIELDS or not isinstance(item['field'], str) or not item['field']:
            raise ValueError('each admitted item needs a field, value, document and text digests, span and receipt digest')
        if item['document_digest'] != source_digest or item['text_digest'] != text_digest:
            raise ValueError('the document or reading digest differs from the admitted item')
        if not is_digest(item['receipt_digest']):
            raise ValueError('the admitted item needs a receipt digest')
        span = item['span']
        if not isinstance(span, dict) or set(span) != {'start', 'end'}:
            raise ValueError('an admitted span needs a start and an end')
        selector = {'selector_profile': NFC_TEXT, **span}
        if not is_selector(selector) or not span['start'] < span['end'] <= len(text):
            raise ValueError('the admitted span is empty or outside the reading')
        citations.append((item, selector))

    descriptors, payloads = {}, {}

    def add(kind, **fields):
        value = check({'descriptor_profile': BASE_PROFILE, 'kind': kind, **fields})
        ref = revision(value)
        descriptors[ref] = canonical(value)
        return ref

    def payload(data, media_type):
        key = digest(data)
        payloads[key] = base64.b64encode(data).decode('ascii')
        return {'digest': key, 'size': len(data), 'media_type': media_type}

    source_ref = add('source', payload=payload(source, source_media_type))
    reading_ref = add('reading', source=source_ref, producer=producer, coverage_profile=COVERAGE,
                      payload=payload(reading, 'text/plain'))
    columns = ['field', 'value']
    schema = add('schema', schema_profile=PROFILE,
                 payload=payload(canonical({'columns': columns}).encode('utf-8'), 'application/json'))
    resources, locators, records, mappings = set(), set(), set(), set()
    for item, selector in citations:
        # ADP-FACT-3: preserve the typed value in the serving table.
        field, value = item['field'], item['value']
        table = {'columns': columns, 'key': ['field'], 'rows': [[field, value]]}
        resource = add('data-resource', schema=schema, data_profile='aavanam/table/v1',
                       payload=payload(canonical(table).encode('utf-8'), 'application/json'))
        resources.add(resource)
        locators.add(add('overlay', subject=resource, origin=producer, values={'source_locator': source_locator}))

        # ADP-FACT-4: only the excerpt uses byte offsets. Keep the original coordinates and receipt identity.
        start, end = selector['start'], selector['end']
        byte_start = len(text[:start].encode('utf-8'))
        excerpt_bytes = text[start:end].encode('utf-8')
        excerpt = add('excerpt', parent=reading_ref, ranges=[{'start': byte_start, 'end': byte_start + len(excerpt_bytes)}],
                      payload=payload(excerpt_bytes, 'text/plain'))
        receipt_digest = item['receipt_digest']
        record = add('evidence', subject=resource, supports=excerpt, origin=producer,
                     ext={'citation': selector, 'receipt_digest': receipt_digest})
        records.add(record)
        identity = canonical([source_digest, text_digest, receipt_digest, start, end, field, value])
        mappings.add(add('native-mapping', subject=record, native_scheme=native_scheme, native_id=identity, checked={}))

    # ADP-FACT-5: the serving closure has no link to the citation records.
    bindings = add('binding-set', bindings={f'fact/{n}': ref for n, ref in enumerate(sorted(resources))})
    unit = add('knowledge-unit', binding_set=bindings)
    overlay_set = add('overlay-set', members=sorted(locators))
    relations = add('relation-set', members=sorted(records))
    serving_root = add('snapshot', collection=serving, units=[unit], overlay_sets=[overlay_set])
    evidence_root = add('snapshot', collection=evidence, relation_sets=[relations], native_mappings=sorted(mappings))
    result = store.call('put', {'descriptors': list(descriptors.values()), 'payloads': payloads})
    if result['status'] != 'ok':
        raise ValueError(f"the store refused the admitted facts: {result['reasons']}")
    return serving_root, evidence_root
