# PDF example: cited, versioned facts with groundgate

This example reads a synthetic PDF data sheet with groundgate. It keeps the admitted facts in aavanam snapshots, and reads each fact back with its citation. It does the steps of issue #71:

1. It installs a pinned groundgate release and its dependencies, each with its hash.
2. groundgate reads the PDF, admits three values and rejects one value that the PDF does not contain.
3. The admitted-facts adapter keeps the `facts@1` export in a serving snapshot and an evidence snapshot (ADP-FXT-1 to ADP-FXT-7).
4. The example publishes the serving snapshot. Then it reads each fact back with the `read` operation (CRD-READ-3). It prints the value and its citation: the page, the span, the cited text, the source digest and the text digest.
5. Version 2 of the PDF changes one value. The example publishes a second snapshot, and shows that the first snapshot gives the same facts as before.

The test [tests/test_pdf_example.py](../../tests/test_pdf_example.py) runs the example and compares its output with the output in this file. A fact with no citation makes the example and the test fail.

## Files

- `pdf_facts.py`: the example.
- `datasheet-v1.pdf` and `datasheet-v2.pdf`: the synthetic data sheets. Each has one page and the same three values, except the flow rate. They name no real product, company or person.
- `make_pdf.py`: the script that writes the two PDF files. Each PDF is ASCII text, with no compression. The test checks that the script gives the committed bytes.
- `requirements.txt`: groundgate 0.14.0 with its extra `pdf`, and each dependency, pinned with its hash.

## Run the example from a clean checkout

You need Python 3.14 or later.

1. Do steps 1 to 4 of [Run an example from a clean checkout](../README.md#run-an-example-from-a-clean-checkout). Then the environment `../aavanam-user` has the aavanam wheel.

2. Install the pinned groundgate and its dependencies in the same environment:

   ```bash
   ../aavanam-user/bin/python -m pip install --require-hashes --only-binary :all: --no-deps -r examples/pdf_facts/requirements.txt
   ```

3. Run the example:

   ```bash
   ../aavanam-user/bin/python -I examples/pdf_facts/pdf_facts.py
   ```

The hashes of `cryptography` and `cffi` are for Linux x86_64 and macOS arm64. On a different platform, the install stops with a hash error.

## Known limit: the page

groundgate 0.14.0 gives no page in the `facts@1` citation. The citation gives the span, the source digest and the text digest. The example takes the page from the layout of the groundgate reading of the PDF, in the function `page_of`. groundgate 0.15 adds the page to the export. A later change moves the example to the page of the export, and changes only `page_of`.

The source digest and the text digest are the digests of groundgate, kept as native identities (ADP-FXT-3). They are not the digests of the aavanam payloads.

Output of `pdf_facts.py`:

```text
groundgate flow admitted
groundgate noise rejected
groundgate pressure admitted
groundgate weight admitted
published datasheet-v1.pdf head is the new snapshot True
fact flow '120' {"page": 1, "source": "sha256:a3839cc36352f2d1f754f5020712ab9309f632ada30f4d3491a725e6c53ed01c", "span": [31, 51], "text": "Flow rate: 120 L/min", "text_digest": "sha256:075742833445d2414013cb741efc5dba2a738fcf747291547adf81a43d1a13f6"}
fact pressure '6' {"page": 1, "source": "sha256:a3839cc36352f2d1f754f5020712ab9309f632ada30f4d3491a725e6c53ed01c", "span": [53, 76], "text": "Maximum pressure: 6 bar", "text_digest": "sha256:075742833445d2414013cb741efc5dba2a738fcf747291547adf81a43d1a13f6"}
fact weight '42' {"page": 1, "source": "sha256:a3839cc36352f2d1f754f5020712ab9309f632ada30f4d3491a725e6c53ed01c", "span": [78, 91], "text": "Weight: 42 kg", "text_digest": "sha256:075742833445d2414013cb741efc5dba2a738fcf747291547adf81a43d1a13f6"}
groundgate flow admitted
groundgate noise rejected
groundgate pressure admitted
groundgate weight admitted
published datasheet-v2.pdf head is the new snapshot True
fact flow '135' {"page": 1, "source": "sha256:8d08b35ff562ca38313054a24ce919ec0b38c233fd5c5eb2fbe986d0c1339beb", "span": [31, 51], "text": "Flow rate: 135 L/min", "text_digest": "sha256:ccda8abf4606d458506c7d200a990384feb806897a2d54ec18dfee657622651c"}
fact pressure '6' {"page": 1, "source": "sha256:8d08b35ff562ca38313054a24ce919ec0b38c233fd5c5eb2fbe986d0c1339beb", "span": [53, 76], "text": "Maximum pressure: 6 bar", "text_digest": "sha256:ccda8abf4606d458506c7d200a990384feb806897a2d54ec18dfee657622651c"}
fact weight '42' {"page": 1, "source": "sha256:8d08b35ff562ca38313054a24ce919ec0b38c233fd5c5eb2fbe986d0c1339beb", "span": [78, 91], "text": "Weight: 42 kg", "text_digest": "sha256:ccda8abf4606d458506c7d200a990384feb806897a2d54ec18dfee657622651c"}
first snapshot unchanged True
changed in the second snapshot ['flow']
```
