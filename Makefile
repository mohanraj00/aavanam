# Run the checks that AGENTS.md names. The null implementation must fail with exit 1.
PYTHON ?= python3

.PHONY: check
check:
	$(PYTHON) -B tools/check_spec.py
	$(PYTHON) -B -m unittest discover -s conformance
	$(PYTHON) -B -m unittest discover -s tests
	$(PYTHON) -B conformance/runner.py --adapter store_adapter:StoreAdapter
	$(PYTHON) -B conformance/runner.py --adapter null_adapter:NullAdapter >/dev/null 2>&1; \
	status=$$?; if [ "$$status" -ne 1 ]; then echo "The null implementation must fail with exit 1, not $$status."; exit 1; fi
	$(PYTHON) -B tools/check_project.py
	@if command -v go >/dev/null 2>&1; then $(MAKE) check-go check-backend-mem; \
	else echo "No go command. The Go checks did not run (make check-go check-backend-mem)."; fi

RUNNER = $(PYTHON) -B conformance/runner.py --adapter backend_adapter:BackendAdapter
BACKEND = AAVANAM_WRITER=backend AAVANAM_DEVICE=reference

# The format check, the vet and the tests of the two Go modules. They need no database.
.PHONY: check-go
check-go:
	cd backend && test -z "$$(gofmt -l .)" && go vet ./... && go test ./...
	cd reference && test -z "$$(gofmt -l .)" && go vet ./... && go test ./...

# The listed vectors on the backend with the storage in memory. They need no database.
.PHONY: check-backend-mem
check-backend-mem:
	$(BACKEND) AAVANAM_STORAGE=mem $(RUNNER) \
		--expect backend/conformance/passing-mem-core.txt --expect backend/conformance/passing-mem-read.txt

# The listed vectors on PostgreSQL. Set AAVANAM_PG_DSN to a PostgreSQL 17 database, for example a local container.
.PHONY: check-backend-pg
check-backend-pg:
	$(BACKEND) AAVANAM_STORAGE=pg $(RUNNER) --expect backend/conformance/passing.txt

# The null storage refuses each call, so the run must fail with exit 1. The summary line shows that the run
# came to its end, because a driver that stops also gives a status other than 0.
.PHONY: check-null-driver
check-null-driver:
	@out=$$($(BACKEND) AAVANAM_STORAGE=null $(RUNNER) 2>&1); status=$$?; echo "$$out" | tail -n 3; \
	if [ "$$status" -ne 1 ]; then echo "The null driver must fail with exit 1, not $$status."; exit 1; fi; \
	if ! echo "$$out" | tail -n 1 | grep -Eq '^[0-9]+ passed, [0-9]+ failed$$'; then \
		echo "The run with the null driver did not come to its end."; exit 1; fi

.PHONY: check-backend
check-backend: check-backend-mem check-backend-pg check-null-driver

# The listed vectors on the device store, with the Python reference as the writer store.
.PHONY: check-device
check-device:
	AAVANAM_WRITER=reference AAVANAM_DEVICE=go $(RUNNER) --expect reference/conformance/passing.txt

# The budget bench (docs/bench.md). It runs S0 on the storage in memory, on PostgreSQL if AAVANAM_PG_DSN
# is set, and on the device store, and prints each measured value. It fails only on a clear miss: a p95 that
# is more than BENCH_FACTOR times its target and is not in BENCH_KNOWN. Reason: the time of a shared CI
# runner varies. Each entry of BENCH_KNOWN is a miss of the first run, with the issue of its cause. Remove
# an entry when its measure meets its target. BENCH_MAX_TIME limits each read measure, so the CI job ends
# in its time limit. The PostgreSQL run removes each row of the database of AAVANAM_PG_DSN.
BENCH_CALLS ?= 1000
BENCH_FACTOR ?= 3
BENCH_MAX_TIME ?= 10s
BENCH_KNOWN ?= mem/read_row=\#128,mem/search_text=\#128,pg/open_context=\#128,pg/read_row=\#128,pg/search_text=\#128,device/open_context=\#128,device/read_row=\#128,device/search_text=\#128
BENCH_CHECK = -calls $(BENCH_CALLS) -max-time $(BENCH_MAX_TIME) -check -factor $(BENCH_FACTOR) -known '$(BENCH_KNOWN)'
.PHONY: check-bench
check-bench:
	@storages=mem; if [ -n "$$AAVANAM_PG_DSN" ]; then storages=mem,pg; \
	else echo "No AAVANAM_PG_DSN. The budget bench does not run on PostgreSQL."; fi; \
	cd backend && go run ./bench -shape S0 -storage $$storages $(BENCH_CHECK)
	cd reference && go run ./bench -shape S0 $(BENCH_CHECK)

# The S1 bench on PostgreSQL, with one client and with 16 concurrent clients. It does not run in CI. It needs
# AAVANAM_PG_DSN. It writes the result file BENCH_OUT, outside the checkout by default. S1_MAX_TIME limits
# each read measure.
BENCH_OUT ?= $(or $(TMPDIR),/tmp)/aavanam-bench-s1.json
S1_MAX_TIME ?= 120s
.PHONY: bench-s1
bench-s1:
	cd backend && go run ./bench -shape S1 -storage pg -clients 16 -calls $(BENCH_CALLS) -max-time $(S1_MAX_TIME) \
		-out "$(abspath $(BENCH_OUT))"
