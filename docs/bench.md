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

If PostgreSQL is in a container of a VM, each PostgreSQL time includes the port forward of the VM. To measure with no port forward, build the bench for the OS of the container and run it in the container of PostgreSQL:

```sh
cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$TMPDIR/aavanam-bench" ./bench
docker cp "$TMPDIR/aavanam-bench" <container>:/tmp/aavanam-bench
docker exec -e AAVANAM_PG_DSN=<DSN on the port of the container> <container> \
    /tmp/aavanam-bench -shape S1 -storage pg -clients 16 -max-time 120s -out /tmp/s1.json
```

In this setup the bench and PostgreSQL use the same CPUs of the VM.

## Layout

- `backend/bench/synth`: the generator of the synthetic store.
- `backend/bench/measure`: the timer, the percentiles, the machine description, the result file and the budget check.
- `backend/bench/suite`: the read measures. The backend and the device bench use the same requests.
- `backend/bench`: the backend bench, for the storage in memory and for PostgreSQL.
- `reference/bench`: the device bench. It is in the `reference` module, because the device store is in that module and the `backend` module does not import it.

## The synthetic store

The generator makes the store from a shape and a seed. The bytes of each payload come from the seed and the index of the payload only. Thus two runs with one shape and one seed send the same requests in the same order. The result file gives the SHA-256 digest of these requests (`request_digest`). A test makes the small shape two times and compares the store fingerprints.

The data is neutral: pseudo-words of two or three syllables from a fixed set, numbers and random bytes.

Each collection is a `serving` collection with a head. A knowledge unit binds 20 artifacts, and an overlay gives it the metadata keys `region` and `band`. The key `band` has the value `b0` for the first 500 units of the store, in the order of the collections, and `b1` for each other unit. Issue #128 added this key, so the store of a later run has different overlay descriptors than the store of the first run. The shape and the payload bytes are the same. The main collection, `c000`, has the measured read context:

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
- `open_context` and `search_text` on a filtered bundle (BUN-FLT-3): the export of `c000` with the filter `region = r0`, whose roots are 25 knowledge units;
- each read measure on a second filtered bundle: the export of each head with the filter `band = b0`, whose roots are 500 knowledge units (`band:<measure>`). Its context has 10 000 text artifacts, the largest table and the 64 KiB artifacts of `c000`.

With `-clients 16`, each read measure also runs with 16 concurrent clients. Each client makes its next call when its last call ends.

A call with a status other than `ok` is a failed call, for example `failed` with `deadline_exceeded` after the deadline of the read binding (NET-LIM-2, 30 s by default). The measure counts the failed calls and continues. The time of a failed call stays in the percentiles. Thus a p95 with failed calls is a lower bound. The result gives the count of failed calls and the first status. Each other error stops the bench.

## The CI rule

A shared CI runner is slow, and its time varies. Thus `make check-bench` fails only on a clear miss:

1. A clear miss is a p95 that is more than 3 times its target (`BENCH_FACTOR`).
2. The `S0` column of the design is the target of each `S0` run: on the storage in memory, on PostgreSQL and on the device store.
3. A known miss does not fail the job. `BENCH_KNOWN` in the [Makefile](../Makefile) lists each target that a recorded run missed, with the issue of its cause. The list is empty after #128. The job still prints each value and marks each known miss.
4. A failed call is a clear miss. A known miss does not excuse it.
5. Each read measure stops after 10 s (`BENCH_MAX_TIME`), but it makes 20 calls or more. Thus a slow measure has a smaller count in CI. The count is in the output.

When a change makes a measure meet its target, remove the measure from `BENCH_KNOWN` in the same change.

## The first run

The first run was on 2026-11-25 (UTC), with `-seed 1`. The result files are [`docs/bench/s0-backend.json`](bench/s0-backend.json), [`docs/bench/s0-device.json`](bench/s0-device.json) and [`docs/bench/s1-pg.json`](bench/s1-pg.json).

The machine:

- CPU: Apple M4, 10 CPUs, 24 GiB of memory.
- OS: Darwin 27.0.0 arm64. Go: go1.27.2.
- PostgreSQL: 17.12, the image `postgres:17` at the digest of CI. It ran in a container on the same host, in a Linux VM of Colima with 4 CPUs and 7.7 GiB of memory. Thus the PostgreSQL times include the port forward of the VM. An earlier text of this record named Docker Desktop. That was an error.
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

A value in bold misses its target. At the time of the first run, each miss was in `BENCH_KNOWN`, with #128. On PostgreSQL, `open_context` is 62 times its target and `search_text` is 20 times its target.

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

## The second run, after #128

