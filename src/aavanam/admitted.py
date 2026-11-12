"""Application-side projection of admitted facts (ADP-FACT-1 to ADP-FACT-5, ADP-FXT-1 to ADP-FXT-7)."""
from __future__ import annotations

import base64
import datetime
import re

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


EXPORT_PROFILE = 'aavanam/admitted-facts-export/v1'
PASSAGE = 'aavanam/admitted-facts-export/passage'
EXPORT_COLUMNS = ['field', 'key', 'member', 'value', 'unit', 'kind']
DECLARATION_FIELDS = {'format', 'identity_member', 'span_unit', 'source_digest', 'text_digest'}
FACT_FIELDS = {'field', 'key', 'value', 'unit', 'citation'}
CITATION_FIELDS = {'source', 'ref', 'url', 'source_sha256', 'text_sha256', 'span', 'receipt_sha256'}
DIGEST_FORMS = {'aavanam/sha256/v1': set(), 'aavanam/prefixed-sha256/v1': {'prefix'},
                'aavanam/prefixed-jcs-sha256/v1': {'prefix', 'member'}}


def _name(value) -> bool:
    return isinstance(value, str) and bool(value) and valid_string(value)


def _scalar(value) -> bool:
    return isinstance(value, (str, bool)) or (isinstance(value, (int, float)) and value == value)


def _date(value) -> bool:
    if not isinstance(value, str) or not re.fullmatch(r'\d{4}-\d{2}-\d{2}', value):
        return False
    try:
        datetime.date.fromisoformat(value)
    except ValueError:
        return False
    return True


def _producer_digest(form, data: bytes, text: str, *, text_form: bool) -> str:
    """Compute one declared producer digest (ADP-FXT-3). Refuse an unknown form."""
    if not isinstance(form, dict) or form.get('form') not in DIGEST_FORMS:
        raise ValueError('the digest form is not a form of ADP-FXT-3')
    if set(form) != {'form', 'scheme'} | DIGEST_FORMS[form['form']] or not _name(form['scheme']):
        raise ValueError('the digest form has different fields')
    if form['form'] == 'aavanam/sha256/v1':
        return digest(data)
    if not all(isinstance(form[f], str) and valid_string(form[f]) for f in DIGEST_FORMS[form['form']]):
        raise ValueError('the digest form fields must be strings')
    prefix = form['prefix'].encode('utf-8')
    if form['form'] == 'aavanam/prefixed-sha256/v1':
        return digest(prefix + data)
    if not text_form:
        raise ValueError('the JCS digest form applies only to the text digest')
    return digest(prefix + canonical({form['member']: text}).encode('utf-8'))


def _span(span, reading: bytes, text: str, unit: str) -> tuple[int, int, int, int]:
    """Return the original offsets and the reading byte range of a span (ADP-FXT-5)."""
    if not isinstance(span, dict) or set(span) != {'start', 'end'}:
        raise ValueError('a span needs a start and an end')
    start, end = span['start'], span['end']
    if not all(isinstance(n, int) and not isinstance(n, bool) for n in (start, end)) or not 0 <= start < end:
        raise ValueError('the span is empty or not two integers')
    if unit == 'code-points':
        if end > len(text):
            raise ValueError('the span is outside the reading')
        byte_start = len(text[:start].encode('utf-8'))
        return start, end, byte_start, byte_start + len(text[start:end].encode('utf-8'))
    if end > len(reading):
        raise ValueError('the span is outside the reading')
    try:
        reading[:start].decode('utf-8'), reading[start:end].decode('utf-8')
    except UnicodeDecodeError as error:
        raise ValueError('a span offset is not on a character boundary') from error
    return start, end, start, end


