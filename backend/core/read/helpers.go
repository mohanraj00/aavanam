package read

import (
	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// The profiles of the base profile that the read operations name.
const (
	CoverageProfile = "aavanam/coverage/v1" // OBS-COV-1
	RenderProfile   = "aavanam/render/v1"   // RND-DOC-1
	Row             = "aavanam/row/v1"      // CRD-SEL-1
	Table           = "aavanam/table/v1"    // DAT-TBL-1
	Lexical         = "aavanam/lexical/v1"  // QRY-LEX-1
)

// field returns the value of a field of an object, or null.
func field(v jsontext.Value, name string) jsontext.Value {
	out, _ := v.Get(name)
	return out
}

// str returns the string of a field of an object, or "".
func str(v jsontext.Value, name string) string { return field(v, name).Str }

// has tells if a list of strings has the string.
func has(list jsontext.Value, s string) bool {
	for _, x := range list.Arr {
		if x.Kind == jsontext.String && x.Str == s {
			return true
		}
	}
	return false
}

// same tells if two values have the same canonical form.
func same(a, b jsontext.Value) bool { return string(canon.Canonical(a)) == string(canon.Canonical(b)) }

// Record is one object of a read context and its descriptor.
type Record struct {
	Ref  string
	Desc jsontext.Value
}

// Records returns the objects of the list that have one of the kinds, in the order of the list.
func Records(c *ops.Call, refs []string, kinds ...string) []Record {
	var out []Record
	for _, ref := range refs {
		desc, ok := c.Descriptor(ref)
		if !ok {
			continue
		}
		kind := check.Kind(desc)
		for _, k := range kinds {
			if kind == k {
				out = append(out, Record{ref, desc})
				break
			}
		}
	}
	return out
}

// item returns an `ok` item with the fields.
func item(fields ...jsontext.Member) envelope.Item { return envelope.OK(fields...) }

// m returns one member.
func m(key string, val jsontext.Value) jsontext.Member { return jsontext.M(key, val) }

// num returns a JSON number.
func num(n int64) jsontext.Value { return jsontext.N(float64(n)) }

// nullable returns a string value, or null for "".
func nullable(s string) jsontext.Value {
	if s == "" {
		return jsontext.Value{}
	}
	return jsontext.S(s)
}

// refusedRef returns the item of a reference with one reason, and the field `ref` (QRY-BDL-2, CHG-IMP-2).
func refusedRef(code, ref string) envelope.Item {
	out := envelope.Refused([]string{code}, ref)
	out.Fields = []jsontext.Member{m("ref", jsontext.S(ref))}
	return out
}

// Located returns one item for each reference that is not in the read context (QRY-BDL-2, CHG-IMP-2).
func Located(rc *ops.ReadContext, refs []string) []envelope.Item {
	var out []envelope.Item
	for _, ref := range refs {
		if code := rc.Locate(ref); code != "" {
			out = append(out, refusedRef(code, ref))
		}
	}
	return out
}

// repeat returns n reasons with one code (one reason for each affected value).
func repeated(code string, n int, ref string) envelope.Item {
	return envelope.Refused(repeat(code, n), ref)
}

// window returns the offset and the limit of a request. A request with no limit gives -1.
func window(c *ops.Call) (offset, limit int64) {
	offset, limit = 0, -1
	if v, ok := c.Request.Get("offset"); ok {
		offset, _ = v.Int()
	}
	if v, ok := c.Request.Get("limit"); ok {
		limit, _ = v.Int()
	}
	return offset, limit
}

// Page returns the entries from `offset`, at most `limit`, and tells if a declared bound stopped the
// operation (STS-ENV-3).
func Page[T any](c *ops.Call, entries []T) ([]T, bool) {
	offset, limit := window(c)
	if offset > int64(len(entries)) {
		offset = int64(len(entries))
	}
	rest := entries[offset:]
	if limit < 0 || int64(len(rest)) <= limit {
		return rest, false
	}
	return rest[:limit], true
}

// result returns the envelope of the items in the read context, with `bounded` from a declared bound.
func result(c *ops.Call, rc *ops.ReadContext, items []envelope.Item, bounded bool) envelope.Envelope {
	var out envelope.Envelope
	if bounded {
		out = c.Bounded(items)
	} else {
		out = c.Result(items)
	}
	if rc != nil {
		out = out.WithContext(rc.ID)
	}
	return out
}

// isCount tells if a value is an integer that is `least` or more.
func isCount(v jsontext.Value, least int64) bool {
	n, ok := v.Int()
	return ok && n >= least
}

// isNonEmpty tells if a value is a string with one or more characters.
func isNonEmpty(v jsontext.Value) bool { return v.Kind == jsontext.String && v.Str != "" }

// isStrings tells if a value is a list of strings.
var isStrings = ops.ListOf(ops.IsStr, true)

// canonOf returns the canonical form of a value.
func canonOf(v jsontext.Value) []byte { return canon.Canonical(v) }