The second run was on 2026-11-25 (UTC), with `-seed 1`, on the change of #128. The result files are [`docs/bench/128-s0-backend.json`](bench/128-s0-backend.json), [`docs/bench/128-s0-device.json`](bench/128-s0-device.json), [`docs/bench/128-s1-pg-a.json`](bench/128-s1-pg-a.json), [`docs/bench/128-s1-pg-b.json`](bench/128-s1-pg-b.json) and [`docs/bench/128-s1-pg-local.json`](bench/128-s1-pg-local.json).

The machine is the machine of the first run, with the same OS, Go, PostgreSQL and SQLite. PostgreSQL ran in a container in the Linux VM of Colima 0.10.3, with 4 CPUs and 7.7 GiB of memory. The run has two setups:

- Through the port forward: the bench ran on the host and connected to the port that the VM forwards to the loopback address. This is the setup of the first run.
- In the container: the bench ran in the container of PostgreSQL, with no port forward. The bench and PostgreSQL used the same 4 CPUs of the VM. Its result file gives Linux and 4 CPUs as the machine.

The host was not idle: sessions of other projects ran on it. The table gives the load average of 1 minute of the host. It includes the load of the bench.

| Part | Start (UTC) | Load at the start | Load at the end |
|---|---|---|---|
| `S0`, memory and PostgreSQL, through the port forward | 09:35 | 4.5 | 5.4 |
| `S0`, device store | 09:38 | 5.4 | 3.7 |
| `S1`, through the port forward, run A | 09:39 | 3.7 | 11.9 |
| `S1`, in the container | 09:54 | 11.9 | 5.5 |
| `S1`, through the port forward, run B | 10:02 | 3.6 | 10.0 |

Each number in this section is for this machine and this load only. A time is in ms. "Before" is the value of the first run. Each measure of `S0` made 1 000 calls with one client.

### `S0`, the read measures

| Measure | Target p95 | Memory p50 | Memory p95 | PostgreSQL p50 | PostgreSQL p95 | Device p50 | Device p95 |
|---|---|---|---|---|---|---|---|
| `open_context` | 25 | 3.1 | 4.2 | 0.86 | 1.2 | 0.058 | 0.092 |
| `read_row` | 5 | 0.049 | 0.083 | 1.0 | 1.2 | 0.043 | 0.050 |
| `read_artifact_64k` | 5 | 0.17 | 0.19 | 1.2 | 1.5 | 0.26 | 0.31 |
| `search_text` | 100 | 5.4 | 7.5 | 17.0 | 22.3 | 11.2 | 13.1 |
| `query_data_where` | none | 7.8 | 8.6 | 16.7 | 21.7 | 11.6 | 13.8 |

Each measure meets its target on each storage. The p95 before and after:

| Measure | Target p95 | Memory before | Memory after | PostgreSQL before | PostgreSQL after | Device before | Device after |
|---|---|---|---|---|---|---|---|
| `open_context` | 25 | 6.3 | 4.2 | 1 555 | 1.2 | 40.4 | 0.092 |
| `read_row` | 5 | 10.5 | 0.083 | 20.4 | 1.2 | 10.6 | 0.050 |
| `read_artifact_64k` | 5 | 0.23 | 0.19 | 3.2 | 1.5 | 0.22 | 0.31 |
| `search_text` | 100 | 144 | 7.5 | 1 962 | 22.3 | 145 | 13.1 |
| `query_data_where` | none | 12.2 | 8.6 | 31.6 | 21.7 | 12.1 | 13.8 |

### `S0`, the filtered bundles on the device

| Device read | Target p95 | p50 | p95 |
|---|---|---|---|
| `open_context` with the snapshot root | 25 | 0.058 | 0.092 |
| `open_context` of the bundle with 25 roots (`region = r0`) | 25 | 0.10 | 0.15 |
| `search_text` of the bundle with 25 roots | 100 | 2.5 | 3.3 |
| `open_context` of the bundle with 500 roots (`band = b0`) | 25 | 1.1 | 1.5 |
| `read_row` of the bundle with 500 roots | 5 | 0.47 | 0.65 |
| `read_artifact_64k` of the bundle with 500 roots | 5 | 0.51 | 0.77 |
| `search_text` of the bundle with 500 roots | 100 | 32.2 | 36.7 |
| `query_data_where` of the bundle with 500 roots | none | 11.0 | 12.0 |

The bundle with 500 knowledge-unit roots is 166 566 216 bytes, and its import took 6 944 ms. Each device read meets its target for the snapshot root and for both filtered bundles (BUN-FLT-3). Before, `open_context` of the bundle with 25 roots had a p95 of 4.0 ms and its `search_text` 21.2 ms. The first run has no bundle with 500 roots.

