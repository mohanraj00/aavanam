package ops

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
)

// Test tells if the value of a request field has the correct type.
type Test func(v jsontext.Value) bool

// Field is one field of a request form.
type Field struct {
	Name     string
	Required bool
	Test     Test
}

// Req returns a field that the request must have.
func Req(name string, test Test) Field { return Field{Name: name, Required: true, Test: test} }

// Opt returns a field that the request can have.
func Opt(name string, test Test) Field { return Field{Name: name, Test: test} }

func fieldOf(fields []Field, name string) (Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Form checks the fields of the request. A field that is not in the form, an absent required field and a
// value of an incorrect type each stop the operation with `code`.
func (c *Call) Form(code string, fields ...Field) {
	for _, m := range c.Request.Obj {
		if _, ok := fieldOf(fields, m.Key); !ok {
			Fail(code)
		}
	}
	for _, f := range fields {
		value, has := c.Request.Get(f.Name)
		if (f.Required && !has) || (has && !f.Test(value)) {
			Fail(code)
		}
	}
}

// Strict checks the form of the request in this order: `unknown_field`, then `field_count`, then
// `field_type` (PUB-CND-1, PUB-HED-2, RND-DOC-1). An absent required field gives `field_count`. A field in
// `many` has a count of 1..n, so an empty list gives `field_count` (KND-SCH-3). A field in `sets` with a
// duplicate gives `field_count` (KND-SCH-5).
func (c *Call) Strict(fields []Field, sets, many []string) {
	for _, m := range c.Request.Obj {
		if _, ok := fieldOf(fields, m.Key); !ok {
			Fail("unknown_field")
		}
	}
	for _, f := range fields {
		if f.Required && !c.Request.Has(f.Name) {
			Fail("field_count")
		}
	}
	for _, name := range many {
		if value, has := c.Request.Get(name); has && value.Kind == jsontext.Array && len(value.Arr) == 0 {
			Fail("field_count")
		}
	}
	for _, name := range sets {
		if value, _ := c.Request.Get(name); value.Kind == jsontext.Array && HasDuplicate(value.Arr) {
			Fail("field_count")
		}
	}
	for _, f := range fields {
		if value, has := c.Request.Get(f.Name); has && !f.Test(value) {
			Fail("field_type")
		}
	}
}

// Counted checks a set field of the request: an empty list (if `many`) or a duplicate gives `field_count`
// (KND-SCH-3, KND-SCH-5).
func (c *Call) Counted(field string, many bool) {
	value, _ := c.Request.Get(field)
	if value.Kind == jsontext.Array && ((many && len(value.Arr) == 0) || HasDuplicate(value.Arr)) {
		Fail("field_count")
	}
}

// HasDuplicate tells if a list has one value two times (KND-SCH-5).
func HasDuplicate(items []jsontext.Value) bool {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seen[string(canon.Ordered(item))] = struct{}{}
	}
	return len(seen) != len(items)
}

// IsStr tells if the value is a JSON string.
func IsStr(v jsontext.Value) bool { return v.Kind == jsontext.String }

// IsText tells if the value is a string that the store can keep in a descriptor, so it agrees with
// I-JSON (KND-SCH-4).
func IsText(v jsontext.Value) bool { return v.Kind == jsontext.String && jsontext.ValidString(v.Str) }

// IsRef tells if the value has the form of a reference or of a digest (IDN-DIG-1).
func IsRef(v jsontext.Value) bool { return v.Kind == jsontext.String && jsontext.IsDigest(v.Str) }

// IsBool tells if the value is true or false.
func IsBool(v jsontext.Value) bool { return v.Kind == jsontext.Bool }

// IsCount returns a test for an integer that is `least` or more.
func IsCount(least int64) Test {
	return func(v jsontext.Value) bool {
		n, ok := v.Int()
		return ok && n >= least
	}
}

// ListOf returns a test for a list whose values each pass a test. If `empty` is false, the list has one
// or more values.
func ListOf(test Test, empty bool) Test {
	return func(v jsontext.Value) bool {
		if v.Kind != jsontext.Array || (!empty && len(v.Arr) == 0) {
			return false
		}
		for _, x := range v.Arr {
			if !test(x) {
				return false
			}
		}
		return true
	}
}

// IsRefs tells if the value is a list of one or more references.
var IsRefs = ListOf(IsRef, false)

// IsNames tells if the value is a list of capability names (LCK-CAP-1).
var IsNames = ListOf(IsText, true)

// IsLabel tells if the value is a completeness label (LCK-CMPL-1).
func IsLabel(v jsontext.Value) bool { return v.Kind == jsontext.String && check.IsLabel(v.Str) }

// IsName returns a test for a string of 1 to `most` characters that the store can keep (PRD-SEC-3).
func IsName(most int) Test {
	return func(v jsontext.Value) bool {
		if !IsText(v) {
			return false
		}
		n := utf8.RuneCountInString(v.Str)
		return 1 <= n && n <= most
	}
}

// IsLog tells if the value is a log name: a list of 1 to 8 strings of 1 to 512 characters (PRD-SEC-3).
func IsLog(v jsontext.Value) bool {
	return v.Kind == jsontext.Array && 1 <= len(v.Arr) && len(v.Arr) <= 8 && ListOf(IsName(512), false)(v)
}

// IsB64 tells if the value is base64 text (RFC 4648 section 4).
func IsB64(v jsontext.Value) bool {
	_, ok := decode(v)
	return ok
}

func decode(v jsontext.Value) ([]byte, bool) {
	// The standard decoder ignores a line end. Base64 text of a request has none.
	if v.Kind != jsontext.String || strings.ContainsAny(v.Str, "\r\n") {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(v.Str)
	return data, err == nil
}

// Unb64 returns the bytes of base64 text of a request. A value that is not base64 text gives `field_type`.
func Unb64(v jsontext.Value) []byte {
	data, ok := decode(v)
	if !ok {
		Fail("field_type")
	}
	return data
}

// B64 returns bytes as base64 text.
func B64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

// Strings returns the strings of a list of strings.
func Strings(v jsontext.Value) []string {
	out := make([]string, len(v.Arr))
	for i, x := range v.Arr {
		out[i] = x.Str
	}
	return out
}
