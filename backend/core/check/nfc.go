// Package check has the checks of a descriptor (STS-MAL-3) and of the bytes that a selector reads.
package check

import (
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// IsNFC tells if bytes are valid UTF-8 in Normalization Form C. A selector of the profile
// `aavanam/nfc-text/v1` applies only to such bytes (ADP-SEL-4, CRD-SEL-4). The standard library has no
// normalization tables, so this check uses the module golang.org/x/text.
func IsNFC(data []byte) bool {
	return utf8.Valid(data) && norm.NFC.IsNormal(data)
}
