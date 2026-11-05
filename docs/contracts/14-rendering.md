# Rendering

State: draft. Prefix: `RND`. Profile: `aavanam/base/v1`.

## Scope

This document defines `render`, a read of a context that gives a rendered document. Rendering is a secondary consumer. The document shows the scope of the read, the check outcomes and the gaps of each subject. It never adds an unqualified verified label.

The rendered document is structured JSON. A presentation, for example HTML or Markdown, is an application output that the application makes from the document. The store does not write prose, and it does not put a payload in the document.

## Topics

| Topic | Subject |
|---|---|
| `DOC` | Rendered documents and cache identity |
| `SCP` | Scope, gap and check display |

## Clauses

### Rendered documents

**RND-DOC-1.** The request of `render` has these fields:

| Field | Value | Count |
|---|---|---|
| `context` | The identity of a read context (CTX-OPN-3) | `1` |
| `roots` | A set of references (KND-SCH-5), the subjects of the document | `1..n` |
| `render_profile` | The name of a render profile, a string. The default is `aavanam/render/v1` | `0..1` |

The store checks the request in this order. In each of these cases, the result has no items (STS-ENV-3 rule 1).

1. If the request has a different field, a field with a count that the table does not permit, or a value of a different type, the status is `failed` with reason `unknown_field`, `field_count` or `field_type`.
2. If `render_profile` is not `aavanam/render/v1`, the status is `unsupported` with reason `unsupported_profile`.

**RND-DOC-2.** If the request is correct, the store renders each root. If a root is not in the store, its subject item (RND-SCP-1) has status `not_found` and reason `unknown_ref`. If a root is in the store but not in the read context, its subject item has status `not_found` and reason `not_in_context`. A root that is not rendered has no other item. STS-ENV-3 gives the envelope status. Thus if one or more roots are rendered and a different root is not, the status is `partial` with reason `items_not_ok` (STS-ENV-3 rule 5).

**RND-DOC-3.** If the status is `ok` or `partial`, the envelope field `document` is a JSON object with these fields, and no other field:

| Field | Value |
|---|---|
| `format` | `aavanam-render/v1` |
| `render_profile` | The render profile of the request |
| `context` | The identity of the read context |
| `roots` | The roots of the request, in ascending string order, one time each |
| `entries` | One object for each `ok` item, with the fields of the item without `status` and `reasons`. The order is the ascending order of the RFC 8785 form of each object in UTF-8 |
| `omitted` | The roots that are not rendered (RND-DOC-2), in ascending string order |

The envelope field `document_id` is `sha256:` and the 64 lowercase hexadecimal digits of the SHA-256 digest of the RFC 8785 form of `document` in UTF-8. With each other status, `document` and `document_id` are null.

**RND-DOC-4.** The content of a read context does not change (CTX-OPN-2). Thus two `render` calls with the same request give the same `document` and the same `document_id`. An application can use `document_id` as the cache identity of a presentation. `render` does not change the store.

**RND-DOC-5.** The order of the items is not specified. A consumer MUST NOT depend on it. The order of `entries` in the document is specified (RND-DOC-3).

### Scope, gaps and checks

**RND-SCP-1.** Each root has one subject item. A subject item has the fields `entry` with the value `subject`, and `ref`, the root. If the root is rendered, the item has status `ok` and also the field `kind`, the kind of the root.

**RND-SCP-2.** For each `check` in the read context whose `subject` is a rendered root, the result has one check item. A check item has status `ok`, and the fields `entry` with the value `check`, `subject`, `ref`, `check_profile`, `outcome`, `scope` and `origin`. `ref` is the check. The other values are the values of the check descriptor. The store copies `outcome` as a string. It does not read or change it (PUB-CND-5).

**RND-SCP-3.** For each observation of type `aavanam/coverage/v1` in the read context whose `values` agree with OBS-COV-1 and whose `subjects` have a rendered root, the result has one coverage item for that root. A coverage item has status `ok`, and the fields `entry` with the value `coverage`, `subject`, `ref`, `unit`, `covered`, `total` and `omissions`, with the values of the observation. OBS-COV-4 applies: the store does not compute a ratio and does not remove an omission.

**RND-SCP-4.** For each rendered root, the result has one gap item for each gap in this table. A gap item has status `ok`, and the fields `entry` with the value `gap`, `subject`, `gap` and `ref`. `ref` is the record of the gap, or null.

| `gap` | The gap applies if | `ref` |
|---|---|---|
| `no_check` | The read context has no check of the subject | null |
| `no_coverage` | The read context has no observation of type `aavanam/coverage/v1` of the subject | null |
| `values_form` | The `values` of a coverage observation of the subject do not agree with OBS-COV-1. This observation has no coverage item | The observation |
| `unknown_total` | A coverage item has a null `total` | The observation |
| `omissions` | A coverage item has one or more omissions | The observation |
| `withdrawn` | A `retraction` or `supersession` in the read context has a check of the subject as its `target` (REL-RET-3). The check item stays | The check |

**RND-SCP-5.** The store MUST NOT add an item, a field or a value that states that a subject is verified, approved, correct or complete. Each value in an item is a value of a descriptor, a reference or a value of a table in this document. Thus a check outcome is shown only with its `check_profile`, `scope` and `origin`. A document with no gap item for a subject does not state that the subject is verified or complete (OBS-COV-4).

**RND-SCP-6.** A presentation of a document MUST show each check outcome with its `check_profile`, `scope` and `origin`, and each gap. It MUST NOT show a verified label without these qualifiers. It MUST show the `omitted` roots. This rule applies to the application that makes the presentation.