### `S0`, the publication of 1 000 descriptors

| Step | Memory p50 | Memory p95 | PostgreSQL p50 | PostgreSQL p95 |
|---|---|---|---|---|
| The `put` calls | 72.4 | 82.7 | 1 554 | 1 686 |
| `create_snapshot` | 13.5 | 19.2 | 1 086 | 1 522 |
| `create_candidate` | 3.9 | 5.7 | 10.1 | 11.3 |
| `publish` | 3.2 | 3.7 | 2.7 | 3.6 |

The build of the store took 5.9 s in memory and 43.0 s on PostgreSQL. Before, it took 1.9 s and 47.5 s.

The write to the storage in memory is slower than before: the p50 of the `put` calls was 46.4 ms. Reason: the storage in memory now makes the token set of each text payload at the write. On PostgreSQL, the p50 of `create_candidate` was 1 275 ms before. It walked the closure of the snapshot through `edges`. Now it reads the stored closure of the snapshot. The other PostgreSQL steps did not change by more than 15%.

### `S0`, the size and the device bundle

The PostgreSQL relations of `S0` took 216 137 728 bytes, with all indexes and the derived tables. Before, they took 140 296 192 bytes. The formula of the size target gives 355 532 800 bytes.

| Value | Before | After |
|---|---|---|
| The bundle of all heads | 287 479 190 bytes | 287 493 078 bytes |
| The bundle size as a ratio to the payload bytes | 1.370 | 1.370 |
| `export` of the bundle | 1 396 ms | 1 277 ms |
| The first import | 18 082 ms | 13 967 ms |
| The device file after the first import | 382 717 952 bytes | 382 730 240 bytes |
| The import of the update, 726 new descriptors | 3 610 ms | 4 855 ms |
| The device file after the update | 389 545 984 bytes | 392 482 816 bytes |
| The filtered bundle with 25 roots | 10 985 668 bytes, import 314 ms | 10 985 668 bytes, import 282 ms |

The write-ahead log was 0 bytes at each size. The bundle is larger than before because each overlay has the new key `band`. The device file after the update is 2.9 MB larger than before. A possible cause is the `table_rows` rows of the largest table (2 MiB), which the row read of the bench now writes. The import of the update was slower than before. This record does not give the cause.

### `S1` on PostgreSQL, one client

`make bench-s1` ran `S1` with one client and with 16 clients, with a time limit of 120 s for each measure. Each cell is p50 / p95.

| Measure | Target p95 | Before | Port forward, run A | Port forward, run B | In the container |
|---|---|---|---|---|---|
| `open_context` | 25 | 8 024 / 11 378 | 1.0 / 1.4 | 1.6 / 3.7 | 0.31 / 0.45 |
| `read_row` | 10 | 189 / 255 | 1.1 / 1.6 | 1.6 / 3.7 | 0.31 / 0.36 |
| `read_artifact_64k` | 10 | 1.4 / 1.9 | 1.5 / 2.3 | 2.2 / 3.8 | 0.52 / 0.66 |
| `search_text` | 100 | 5 527 / 6 545 | 20.1 / 26.0 | 25.1 / 35.4 | 17.1 / 21.6 |
| `query_data_where` | none | 219 / 305 | 186 / 253 | 165 / 210 | 111 / 165 |

Each measure with a target meets it in each setup and in each run. The time limit stopped `query_data_where` at 624 calls in run A and at 691 calls in run B. Each other measure made 1 000 calls.

### `S1` on PostgreSQL, 16 clients

The design gives no target for 16 clients. The limit of this record is 2 times the target of one client, with no failed call. Each cell is p50 / p95.

| Measure | Limit p95 | Before | Port forward, run A | Port forward, run B | In the container |
|---|---|---|---|---|---|
| `open_context` | 50 | 30 000 / 30 002, 64 failed | 5.9 / 7.5 | 4.7 / 7.0 | 1.4 / 1.6 |
| `read_row` | 20 | 2 011 / 2 903 | 5.7 / **23.1** | 4.4 / 6.3 | 0.89 / 1.2 |
| `read_artifact_64k` | 20 | 10.0 / 14.4 | 10.0 / **27.9** | 7.7 / 11.4 | 2.8 / 5.3 |
| `search_text` | 200 | 27 548 / 29 164 | 190 / **423** | 144 / 179 | 124 / 164 |
| `query_data_where` | none | 2 005 / 2 780 | 1 807 / 2 257 | 1 644 / 1 856 | 937 / 1 252 |

A value in bold is more than the limit. No call failed in run A, in run B or in the container. Each measure made 1 000 calls.

