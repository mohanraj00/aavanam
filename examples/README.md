# Examples

Each example is one file or one folder in this folder. Each example uses only the public API of the `aavanam` package. The test [tests/test_examples.py](../tests/test_examples.py) runs each example and compares its output with the output in this file.

## Run an example from a clean checkout

You need Python 3.14 or later. The [README](../README.md#python-version) gives the reason.

1. Clone the repository and go to its folder:

   ```bash
   git clone https://github.com/mohanraj00/aavanam && cd aavanam
   ```

2. Make a clean environment and install the pinned build tools:

   ```bash
   python3 -m venv ../aavanam-build
   ../aavanam-build/bin/python -m pip install --require-hashes --only-binary :all: --no-deps -r .github/requirements-build.txt
   ```

3. Build the wheel and the source archive. The command writes them to `../aavanam-dist`:

   ```bash
   ../aavanam-build/bin/python -m build --no-isolation --outdir ../aavanam-dist .
   ```

4. Make a second clean environment and install only the wheel:

   ```bash
   python3 -m venv ../aavanam-user
   ../aavanam-user/bin/python -m pip install --no-index --no-deps ../aavanam-dist/aavanam-0.1.0-py3-none-any.whl
   ```

5. Run the example:

   ```bash
   ../aavanam-user/bin/python -I examples/quickstart.py
   ```

The `-I` flag removes the script folder and `PYTHONPATH` from the import path. Thus the example imports `aavanam` from the wheel, not from `src/`.

The build writes `build/` and `src/aavanam.egg-info/` in the checkout. Remove them before you run `make check`, because the project check records each file in the checkout. The CI builds from a clean copy of the commit for this reason.

The package is not on a package index. A publication to an index needs a separate approval of the maintainer.

## quickstart.py

The example does these steps in an in-memory store:

1. It puts a source, a reading of the source and the evidence that links the reading to a binding set.
2. It creates a snapshot of the collection `pump-manual`.
3. It makes a publication candidate and publishes it with a receipt. The example gives the acceptance function, because the application owns the acceptance decision (PUB-RCP-2).
4. It opens a read context on the published head and finds the reading with a lexical search.
5. It reads all the bytes of the reading with the `read` operation (CRD-READ-3), and finds the quote in these bytes.
6. It reads only the bytes of the quote with a selector of profile `aavanam/bytes/v1` (CRD-SEL-3). Then it prints the quote and its citation.

The citation names the collection, the snapshot, the item and a byte range in the `aavanam/bytes/v1` selector form. The selector of the citation is the selector of the second read. Thus a consumer can read the quote again with the reference and the selector of the citation.

Output of `quickstart.py`:

```text
snapshot ok
publish ok published head is the snapshot True
text 'Replace the seal every 500 hours.'
citation {
  "collection": "pump-manual",
  "kind": "reading",
  "ref": "sha256:8ebe363e604602468b20713463e26473626830a7f02167c6914963edf4e123df",
  "selector": {
    "end": 43,
    "selector_profile": "aavanam/bytes/v1",
    "start": 10
  },
  "snapshot": "sha256:41b454e3249a0bdad455118479bd34ade91a652b2957e9b8cb3f6676ca6a431c"
}
```
