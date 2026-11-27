# Network read binding

State: draft. Prefix: `NET`. Profile: `aavanam-read/v1`.

## Scope

This document defines a network binding for the operations that do not write content: the transport, the operations in the binding, the limits, the version rule and the compatibility statement. The binding has the identity `aavanam-read/v1`. Its clauses are not part of the base profile.

This document adds no operation, no status and no reason. Each result is the result envelope of the base profile (STS-ENV-1). A store with no network service does not declare this binding. Access control is not in the scope of this specification. A deployment puts its access control in front of the service.

## Topics

| Topic | Subject |
|---|---|
| `TRN` | The transport, the request and the response |
| `SCP` | The operations in the binding |
| `LIM` | The size limit and the deadline |
| `VER` | The version rule |
| `CMP` | The compatibility statement |

## Clauses

### Transport

**NET-TRN-1.** The transport is HTTP/1.1 over TCP. A client calls an operation with the method `POST` and the path `/v1/op/<operation>`, where `<operation>` is the operation name of the operation table (document 03). The body of the request is the request of the operation, as one JSON text in UTF-8.

**NET-TRN-2.** The server MUST give the bytes of the body to the checks of the request with no earlier decode. It MUST NOT use a decoder that repairs the text, for example one that replaces invalid UTF-8 or a lone surrogate, or one that accepts a duplicate key. Thus a body that is not correct JSON gives the envelope of the operation with status `failed` and its reason of the reason table, for example `malformed_json` or `duplicate_key` (STS-MAL-1).

**NET-TRN-3.** The response to a call of NET-TRN-1 has the HTTP status 200 and the header `Content-Type: application/json`. Its body is the result envelope of the operation (STS-ENV-1), as one JSON text in UTF-8. Each status of the status table is in the envelope field `status`. The server MUST NOT use a different HTTP status for an operation result, also for a result that is not `ok`. A client MUST read the envelope field `status` to know the outcome.

**NET-TRN-4.** The HTTP statuses other than 200 are for a request that is not a call of an operation in the binding. Such a response has no envelope. The method `GET` on the path `/v1/about` gives the description of NET-VER-1. A different path, or a different method on these paths, gives the HTTP status 404 or 405.

**NET-TRN-5.** The listener of the binding uses the loopback address unless the configuration of the service gives a different address. A service that also accepts write operations MUST use a different listener for them. That listener is not a part of this binding.

### Operations

**NET-SCP-1.** The binding has these operations, which do not write or remove content: `open_context`, `close_context`, `get`, `query_data`, `search`, `context_bundle`, `evidence`, `coverage`, `observations`, `render`, `compare`, `impact`, `list_items`, `read`, `search_text`, `snapshot_changes`, `item_history`, `publications` and `export`. `open_context` adds a read context to the store, and `close_context` ends one (CTX-LIF-3). A read context is not content (CTX-OPN-2). The binding has `close_context` from `api_minor` 1 (NET-VER-2). Each operation gives the same status, reasons and items as a direct call of the store with the same request.

**NET-SCP-2.** If `<operation>` has no row in the operation table, the response has the HTTP status 200 and the envelope of STS-MAL-4, with status `unsupported` and reason `unsupported_operation`. If its row gives only `unsupported`, the envelope has reason `reserved_profile`. The server does not check the body in these two cases.

**NET-SCP-3.** If `<operation>` has a row in the operation table with more than `unsupported`, and it is not an operation of NET-SCP-1, the response has the HTTP status 404 and no envelope. These are the operations that write or remove content, for example `put`, `import`, `publish`, `execution_inputs`, `remove` and `gc_collect`, the operations of the producer section (document 19) and `removals`. The server MUST NOT run them on the listener of the binding.

### Limits

**NET-LIM-1.** The server declares a limit for the size of a request body, in bytes. If a body is larger than the limit, the response has the HTTP status 413 and no envelope. The server does not run the operation.

**NET-LIM-2.** The server declares a deadline for one call, in milliseconds. If an operation does not complete before the deadline, the response has the HTTP status 200 and an envelope with status `failed` and reason `deadline_exceeded`, with no items (STS-ENV-3 rule 1). The result MUST NOT have a part of the items.

### Version rule

**NET-VER-1.** The identity of the API is `aavanam-read/v1`. `GET /v1/about` gives the HTTP status 200 and a JSON object with these fields:

| Field | Value |
|---|---|
| `api` | `aavanam-read/v1` |
| `api_minor` | The minor version of the API, an integer. The first version is 0 |
| `profile` | `aavanam/base/v1` |
| `profile_minor` | The minor version of the base profile that the server implements (`SPEC.md`), an integer |
| `operations` | The operation names of NET-SCP-1 that the server has, in ascending string order |
| `max_body_bytes` | The limit of NET-LIM-1, an integer |
| `deadline_ms` | The deadline of NET-LIM-2, an integer |

**NET-VER-2.** A compatible change increments `api_minor`. The compatible changes are a new operation in NET-SCP-1, a new field of the description, a new field of an envelope or of an item, and a new reason code with a status of the status table. A compatible change does not change the path prefix `/v1`.

**NET-VER-3.** Each other change is incompatible, for example the removal or the new name of an operation or of a field, a different type of a field, or a different status or meaning of a result. An incompatible change MUST get a new identity and a new path prefix, for example `aavanam-read/v2` and `/v2`. A server MUST NOT make an incompatible change under `/v1`.

### Compatibility

**NET-CMP-1.** A client of `aavanam-read/v1` MUST ignore a field of the description, of an envelope or of an item that it does not know. It MUST treat a result with a reason code that it does not know by the status of the result. Only `ok` is success (STS-SET-2). Thus a client of minor version N can use a server of each later minor version of `/v1`.

**NET-CMP-2.** A client that needs an operation or a field of a later minor version reads `api_minor` and `operations` first. If the server does not have the operation, the call gives the envelope of NET-SCP-2. The client MUST NOT treat this result as an empty result.

**NET-CMP-3.** After a server starts `/v2`, it MUST keep `/v1` for one or more minor releases of the service. The release notes give the last release that has `/v1`.
