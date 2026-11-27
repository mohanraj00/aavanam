# Browse page

This example is a standalone page that browses a store through its read API. It is read-only. It shows these views:

1. The list of collections, with the head of each collection and the number of its publications.
2. The history of the head of one collection: each publication, with its previous head and its new head.
3. One snapshot: the items of a read context on the snapshot, with their kinds and logical IDs.
4. One item: its content as text, its citation, the history of its logical ID and its descriptor.

The item view can also cite a byte range of the content. It reads the range again with a selector of profile `aavanam/bytes/v1` (CRD-SEL-3), and the citation has that selector.

## Read API version

The page needs the read API `aavanam-read/v1`, minor version 0 or later ([document 22](../../docs/contracts/22-network-read-binding.md)). At start, the page reads `GET /v1/about`. It stops if the API is not `aavanam-read/v1` or if the server does not have one of these operations (NET-CMP-2):

| Operation | Use |
|---|---|
| `publications` | The heads and the history of a head (PUB-LOG-3) |
| `open_context` | A read context on one snapshot (CTX-OPN-1) |
| `list_items` | The items of a snapshot (DSC-LST-3) |
| `read` | The descriptor and the bytes of one item (CRD-READ-3) |
| `item_history` | The history of one logical ID (LIF-HST-2) |

The page ignores a field that it does not know (NET-CMP-1). It shows the status and the reasons of each result that is not `ok`. It does not show such a result as an empty result.

## What the page writes

The page writes no content. Each operation of the table is in NET-SCP-1, so none of them writes content. `open_context` adds a read context to the store, and a read context is not content (CTX-OPN-2). The page opens a new read context for each snapshot view and each item view. On `aavanam-server`, a read context has a life (`--context-life`), and the service removes it after that life. The page has no session and keeps no state.

The read API has no operation that lists collections. Thus the page lists the collections of its configuration (`--collection`).

## Run it from a clean checkout

You need Python 3.14 or later and Go 1.27 or later. Run this command from the root of the checkout:

```bash
python3 -B examples/browse_page/run.py
```

The command does these steps:

1. It builds the backend driver `aavanam-conform` with `go build`. If `AAVANAM_CONFORM_BIN` gives the command of a built driver, it uses that driver.
2. It starts the driver with the storage in memory. The driver has the read API on a port of the loopback address.
3. It writes the fixture of [fixture.json](fixture.json) to the store over the line protocol of the driver. The fixture has two collections. The collection `sample-field-guide` has the section `serving` and two publications. The collection `sample-site-notes` has one publication.
4. It downloads the pinned mugham UI kit and checks its digest (see below).
5. It starts the page on `http://127.0.0.1:8440/`. Use `--port` to change the port.

Stop the command with Ctrl-C. The store is in memory, so it stops with the command.

The fixture is synthetic. Its writes go over the line protocol, not over the read API. The page itself only reads.

## Run it on a different store

Give the origin of a read API and one or more collections, for example for `aavanam-server` with its default read address:

```bash
python3 -B examples/browse_page/browse.py --read-url http://127.0.0.1:8437 --collection sample-field-guide
```

## The UI kit

The page uses only the two style files of the mugham UI kit: `src/tokens/tokens.css` and `src/styles/page.css`. It uses no script of the kit, and the page has no script. The kit is an option. With `--no-kit`, the page has no style and no download.

The page uses the published release artifact of the kit, not a local build:

| Field | Value |
|---|---|
| Version | v0.8.0 |
| URL | `https://github.com/mohanraj00/mugham/releases/download/v0.8.0/mugham.tar` |
| SHA-256 | `0e58a1f3ba7c5895864d6541dc6a3ec271db764ee4ba5e83b67bde9f8a0b0e26` |

The page checks the SHA-256 digest of the archive after the download, before it keeps the archive in the cache. It checks the digest again each time that it reads the archive from the cache. If a digest is different, the page stops. The default cache is `~/.cache/aavanam-browse`. Use `--kit-cache` to change it. The page reads the two style files from the archive in memory. It does not extract the archive to disk.

## Security

Access control is not in the scope of the read API. Thus:

- The page uses the loopback address only.
- The page accepts a request only if its `Host` header names the loopback address and the port of the page. Thus a different site cannot read the page through a DNS name that points to the loopback address.
- The page accepts only `GET`. Each other method gets the HTTP status 405.
- The page shows each value from the store only as escaped text. It shows the content of an item only if the media type is text and the bytes are UTF-8. Its `Content-Security-Policy` permits no script.

## Test

[tests/test_browse_page.py](../../tests/test_browse_page.py) starts the driver with the fixture store and the page. It checks the list of collections, the history of a head, a snapshot view and a content view with its citation. It also checks that the fingerprint of the store does not change. `make check-backend-mem` runs it, and the `backend` job of CI runs that target.