def _fact(fact, declaration, reading: bytes, text: str, digests: tuple[str, str]) -> dict:
    """Check one fact and its citation (ADP-FXT-4, ADP-FXT-5). Return its checked parts."""
    if not isinstance(fact, dict) or not FACT_FIELDS <= set(fact) <= FACT_FIELDS | {'kind'}:
        raise ValueError('a fact has different members')
    kind = fact.get('kind', 'scalar')
    field, key, value, unit = fact['field'], fact['key'], fact['value'], fact['unit']
    if not _name(field) or not (key is None or _name(key)) or not (unit is None or _name(unit)):
        raise ValueError('a fact needs a field name, and its key and unit are null or a name')
    forms = {'scalar': _scalar, 'date': _date, 'passage': lambda v: isinstance(v, str) and bool(v),
             'record': lambda v: isinstance(v, dict) and bool(v) and all(_name(m) and _scalar(x) for m, x in v.items())}
    if kind not in forms or not forms[kind](value):
        raise ValueError('a fact has an unknown kind or a value of a different form')
    if unit is not None and kind != 'scalar':
        raise ValueError('only a scalar fact has a unit')
    citation = fact['citation']
    if not isinstance(citation, dict) or not CITATION_FIELDS <= set(citation) <= CITATION_FIELDS | {'locator'}:
        raise ValueError('a fact has no citation or a citation has different members')
    if citation['source'] != 'document' or citation['ref'] is not None:
        raise ValueError('a citation needs a document source and no reference')
    url = citation['url']
    if not (url is None or _name(url)):
        raise ValueError('a citation url is null or a string')
    if (citation['source_sha256'], citation['text_sha256']) != digests:
        raise ValueError('the source or text digest differs from the supplied bytes')
    if not is_digest(citation['receipt_sha256']):
        raise ValueError('a citation needs a receipt digest')
    locator = citation.get('locator')
    if 'locator' in citation and (kind != 'passage' or not is_selector(locator)):
        raise ValueError('only a passage has a locator, and a locator is a selector')
    start, end, byte_start, byte_end = _span(citation['span'], reading, text, declaration['span_unit'])
    return {'kind': kind, 'field': field, 'key': key, 'value': value, 'unit': unit, 'url': url, 'locator': locator,
            'receipt': citation['receipt_sha256'], 'offsets': (start, end), 'bytes': (byte_start, byte_end)}


