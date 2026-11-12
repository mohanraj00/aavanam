# Content reads and text search

State: draft. Prefix: `CRD`. Profile: `aavanam/base/v1`.

## Scope

This document defines the read contract. A content read gives the descriptor and the bytes of one member of a read context, with an optional selector. A text search gives the locations of a word sequence in the text artifacts of a read context. A consumer reads the bytes of a location with a content read. Semantic search stays reserved (RSV-RSP-2).

The rules of this document use only the members and the roots of the read context, the descriptors and the payload bytes. They do not use a section, a catalog record or the backend of the store. Thus a writer store and a device store with the same members give the same results (BUN-CLS-3).

## Topics

| Topic | Subject |
|---|---|
| `READ` | The content read of one member |
| `SEL` | The selector forms of a read |
| `TXT` | The text search, its candidates and its match rules |
| `ORD` | The order and the pages of a text search |
| `SRV` | Serving contexts and Evidence material |
| `LOC` | Search items are locations, not values |

## Clauses

### Content read

**CRD-READ-1.** The request of `read` has the fields `context` (CTX-OPN-3) and `ref`, a reference. It can also have the field `selector`, a selector (REL-SEL-1). The store checks the request in this order, and stops at the first check that fails. In each case, the result has no items (STS-ENV-3 rule 1).

1. If the request has a field that is not in this clause, the result has status `failed` and reason `unknown_field`.
2. If the request does not have `context` or `ref`, the result has status `failed` and reason `field_count`.
3. If a value does not agree with its type, the result has status `failed` and reason `field_type`. If `selector` has a profile of the table of CRD-SEL-1, and its fields do not agree with that profile, the reason is also `field_type`.
4. If the read context expired, the result has status `failed` and reason `context_expired`.

**CRD-READ-2.** If the request passes the checks of CRD-READ-1, the result has one item. The store checks the item in these steps, in this order. It stops at the first check that fails, and the item has the reason of that check only. The `refs` of the reason name `ref`. STS-ENV-3 gives the envelope status.

1. Reference: QRY-GET-2 gives `unknown_ref` or `not_in_context`.
2. Selector: CRD-SEL-2 gives `unsupported_selector`.
3. Profile: CRD-SEL-5 gives `unsupported_profile`.
4. Payload bytes: if the descriptor has a `payload` field, and its bytes are not in the store, the item has status `not_found` and reason `payload_missing`.
5. Payload form: CRD-SEL-4 and CRD-SEL-5 give `payload_form`.
6. Range: CRD-SEL-3, CRD-SEL-4 and CRD-SEL-5 give `selector_range`.

**CRD-READ-3.** If no check of CRD-READ-2 fails, the item has status `ok` and the fields `ref`, `kind`, `descriptor`, `selector`, `data` and `row`. An item that is not `ok` has no other field.

1. `kind` is the kind of the member.
2. `descriptor` is the descriptor of the member, as a JSON object. Its fields and values are the fields and values of the canonical form (IDN-CAN-1).
3. `selector` is the `selector` of the request, or null if the request has none.
4. `data` is the base64 text (RFC 4648 section 4) of the selected bytes. Without a selector, the selected bytes are all the payload bytes. If the request has no selector and the descriptor has no `payload` field, `data` is null. If the selector has the profile `aavanam/row/v1`, `data` is null.
5. `row` is the selected row of CRD-SEL-5, or null for each other read.

**CRD-READ-4.** `read` does not need a capability (CTX-PRJ-1). A projection does not hide a member (CTX-PRJ-5). `read` reads only the member that the request names. It does not read a reference of the member, and it gives no item for one. A consumer reads a referenced object with a second `read`.

**CRD-READ-5.** `read` gives exact bytes. It does not decode, parse, convert or normalize the bytes of an `artifact`, `runtime-profile` or `receipt` payload (KND-OPQ-2). It reads the payload of a kind of KND-OPQ-1 only as CRD-SEL-4 and CRD-SEL-5 give. `read` does not change the store.

### Selector forms

**CRD-SEL-1.** `read` supports the selector profiles in this table, and no other profile:

| Profile | Fields | Target |
|---|---|---|
| `aavanam/bytes/v1` | `selector_profile`, `start`, `end` (REL-SEL-3) | A member with a `payload` field |
| `aavanam/nfc-text/v1` | `selector_profile`, `start`, `end` (ADP-SEL-4) | A `reading` |
| `aavanam/row/v1` | `selector_profile`, `index` | A `data-resource` |

For `aavanam/row/v1`, `index` is an integer of 0 or more. A selector of this profile with a different field, a missing field or a different value does not agree with its type (CRD-READ-1 step 3).

