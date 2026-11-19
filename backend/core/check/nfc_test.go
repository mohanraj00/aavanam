package check

import "testing"

func TestIsNFC(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"empty", "", true},
		{"ASCII", "a reading", true},
		{"composed letter", "caf\u00e9", true},
		{"letter and combining mark", "cafe\u0301", false},
		{"Hangul syllable", "\uac00", true},
		{"Hangul jamo", "\u1100\u1161", false},
		{"marks in the canonical order", "a\u0323\u0307", false}, // U+1EA1 U+0307 is the composed form
		{"singleton", "\u212b", false},                           // ANGSTROM SIGN has the form U+00C5
		{"invalid UTF-8", "\xff", false},
		{"surrogate bytes", "\xed\xa0\x80", false},
		{"cut sequence", "\xc3", false},
	}
	for _, c := range cases {
		if got := IsNFC([]byte(c.data)); got != c.want {
			t.Errorf("%s: IsNFC gives %v, not %v", c.name, got, c.want)
		}
	}
}
