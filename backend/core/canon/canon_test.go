package canon_test

import (
	"os"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
)

// testdata/fixtures.json has each descriptor of the fixture vectors with the canonical form and the
// revision of the reference implementation (testdata/golden.py). The bytes must be equal (IDN-CAN-1,
// IDN-CAN-2).
func TestFixtureDigestsAgreeWithTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	rows, formError := jsontext.Parse(data, jsontext.Strict)
	if formError != nil || len(rows.Arr) < 100 {
		t.Fatalf("testdata/fixtures.json: %v, %d rows", formError, len(rows.Arr))
	}
	for _, row := range rows.Arr {
		text, _ := row.Get("text")
		want, _ := row.Get("canonical")
		ref, _ := row.Get("ref")
		value, formError := jsontext.Parse([]byte(text.Str), jsontext.Strict)
		if formError != nil {
			t.Fatalf("%s: %v", ref.Str, formError)
		}
		desc, refusal := check.Normal(value)
		if refusal != nil {
			t.Fatalf("%s: %v", ref.Str, refusal)
		}
		if got := string(canon.Canonical(desc)); got != want.Str {
			t.Errorf("%s: the canonical form is different\n got  %s\n want %s", ref.Str, got, want.Str)
		}
		if got := canon.Revision(desc); got != ref.Str {
			t.Errorf("the revision is %s, not %s", got, ref.Str)
		}
	}
}

func TestCanonicalForm(t *testing.T) {
	cases := []struct{ text, want string }{
		// RFC 8785, section 3.2.3: the keys are in the order of their UTF-16 code units, so U+E000 comes
		// after U+1F600, whose first code unit is D83D. The order of the code points is the opposite.
		{`{"\ue000":1,"\ud83d\ude00":2,"b":3,"a":4,"":5,"aa":6}`, "{\"\":5,\"a\":4,\"aa\":6,\"b\":3,\"\U0001f600\":2,\"\ue000\":1}"},
		{`[1.0, 1e2, -0, 1E21, 0.000001, 1e-7, 100000000000000000000]`, `[1,100,0,1e+21,0.000001,1e-7,100000000000000000000]`},
		// Only the quotation mark, the reverse solidus and the control characters have an escape.
		{`["\u0000\u001f\b\f\n\r\t\"\\\/<>&\u2028\u007f\u00e9"]`, "[\"\\u0000\\u001f\\b\\f\\n\\r\\t\\\"\\\\/<>&\u2028\u007f\u00e9\"]"},
		{` { "b" : [ ] , "a" : { } , "c" : [ null , true , false ] } `, `{"a":{},"b":[],"c":[null,true,false]}`},
	}
	for _, c := range cases {
		value, err := jsontext.Parse([]byte(c.text), jsontext.Strict)
		if err != nil {
			t.Fatalf("%s: %v", c.text, err)
		}
		if got := string(canon.Canonical(value)); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.text, got, c.want)
		}
	}
	value, _ := jsontext.Parse([]byte(`{"b":1,"a":{"d":2,"c":3}}`), jsontext.Strict)
	if got := string(canon.Ordered(value)); got != `{"b":1,"a":{"d":2,"c":3}}` {
		t.Errorf("Ordered changed the order of the members: %s", got)
	}
	if got := canon.Revision(jsontext.O()); got != jsontext.Digest([]byte("{}")) {
		t.Errorf("the revision of an empty object: %s", got)
	}
}

// A string of a request can have a lone surrogate. The text of an envelope gives it with an escape.
func TestLoneSurrogateHasAnEscape(t *testing.T) {
	value, _ := jsontext.Parse([]byte(`"a\ud800b\udfff"`), jsontext.Lenient)
	if got := string(canon.Canonical(value)); got != `"a\ud800b\udfff"` {
		t.Errorf("got %s", got)
	}
	if got := string(canon.AppendString(nil, "a\xffb")); got != "\"a\ufffdb\"" {
		t.Errorf("invalid UTF-8: %s", got)
	}
}
