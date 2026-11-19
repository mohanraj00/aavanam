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

# The budget bench of the backend (docs/design/backend.md, section 7). It needs AAVANAM_PG_DSN.
.PHONY: check-bench
check-bench:
	@if [ -d backend/bench ]; then cd backend && go run ./bench; \
	else echo "No backend/bench in the tree. The budget bench did not run."; fi
