package jsontext

import (
	"math"
	"strings"
	"testing"
)

func outcome(v Value, err *FormError) (string, int) {
	if err != nil {
		return err.Codes[0], len(err.Codes)
	}
	return "ok", 0
}

// Step 1 of STS-MAL-3: `malformed_json`, then `duplicate_key`, then `non_ijson_value`, with one code for
// each affected value.
func TestStrictForm(t *testing.T) {
	cases := []struct {
		text  string
		code  string
		count int
	}{
		{`{"a":1}`, "ok", 0},
		{` [ 1 , "x" , null , true , false , {} ] `, "ok", 0},
		{``, "malformed_json", 1},
		{`{"a":1,}`, "malformed_json", 1},
		{`[1] x`, "malformed_json", 1},
		{`{'a':1}`, "malformed_json", 1},
		{`[01]`, "malformed_json", 1},
		{`[1.]`, "malformed_json", 1},
		{`[+1]`, "malformed_json", 1},
		{`NaN`, "malformed_json", 1},
		{`[Infinity]`, "malformed_json", 1},
		{"[\"a\tb\"]", "malformed_json", 1},
		{`["\x"]`, "malformed_json", 1},
		{`["\u12"]`, "malformed_json", 1},
		{"\ufeff[]", "malformed_json", 1},
		{"[\"\xff\"]", "malformed_json", 1},         // invalid UTF-8
		{"[\"\xc3\"]", "malformed_json", 1},         // a cut sequence
		{"[\"\xed\xa0\x80\"]", "malformed_json", 1}, // surrogate bytes in the text
		{"[\"\xc0\xaf\"]", "malformed_json", 1},     // an overlong form
		{`{"a":1,"a":2}`, "duplicate_key", 1},
		{`{"a":1,"a":2,"a":3}`, "duplicate_key", 2},
		{`{"a":{"b":1,"b":2},"a":0}`, "duplicate_key", 2},
		{`{"\u0061":1,"a":2}`, "duplicate_key", 1},
		{`{"a":1,"a":2,"b":"\ud800"}`, "duplicate_key", 1}, // the first check that fails gives the codes
		{`["\ud800"]`, "non_ijson_value", 1},               // a lone high surrogate
		{`["\udc00\ud800"]`, "non_ijson_value", 1},         // a pair in the incorrect order is one string
		{`["\ud83d\ude00"]`, "ok", 0},                      // a surrogate pair is one code point
		{`{"\udfff":"\ud800"}`, "non_ijson_value", 2},
		{`["\ufffe"]`, "non_ijson_value", 1}, // a noncharacter (RFC 7493, section 2.1)
		{`["\ufdd0", "\ud83f\udfff"]`, "non_ijson_value", 2},
		{`[9007199254740993]`, "non_ijson_value", 1},
		{`[9007199254740993e0]`, "non_ijson_value", 1},
		{`[100000000000000000000, 1e20]`, "ok", 0},
		{`[1e400, -1e400]`, "non_ijson_value", 2},
		{`[0.1, 1.5e300, 5e-324, 0e999999999999999999999, -0.0]`, "ok", 0},
		{`[1e-400]`, "non_ijson_value", 1},
		{`[1.00000000000000011]`, "non_ijson_value", 1},
		{`[1e400, "\ud800", 9007199254740993]`, "non_ijson_value", 3},
	}
	for _, c := range cases {
		if code, count := outcome(Parse([]byte(c.text), Strict)); code != c.code || count != c.count {
			t.Errorf("%q: %s x %d, not %s x %d", c.text, code, count, c.code, c.count)
		}
	}
}

// A descriptor text of a request is a JSON string. A lone surrogate escape of the request stays visible in
// the text as three bytes, and it is a string outside I-JSON, not malformed JSON.
func TestInnerTextKeepsALoneSurrogate(t *testing.T) {
	request, err := Parse([]byte(`["{\"a\":\"\ud800\"}", "[\"\\ud800\"]", "{\"a\":1,\"a\":2}"]`), Lenient)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"non_ijson_value", "non_ijson_value", "duplicate_key"}
	for n, text := range request.Arr {
		if code, _ := outcome(ParseInner([]byte(text.Str), Strict)); code != want[n] {
			t.Errorf("text %d: %s, not %s", n, code, want[n])
		}
	}
	if code, _ := outcome(ParseInner([]byte("[\"\xff\"]"), Strict)); code != "malformed_json" {
		t.Errorf("invalid UTF-8 in an inner text: %s", code)
	}
}

