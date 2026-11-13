"""Write the two synthetic one-page PDF data sheets of the PDF example.

The data sheets are synthetic. They name no real product, company or person. Each file is a PDF 1.4
document in ASCII, with one page, the standard font Helvetica and an uncompressed content stream.
Version 2 changes only the flow rate. Run this file to write the two PDF files again:

    python3 examples/pdf_facts/make_pdf.py

tests/test_pdf_example.py checks that this script gives the same bytes as the committed files.
"""
from __future__ import annotations

import sys
from pathlib import Path

LINES = [
    'Sample Pump SP-100 Data Sheet',
    'Flow rate: {flow} L/min',
    'Maximum pressure: 6 bar',
    'Weight: 42 kg',
]
VERSIONS = {'datasheet-v1.pdf': 120, 'datasheet-v2.pdf': 135}


def pdf(flow: int) -> bytes:
    """Give the bytes of one data sheet with the given flow rate."""
    text = ''.join(f'BT /F1 12 Tf 72 {720 - 24 * n} Td ({line.format(flow=flow)}) Tj ET\n' for n, line in enumerate(LINES))
    stream = text.encode('ascii')
    objects = [
        b'<< /Type /Catalog /Pages 2 0 R >>',
        b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
        b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
        b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>',
        b'<< /Length %d >>\nstream\n' % len(stream) + stream + b'endstream',
    ]
    out = bytearray(b'%PDF-1.4\n')
    offsets = []
    for number, body in enumerate(objects, 1):
        offsets.append(len(out))
        out += b'%d 0 obj\n' % number + body + b'\nendobj\n'
    xref = len(out)
    out += b'xref\n0 %d\n0000000000 65535 f \n' % (len(objects) + 1)
    out += b''.join(b'%010d 00000 n \n' % offset for offset in offsets)
    out += b'trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n' % (len(objects) + 1, xref)
    return bytes(out)


def main() -> None:
    folder = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent
    for name, flow in VERSIONS.items():
        (folder / name).write_bytes(pdf(flow))


if __name__ == '__main__':
    main()