Through the port forward, the result is not stable. Run A is more than the limit for three measures, and run B is not more than the limit for each measure. The two runs have the same code and the same store. Thus this record does not show that the backend meets the limit for 16 clients through the port forward of this host.

In the container, each measure is not more than its limit. `search_text` has a p95 of 164 ms against the limit of 200 ms. This is the only setup of this record that shows the limit for 16 clients, and it has one run.

A profile of `search_text` with 16 clients on the host gave these values. The profile has no result file:

- The process that forwards the port of the VM used more than one CPU.
- PostgreSQL used about 1.3 of its 4 CPUs, and most of its connections were idle.
- A pool of 32 connections, in place of the default of 10, gave the same times.
- One search of one client took about 17 ms: about 6 ms for the scan of the candidate bytes, about 6 ms for the statement that reads the bit sets of the 2 000 text digests, and the rest for the other statements.

The profile shows no wait for a connection and no wait in PostgreSQL. This points to the CPUs of the host and to the port forward as the limit for 16 clients: the bench, the port forward and the VM of PostgreSQL use the same 10 CPUs, with the load of other sessions. The run in the container agrees with this, but this record has no measure that proves it.

### `S1`, the publication of 1 000 descriptors

Each cell is p50 / p95.

| Step | Before | Port forward, run A | Port forward, run B | In the container |
|---|---|---|---|---|
| The `put` calls | 1 428 / 1 991 | 3 173 / 3 664 | 1 715 / 2 221 | 404 / 566 |
| `create_snapshot` | 4 188 / 4 635 | 10 239 / 12 503 | 5 535 / 5 831 | 982 / 1 037 |
| `create_candidate` | 4 267 / 4 787 | 77 / 102 | 46 / 70 | 32 / 69 |
| `publish` | 2.5 / 4.5 | 4.2 / 16.8 | 3.1 / 4.2 | 0.86 / 2.1 |
| The build of the store | 474 s | 513 s | 484 s | 218 s |

`create_candidate` reads the stored closure of the snapshot now. Through the port forward, the `put` calls and `create_snapshot` were slower than before in both runs: the p50 of `create_snapshot` is 1.3 times the value of the first run in run B and 2.4 times in run A. Possible causes are the derived rows that a write now makes (the `closures` rows of a new snapshot and the token sets) and the host load. This record does not separate them. The sum of the p50 of the four steps was 9 886 ms before. It is 7 300 ms in run B and 13 493 ms in run A.

The PostgreSQL relations of `S1` took 2 191 810 560 bytes in run A and in the container, and 2 191 851 520 bytes in run B, with all indexes and the derived tables. Before, they took 1 501 536 256 bytes. The size target is 3 630 825 472 bytes. Thus `S1` meets the size target on this machine.

## Findings of the first run

- `open_context`, the row read and `search_text` miss their targets on each storage. On PostgreSQL, the time grows with the store: from `S0` to `S1`, the p95 of `open_context` grows from 1.6 s to 11.4 s and the p95 of `search_text` from 2.0 s to 6.5 s. #128 is the issue for the read indexes of the design.
- In an earlier `S1` run on this machine, one `open_context` call with one client failed with `deadline_exceeded`. That run stopped at the failure, so it has no result file. The bench now counts a failed call and continues.
- The 64 KiB artifact read meets its target on each storage and on both shapes.
- An update bundle holds the full closure (BUN-EXP-2), so a change of 1% of the artifacts sends the full 287 MB again. The bundle is 1.37 times its payload bytes. #129 is the issue for a delta bundle and a raw byte form.
- The PostgreSQL size meets the target on `S0` and on `S1`.

## Findings of the second run

- On `S0`, each read measure meets its target on the storage in memory, on PostgreSQL and on the device store. On the device, this is true for the snapshot root, for the filtered bundle with 25 roots and for the filtered bundle with 500 knowledge-unit roots. `BENCH_KNOWN` is empty.
- On `S1` with one client, each read measure meets its target, through the port forward and in the container.
- On `S1` with 16 clients, no call failed. In the container, each measure is not more than 2 times its target. Through the port forward of this host, one run of two is more than this limit for `read_row`, `read_artifact_64k` and `search_text`. The limit for 16 clients is shown only in the setup with no port forward.
- The write to the storage in memory is slower, because it makes the token sets. On `S1` through the port forward, the `put` calls and `create_snapshot` were slower than in the first run, and `create_candidate` is about 90 times faster. The cause of the slower steps is not known from this record.
- The derived tables add 46% to the PostgreSQL size of `S1`. The size is 60% of the size target.
- The scan of the candidate bytes makes one allocation for each word. It is a possible later change to decrease the CPU of `search_text`.