// The reader has an explicit stack, so the depth of a text cannot use up the stack of the process.
func TestDepthNeedsNoRecursion(t *testing.T) {
	const depth = 1_000_000
	text := strings.Repeat("[", depth) + strings.Repeat("]", depth)
	v, err := Parse([]byte(text), Strict)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for ; len(v.Arr) == 1; v = v.Arr[0] {
		n++
	}
	if n != depth-1 || !IsIJSON(v) {
		t.Errorf("depth %d, not %d", n+1, depth)
	}
	text = strings.Repeat(`{"a":`, depth) + "1" + strings.Repeat("}", depth)
	if _, err := Parse([]byte(text), Strict); err != nil {
		t.Error(err)
	}
	if code, _ := outcome(Parse([]byte(strings.Repeat("[", depth)), Strict)); code != "malformed_json" {
		t.Errorf("a text with no end: %s", code)
	}
}

func TestValues(t *testing.T) {
	v, err := Parse([]byte(`{"n":1.0,"e":1e2,"z":-0,"s":"a\u0000\"\n","l":[null,true],"o":{"k":"v"}}`), Strict)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := v.Get("n")
	e, _ := v.Get("e")
	z, _ := v.Get("z")
	s, _ := v.Get("s")
	if x, ok := n.Int(); !ok || x != 1 || !e.IsInt() || e.Num != 100 || math.Signbit(z.Num) || s.Str != "a\x00\"\n" {
		t.Errorf("values: %+v %+v %+v %q", n, e, z, s.Str)
	}
	if got := strings.Join(v.Keys(), ","); got != "n,e,z,s,l,o" || v.Has("x") {
		t.Errorf("the keys are in the order of the text: %s", got)
	}
	if text, ok := s.Text(); !ok || text != s.Str {
		t.Error("Text")
	}
}

// A request is not a descriptor. The Lenient reader keeps a value outside I-JSON, and marks it.
func TestLenientKeepsValuesOutsideIJSON(t *testing.T) {
	v, err := Parse([]byte(`{"big":9007199254740993,"inf":1e400,"s":"\ud800","ok":[1,"a"]}`), Lenient)
	if err != nil {
		t.Fatal(err)
	}
	big, _ := v.Get("big")
	inf, _ := v.Get("inf")
	s, _ := v.Get("s")
	good, _ := v.Get("ok")
	if !big.Lossy || !inf.Lossy || ValidString(s.Str) || s.Str != "\xed\xa0\x80" {
		t.Errorf("lenient values: %+v %+v %q", big, inf, s.Str)
	}
	if _, ok := big.Int(); ok || IsIJSON(big) || IsIJSON(inf) || IsIJSON(s) || IsIJSON(v) || !IsIJSON(good) {
		t.Error("IsIJSON accepts a value outside I-JSON")
	}
	for _, text := range []string{`{"a":1,"a":2}`, `[1,]`, "[\"\xff\"]"} {
		if _, err := Parse([]byte(text), Lenient); err == nil {
			t.Errorf("the Lenient reader accepted %q", text)
		}
	}
}

func TestNumberForm(t *testing.T) {
	cases := []struct {
		x    float64
		want string
	}{
		{0, "0"}, {1, "1"}, {-1.5, "-1.5"}, {1e21, "1e+21"}, {1e20, "100000000000000000000"}, {1e-6, "0.000001"},
		{1e-7, "1e-7"}, {123456789012345680000, "123456789012345680000"}, {5e-324, "5e-324"},
		{1.7976931348623157e308, "1.7976931348623157e+308"}, {0.1, "0.1"}, {100, "100"}, {1.5e300, "1.5e+300"},
		{9007199254740992, "9007199254740992"}, {0.000123, "0.000123"}, {-1e-10, "-1e-10"},
	}
	for _, c := range cases {
		if got := ESNumber(c.x); got != c.want {
			t.Errorf("ESNumber(%g) = %s, not %s", c.x, got, c.want)
		}
	}
	for token, want := range map[string]bool{"1": true, "1.0": true, "9007199254740993": false, "1e400": false,
		"0.1": true, "0.10": true, "1e-400": false, "0e-400": true, "0.30000000000000004": true, "0.300000000000000041": false} {
		if _, kept := Kept(token); kept != want {
			t.Errorf("Kept(%s) = %v", token, kept)
		}
	}
}

func TestDigestForm(t *testing.T) {
	d := Digest([]byte("abc"))
	if d != "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || !IsDigest(d) {
		t.Errorf("Digest: %s", d)
	}
	for _, s := range []string{"", "sha256:", d[:70], d + "0", strings.ToUpper(d), "sha512:" + d[7:], d + "\n", strings.Replace(d, "b", "g", 1)} {
		if IsDigest(s) {
			t.Errorf("IsDigest accepts %q", s)
		}
	}
}