def put_export_facts(store: Store, export_json: str, declaration: dict, *, source: bytes, reading: bytes,
                     source_media_type: str, source_locator: str, producer: str, native_scheme: str,
                     serving: str, evidence: str) -> tuple[str, str] | None:
    """Read one producer export of one document and reading pair (ADP-FXT-1 to ADP-FXT-7).

    The declaration is data: it names the export version, the span unit and the digest forms.
    Return the serving and evidence roots, or None for an export with no facts.
    """
    if not isinstance(declaration, dict) or set(declaration) != DECLARATION_FIELDS:
        raise ValueError('the reader declaration has different fields')
    if not _name(declaration['format']) or declaration['span_unit'] not in ('code-points', 'utf8-bytes'):
        raise ValueError('the reader declaration needs a format and a span unit')
    identity = declaration['identity_member']
    if not (identity is None or (_name(identity) and identity not in ('format', 'facts'))):
        raise ValueError('the identity member is null or a different member name')
    try:
        export = parse(export_json)
    except FormError as error:
        raise ValueError('the export must be I-JSON') from error
    members = {'format', 'facts'} | ({identity} if identity else set())
    if not isinstance(export, dict) or set(export) != members or export['format'] != declaration['format']:
        raise ValueError('the export has a different format or different members')
    if identity and not _name(export[identity]):
        raise ValueError('the export identity is not a string')
    if not isinstance(export['facts'], list):
        raise ValueError('the export facts must be a list')
    labels = (serving, evidence, source_media_type, source_locator, producer, native_scheme)
    if not all(_name(label) for label in labels):
        raise ValueError('the collections, media type, locator, producer and native scheme need names')
    if store.section_of(serving) != 'serving' or store.section_of(evidence) != 'evidence':
        raise ValueError('declare serving and evidence collections before the import')

    # ADP-FACT-2 and ADP-FXT-3: check the complete pair before sending any records to the store.
    text = decode_nfc(reading)
    source_form, text_form = declaration['source_digest'], declaration['text_digest']
    if isinstance(source_form, dict) and isinstance(text_form, dict) and source_form.get('scheme') == text_form.get('scheme'):
        raise ValueError('the source and text digest forms need different schemes (ADP-FXT-3)')
    digests = (_producer_digest(source_form, source, text, text_form=False),
               _producer_digest(text_form, reading, text, text_form=True))
    facts = [_fact(fact, declaration, reading, text, digests) for fact in export['facts']]
    if not facts:
        return None

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
    # ADP-FXT-3: keep each producer digest as a native identity, never as a store digest.
    mappings = {add('native-mapping', subject=source_ref, native_scheme=source_form['scheme'],
                    native_id=digests[0], checked=source_form),
                add('native-mapping', subject=reading_ref, native_scheme=text_form['scheme'],
                    native_id=digests[1], checked=text_form)}
    schema = add('schema', schema_profile=EXPORT_PROFILE,
                 payload=payload(canonical({'columns': EXPORT_COLUMNS}).encode('utf-8'), 'application/json'))
    span_profile = NFC_TEXT if declaration['span_unit'] == 'code-points' else 'aavanam/bytes/v1'
    subjects, locators, records = set(), set(), set()
    for fact in facts:
        # ADP-FXT-6: a passage is a text artifact. Each other kind gives table rows with no value conversion.
        field, key, kind, value = fact['field'], fact['key'], fact['kind'], fact['value']
        if kind == 'passage':
            subject = add('artifact', artifact_type=PASSAGE, payload=payload(value.encode('utf-8'), 'text/plain'))
        else:
            if kind == 'record':
                rows = [[field, key, m, value[m], None, kind] for m in sorted(value, key=lambda m: m.encode('utf-16-be'))]
            else:
                rows = [[field, key, None, value, fact['unit'], kind]]
            table = {'columns': EXPORT_COLUMNS, 'key': ['field', 'key', 'member'], 'rows': rows}
            subject = add('data-resource', schema=schema, data_profile='aavanam/table/v1',
                          payload=payload(canonical(table).encode('utf-8'), 'application/json'))
        subjects.add(subject)
        locators.add(add('overlay', subject=subject, origin=producer, values={'source_locator': source_locator}))

        # ADP-FXT-7: the excerpt uses exact reading bytes. ext keeps the original coordinates.
        byte_start, byte_end = fact['bytes']
        excerpt = add('excerpt', parent=reading_ref, ranges=[{'start': byte_start, 'end': byte_end}],
                      payload=payload(reading[byte_start:byte_end], 'text/plain'))
        start, end = fact['offsets']
        ext = {'citation': {'selector_profile': span_profile, 'start': start, 'end': end},
               'receipt_digest': fact['receipt']}
        if fact['url'] is not None:
            ext['url'] = fact['url']
        if fact['locator'] is not None:
            ext['locator'] = fact['locator']
        if identity:
            ext['export_identity'] = export[identity]
        record = add('evidence', subject=subject, supports=excerpt, origin=producer, ext=ext)
        records.add(record)
        native_id = canonical([*digests, fact['receipt'], start, end, field, key, kind, value, fact['unit'],
                               fact['url'], fact['locator']])
        mappings.add(add('native-mapping', subject=record, native_scheme=native_scheme, native_id=native_id, checked={}))

    # ADP-FACT-5: the serving closure has no link to the citation records.
    bindings = add('binding-set', bindings={f'fact/{n}': ref for n, ref in enumerate(sorted(subjects))})
    unit = add('knowledge-unit', binding_set=bindings)
    overlay_set = add('overlay-set', members=sorted(locators))
    relations = add('relation-set', members=sorted(records))
    serving_root = add('snapshot', collection=serving, units=[unit], overlay_sets=[overlay_set])
    evidence_root = add('snapshot', collection=evidence, relation_sets=[relations], native_mappings=sorted(mappings))
    result = store.call('put', {'descriptors': list(descriptors.values()), 'payloads': payloads})
    if result['status'] != 'ok':
        raise ValueError(f"the store refused the export facts: {result['reasons']}")
    return serving_root, evidence_root
