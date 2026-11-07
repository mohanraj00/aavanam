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
