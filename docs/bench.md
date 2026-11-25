# Budget bench

The budget bench measures the backend and the device store against the size and time budget of [the design](design/backend.md#7-size-and-time-budget), section 7. Issue #105 added it.

Each number in this document and in the result files is for the machine of its run only. It is not a claim for a different machine, a different load or a production deployment.

## Commands

| Command | What it runs |
|---|---|
| `make check-bench` | `S0` on the storage in memory, on PostgreSQL if `AAVANAM_PG_DSN` is set, and on the device store. It checks the budget. CI runs it in the `backend` job |
| `make bench-s1` | `S1` on PostgreSQL, with one client and with 16 concurrent clients. It needs `AAVANAM_PG_DSN`. CI does not run it |
| `cd backend && go run ./bench -help` | The flags of the backend bench |
| `cd reference && go run ./bench -help` | The flags of the device bench |

CAUTION: Give the PostgreSQL run a database for tests only. The bench removes each row of the database of `AAVANAM_PG_DSN` before it makes the store, as the conformance driver does.

Write a result file outside the checkout with `-out`, unless the file is a recorded result in `docs/bench/`.

## Layout

- `backend/bench/synth`: the generator of the synthetic store.
- `backend/bench/measure`: the timer, the percentiles, the machine description, the result file and the budget check.
- `backend/bench/suite`: the read measures. The backend and the device bench use the same requests.
- `backend/bench`: the backend bench, for the storage in memory and for PostgreSQL.
- `reference/bench`: the device bench. It is in the `reference` module, because the device store is in that module and the `backend` module does not import it.

## The synthetic store

The generator makes the store from a shape and a seed. The bytes of each payload come from the seed and the index of the payload only. Thus two runs with one shape and one seed send the same requests in the same order. The result file gives the SHA-256 digest of these requests (`request_digest`). A test makes the small shape two times and compares the store fingerprints.

The data is neutral: pseudo-words of two or three syllables from a fixed set, numbers and random bytes.

Each collection is a `serving` collection with a head. A knowledge unit binds 20 artifacts, and an overlay gives it the metadata key `region`. The main collection, `c000`, has the measured read context:

- 2 000 text artifacts (`text/plain`). The phrase `amber harbor` is in each 100th text artifact. The syllable set cannot make these two words.
- An artifact of exactly 64 KiB at each 50th artifact.
- The largest table, with the columns `key`, `value`, `group` and `note`. The column `group` has 100 values, so a `where` filter on one value selects 1% of the rows.
- For `S1`, random bytes (`application/octet-stream`) in the other artifacts of the main context.

The other collections hold the rest of the declared descriptors and bytes, in text artifacts.

| Value | `S0` declared | `S0` made | `S1` declared | `S1` made |
|---|---|---|---|---|
| Descriptors | 20 000 | 20 000 | 200 000 | 200 000 |
| Payload bytes | 200 MiB | 200 MiB | 2 GiB | 2 GiB |
| Collections, each with a head | 10 | 10 | 50 | 50 |
| Members of the main context | 2 000 | 2 304 | 20 000 | 19 991 |
| Text artifacts of the main context | 2 000 | 2 000 | 2 000 | 2 000 |
| Rows and bytes of the largest table | 10 000, 2 MiB | 10 000, 2 MiB | 100 000, 20 MiB | 100 000, 20 MiB |

The main context of `S0` has more members than the design states. Reason: it must have 2 000 text artifacts, and each unit of 20 artifacts adds a binding set, a knowledge unit and an overlay.

## Measures

The read measures call the backend over the network read binding on the loopback address (document 22), with PostgreSQL on the same host. They call the device store in the process, as an application on a device does. Each measure makes 1 000 calls with one client by default, and gives the count, p50, p95 and the maximum. The percentiles use the nearest rank.

| Measure | Request |
|---|---|
| `open_context` | `open_context` with the head of `c000`. The device has no heads, so it uses the snapshot root |
| `read_row` | `read` of one row of the largest table (`aavanam/row/v1`), a different row for each call |
| `read_artifact_64k` | `read` of a 64 KiB text artifact |
| `search_text` | `search_text` for `amber harbor` in the main context |
| `query_data_where` | `query_data` of the largest table with `where` on `group` |
| `publish_1000:<step>` | 40 new units in `c000`, 1 000 new descriptors, then a new head: the `put` calls, `create_snapshot`, `create_candidate` and `publish`, in the process. Five publications |

The device bench also measures the device bundle on `S0`:

- the bundle of the heads of all collections: its size, and the size as a ratio to its payload bytes;
- the time of the first import;
- the time of a bundle update that changes 1% of the artifacts (each changed unit gets a new binding set, a new knowledge unit with the same logical ID and a new overlay);
- the size of the device file after the import and after the update. The bench closes the store before it reads a size, so the write-ahead log is in the file;
- `open_context` and `search_text` on a filtered bundle (BUN-FLT-3): the export of `c000` with the filter `region = r0`, whose roots are 25 knowledge units.

With `-clients 16`, each read measure also runs with 16 concurrent clients. Each client makes its next call when its last call ends.

A call with a status other than `ok` is a failed call, for example `failed` with `deadline_exceeded` after the deadline of the read binding (NET-LIM-2, 30 s by default). The measure counts the failed calls and continues. The time of a failed call stays in the percentiles. Thus a p95 with failed calls is a lower bound. The result gives the count of failed calls and the first status. Each other error stops the bench.

## The CI rule

A shared CI runner is slow, and its time varies. Thus `make check-bench` fails only on a clear miss:

1. A clear miss is a p95 that is more than 3 times its target (`BENCH_FACTOR`).
2. The `S0` column of the design is the target of each `S0` run: on the storage in memory, on PostgreSQL and on the device store.
3. A known miss does not fail the job. `BENCH_KNOWN` in the [Makefile](../Makefile) lists each target that the first run missed, with the issue of its cause. The job still prints each value and marks each known miss.
4. A failed call is a clear miss. A known miss does not excuse it.
5. Each read measure stops after 10 s (`BENCH_MAX_TIME`), but it makes 20 calls or more. Thus a slow measure has a smaller count in CI. The count is in the output.

When a change makes a measure meet its target, remove the measure from `BENCH_KNOWN` in the same change.

## The first run

The first run was on 2026-11-25 (UTC), with `-seed 1`. The result files are [`docs/bench/s0-backend.json`](bench/s0-backend.json), [`docs/bench/s0-device.json`](bench/s0-device.json) and [`docs/bench/s1-pg.json`](bench/s1-pg.json).

The machine:

- CPU: Apple M4, 10 CPUs, 24 GiB of memory.
- OS: Darwin 27.0.0 arm64. Go: go1.27.2.
- PostgreSQL: 17.12, the image `postgres:17` at the digest of CI. It ran in Docker Desktop on the same host, in a Linux VM with 7.7 GiB of memory. Thus the PostgreSQL times include the network of the VM.
- SQLite of the device store: 3.54.1 (`modernc.org/sqlite`).

Each number in this section is for this machine only. Each measure of `S0` made 1 000 calls with one client. A time is in ms. Each value is from the result files.

### `S0`, the read measures

| Measure | Target p95 | Memory p50 | Memory p95 | PostgreSQL p50 | PostgreSQL p95 | Device p50 | Device p95 |
|---|---|---|---|---|---|---|---|
| `open_context` | 25 | 3.7 | 6.3 | 988 | **1 555** | 32.8 | **40.4** |
| `read_row` | 5 | 8.5 | **10.5** | 17.0 | **20.4** | 9.1 | **10.6** |
| `read_artifact_64k` | 5 | 0.18 | 0.23 | 1.6 | 3.2 | 0.20 | 0.22 |
| `search_text` | 100 | 103 | **144** | 1 414 | **1 962** | 133 | **145** |
| `query_data_where` | none | 10.3 | 12.2 | 22.1 | 31.6 | 10.9 | 12.1 |

A value in bold misses its target. Each miss is in `BENCH_KNOWN`, with #128. On PostgreSQL, `open_context` is 62 times its target and `search_text` is 20 times its target.

### `S0`, the publication of 1 000 descriptors

Five publications, in the process. Each one adds 40 units to `c000` and publishes a new head.

| Step | Memory p50 | Memory p95 | PostgreSQL p50 | PostgreSQL p95 |
|---|---|---|---|---|
| The `put` calls | 46.4 | 51.6 | 1 779 | 2 172 |
| `create_snapshot` | 15.0 | 21.1 | 1 253 | 1 699 |
| `create_candidate` | 4.4 | 5.2 | 1 275 | 1 619 |
| `publish` | 3.3 | 3.4 | 3.0 | 4.0 |

The build of the store took 1.9 s in memory and 47.5 s on PostgreSQL.

### `S0`, the PostgreSQL size

The PostgreSQL relations of `S0` took 140 296 192 bytes, with all indexes. The formula of the size target gives 355 532 800 bytes for `S0`. The design states the size target for `S1` only. The relations are smaller than the payload bytes, because PostgreSQL compresses the text payloads (TOAST).

### `S0`, the device bundle

| Value | Result |
|---|---|
| The bundle of all heads | 287 479 190 bytes |
| Its payload bytes | 209 714 930 bytes |
| The bundle size as a ratio to the payload bytes | 1.370 |
| `export` of the bundle | 1 396 ms |
| The first import | 18 082 ms |
| The device file after the first import | 382 717 952 bytes |
| The update bundle, 1% of the artifacts changed | 287 479 190 bytes, 726 new descriptors |
| The import of the update | 3 610 ms |
| The device file after the update | 389 545 984 bytes |
| The filtered bundle (`region = r0`, 25 knowledge-unit roots) | 10 985 668 bytes, import 314 ms |

The write-ahead log was 0 bytes at each size. The design gives no target for these values.

The update bundle has the same size as the first bundle. Reason: an `export` holds the full closure of its roots (BUN-EXP-2), so an update sends each byte again. The ratio of 1.370 comes from the base64 text of the payloads in the bundle. #129 is the issue for a delta bundle and a raw byte form.

| Device read | p50 | p95 |
|---|---|---|
| `open_context` with the snapshot root | 32.8 | 40.4 |
| `open_context` of the filtered bundle, 25 roots | 3.8 | 4.0 |
| `search_text` of the filtered bundle | 18.8 | 21.2 |

### `S1` on PostgreSQL

`make bench-s1` ran `S1` with one client and with 16 clients, with a time limit of 120 s for each measure (`S1_MAX_TIME`). The build of the store took 474 s. The main context has 19 991 members.

| Measure | Target p95, one client | 1 client: count | p50 | p95 | 16 clients: count | p50 | p95 |
|---|---|---|---|---|---|---|---|
| `open_context` | 25 | 20 | 8 024 | **11 378** | 64 | 30 000 | **30 002**, 64 failed |
| `read_row` | 10 | 608 | 189 | **255** | 907 | 2 011 | 2 903 |
| `read_artifact_64k` | 10 | 1 000 | 1.4 | 1.9 | 1 000 | 10.0 | 14.4 |
| `search_text` | 100 | 21 | 5 527 | **6 545** | 80 | 27 548 | 29 164 |
| `query_data_where` | none | 515 | 219 | 305 | 910 | 2 005 | 2 780 |

The design gives no target for 16 clients. With 16 clients, each `open_context` call failed with `deadline_exceeded` after the 30 s deadline of the read binding. Thus the p95 of 30 002 ms is a lower bound. A count of less than 1 000 shows that the time limit stopped the measure.

| `publish_1000` step, `S1` | p50 | p95 |
|---|---|---|
| The `put` calls | 1 428 | 1 991 |
| `create_snapshot` | 4 188 | 4 635 |
| `create_candidate` | 4 267 | 4 787 |
| `publish` | 2.5 | 4.5 |

The PostgreSQL relations of `S1` took 1 501 536 256 bytes, with all indexes. The size target is 3 630 825 472 bytes. Thus `S1` meets the size target on this machine. Most payloads of `S1` are text, and PostgreSQL compresses them.

## Findings

- `open_context`, the row read and `search_text` miss their targets on each storage. On PostgreSQL, the time grows with the store: from `S0` to `S1`, the p95 of `open_context` grows from 1.6 s to 11.4 s and the p95 of `search_text` from 2.0 s to 6.5 s. #128 is the issue for the read indexes of the design.
- In an earlier `S1` run on this machine, one `open_context` call with one client failed with `deadline_exceeded`. That run stopped at the failure, so it has no result file. The bench now counts a failed call and continues.
- The 64 KiB artifact read meets its target on each storage and on both shapes.
- An update bundle holds the full closure (BUN-EXP-2), so a change of 1% of the artifacts sends the full 287 MB again. The bundle is 1.37 times its payload bytes. #129 is the issue for a delta bundle and a raw byte form.
- The PostgreSQL size meets the target on `S0` and on `S1`.