**CRD-SEL-2.** If the profile of `selector` is not in the table of CRD-SEL-1, or the member is not a target of the profile, the item has status `unsupported` and reason `unsupported_selector` (REL-SEL-2). The store MUST NOT apply a different selector, and MUST NOT give the whole member in its place. The profiles of REL-SEL-5 are not in the table.

**CRD-SEL-3.** A selector of profile `aavanam/bytes/v1` selects the payload bytes from offset `start` up to offset `end`, without the byte at `end` (REL-SEL-3). If `end` is more than the payload size, the item has status `failed` and reason `selector_range` (REL-SEL-4). The store does not check the form of the payload.

**CRD-SEL-4.** A selector of profile `aavanam/nfc-text/v1` selects the UTF-8 bytes of the code points from `start` up to `end`, without the code point at `end`. ADP-SEL-4 gives `payload_form` for a payload that is not valid UTF-8 in NFC, and `selector_range` for a range that ends after the last code point.

**CRD-SEL-5.** A selector of profile `aavanam/row/v1` selects one row of a table. If the store does not support the `data_profile` of the member, DAT-PRF-2 gives `unsupported_profile`. Under `aavanam/table/v1`, DAT-TBL-3 gives `payload_form`. If `index` is the number of rows or more, the item has status `failed` and reason `selector_range`. If not, `row` is the row at the position `index` in the table order (DAT-TBL-2), as an object in the form of QRY-DAT-3 step 2.

### Text search

**CRD-TXT-1.** The request of `search_text` has the fields `context` (CTX-OPN-3) and `query`, a string of 1 to 1024 Unicode scalar values. It can also have these fields:

| Field | Value |
|---|---|
| `offset` | An integer of 0 or more. The default is 0 |
| `limit` | An integer of 1 or more. If it is absent, the request has no bound |

The store checks the request in this order, and stops at the first check that fails. In each case, the result has no items (STS-ENV-3 rule 1).

1. If the request has a field that is not in this clause, the result has status `failed` and reason `unknown_field`.
2. If the request does not have `context` or `query`, the result has status `failed` and reason `field_count`.
3. If a value does not agree with its type, the result has status `failed` and reason `field_type`. A string with a surrogate code point does not agree with the type of `query`. If the query has no word (CRD-TXT-3), or more than 32 words, the reason is also `field_type`.
4. If the read context expired, the result has status `failed` and reason `context_expired`.
5. If the context does not have the capability `aavanam/text/v1`, CTX-PRJ-4 gives `capability_unavailable`.

**CRD-TXT-2.** The candidates are the members of the read context of kind `artifact` that have a text media type. The store reads the media type from the field `media_type` of the `payload` of the descriptor. It does not read a media type from the payload bytes. The store does these steps on the media type:

1. It keeps the part before the first `;`, or all of the value if it has no `;`.
2. It removes each ASCII space (byte 32) at the start and at the end of this part.
3. It changes each byte of `A` to `Z` into the byte of `a` to `z`.

The media type is a text media type if the result starts with `text/` and has one or more bytes after `text/`. The store does not use a `charset` parameter, and it does not decode the payload. Thus it scans a payload with `charset=utf-16` as bytes. A member of a different kind is not a candidate, also if its payload is text.

**CRD-TXT-3.** The word rule and the case rule apply to bytes. They do not decode UTF-8.

1. A word byte is a byte of an ASCII letter (`A` to `Z`, `a` to `z`), a byte of an ASCII digit (`0` to `9`), or a byte of value 128 or more. Each other byte is a separator. Byte 0 is a separator.
2. A word is a maximal sequence of word bytes. Its start is the offset of its first byte. Its end is the offset after its last byte.
3. The folded form of a word changes each byte of `A` to `Z` into the byte of `a` to `z`. It changes no other byte. Thus the case rule is ASCII only.
4. The words of the query are the words of the UTF-8 bytes of `query`, in their order. The separators of the query have no effect. Thus the query `a-b`, the query `a b` and the query `A OR b` have the words `a b`, `a b` and `a or b`.

The SQLite FTS5 `ascii` tokenizer uses the same word bytes, but it can cut a long word. A backend can use an FTS5 index to find the candidates. It MUST NOT use the query syntax of FTS5 on the request text, and it MUST get each word and each offset from the payload bytes.

**CRD-TXT-4.** A match is a word of a candidate payload where the folded forms of this word and of the next N - 1 words are equal to the folded forms of the N words of the query, in the same order. The separators between the words do not change a match. Two matches can overlap. For example, the query `a a` has two matches in the payload `a a a`, at offsets 0 and 2. The locator of a match is a selector of profile `aavanam/bytes/v1`. Its `start` is the start of the first matched word, and its `end` is the end of the last matched word. Thus a `read` with the locator gives the matched bytes (CRD-SEL-3).

**CRD-TXT-5.** If the payload bytes of a candidate are not in the store, the candidate gives one item with status `not_found` and reason `payload_missing`. The `refs` of the reason name the candidate. The store does not skip the candidate.

**CRD-TXT-6.** Each match gives one item with status `ok` and the fields `ref`, `snapshots` and `selector`.

1. `ref` is the candidate.
2. `snapshots` is the list of each root of the read context of kind `snapshot` whose closure has the candidate (KND-DEP-3), in ascending string order and with no duplicate. The list is empty if no snapshot root has the candidate, for example in a context with only a knowledge-unit root.
3. `selector` is the locator of CRD-TXT-4.

An item of CRD-TXT-5 has no field other than `status` and `reasons`.

**CRD-TXT-7.** A word comparison is a byte comparison. It is not a parse, an interpretation or a decode of the payload in the sense of KND-OPQ-1 and KND-OPQ-2. The store does not use the format of an `artifact` payload, for example its markup or its syntax. `search_text` does not change the store.

### Order and pages

**CRD-ORD-1.** The items are in ascending string order of `ref`. In one `ref`, the `ok` items are in ascending order of the `start` of their locators. The item of CRD-TXT-5 has no match, so it is the only item of its `ref`. The order does not depend on the backend, on the order of the roots or on the time of the call.

**CRD-ORD-2.** The items start at the position `offset` in this list. If the request has `limit`, the result has `limit` items or fewer. If more items occur after `offset` than `limit`, a declared bound stopped the operation. STS-ENV-3 gives the envelope status. Thus a page that has no `ok` item and a next item has status `failed` and reason `incomplete_result`, and no items (STS-ENV-3 rule 3). To read the next items, a consumer sends the same request with `offset` plus `limit`. The context does not change between the two reads (CTX-OPN-2).

**CRD-ORD-3.** The items of a search depend only on the members and the roots of the read context, the payload bytes and the request. Thus a device context and a writer context under the conditions of BUN-CLS-3 give the same items for the same request. These conditions are the same roots, the same capabilities and an `ok` import of the bundle. A rebuild of an index from the same snapshot does not change the items.

### Serving contexts and Evidence material

**CRD-SRV-1.** Evidence material is a descriptor of kind `source`, `reading`, `excerpt` or `evidence`, and the payload bytes that a payload field of such a descriptor names. The store identifies Evidence material by the kind of the descriptor. It does not compare the bytes of a different member with Evidence material.

**CRD-SRV-2.** A Serving context is a read context on a writer store in which each root is a snapshot whose `collection` has the section `serving` (PUB-SEC-1). By PUB-SEC-3, no member of a Serving context is Evidence material. `read` gives only a member (CRD-READ-2 step 1). Thus in a Serving context, `read` of a reference to Evidence material gives `not_in_context` if the store has it, or `unknown_ref` if not. The store MUST NOT give its descriptor or its bytes.

**CRD-SRV-3.** A device store has no sections (PUB-SEC-1). A device context with the roots of a Serving context has the same members (BUN-CLS-3). Thus it has no member that is Evidence material, and `read` in it gives the outcomes of CRD-SRV-2. This rule uses only the members, so the device store does not need the section of a collection.

**CRD-SRV-4.** A read context with a root in an `evidence` collection is not a Serving context. In such a context, `read` gives Evidence material that is a member (PUB-SEC-5).

**CRD-SRV-5.** CRD-SRV-2 does not apply to bytes that a member of a different kind names. For example, an `artifact` can have the same payload digest as a `reading`. Then a read of the artifact gives these bytes as the bytes of the artifact (KND-OPQ-2), and `search_text` gives matches in them. The producer decides which content goes into a `serving` collection. The store does not inspect the content of a payload or of a JSON value, for example an `ext` value or an overlay value (PUB-SEC-4).

**CRD-SRV-6.** `search_text` MUST NOT give an item for Evidence material in any read context, because no kind of CRD-SRV-1 is a candidate (CRD-TXT-2). Thus a word that occurs only in Evidence material gives no item, also in a context with an `evidence` root. A search item has no payload bytes, and its `snapshots` names only roots of the context.

### Locations, not values

**CRD-LOC-1.** A search item is a location, not a value. It has no payload bytes. A consumer MUST read a value with `read` or `query_data`. A consumer MUST NOT use a search item as a value, or in place of an exact lookup that is not `ok` (STS-SET-2). A text search is not a semantic search (RSV-RSP-2). An empty item list does not state that the words do not occur outside the candidates (STS-ENV-2). The rules of this clause apply to the consumer. A store vector does not test them.
