package check

import (
	"regexp"
	"sort"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
)

// The selector profiles of the base profile (REL-SEL-3, ADP-SEL-4).
const (
	Bytes   = "aavanam/bytes/v1"
	NFCText = "aavanam/nfc-text/v1"
)

var label = regexp.MustCompile(`^[a-z0-9.-]+/[a-z0-9.-]+/v[1-9][0-9]*\z`)

// IsLabel tells if a string is a completeness label (LCK-CMPL-1).
func IsLabel(s string) bool { return label.MatchString(s) }

// Refusal is a check that fails. Codes has one code for each affected value, and each code is the same.
// Refs names the affected references.
type Refusal struct {
	Codes []string
	Refs  []string
}

func (r *Refusal) Error() string { return r.Codes[0] }

func refuse(code string, n int, refs ...string) *Refusal {
	codes := make([]string, n)
	for i := range codes {
		codes[i] = code
	}
	return &Refusal{Codes: codes, Refs: refs}
}

func isInt(v jsontext.Value) bool { return v.IsInt() && !v.Lossy }

// only tells if an object has each of the keys and no other key.
func only(v jsontext.Value, keys ...string) bool {
	if v.Kind != jsontext.Object || len(v.Obj) != len(keys) {
		return false
	}
	for _, key := range keys {
		if !v.Has(key) {
			return false
		}
	}
	return true
}

// IsSelector checks the form of a selector (REL-SEL-1, REL-SEL-3, ADP-SEL-4). A selector of a profile that
// the base profile does not have keeps its form: the operation that applies it gives `unsupported_selector`.
func IsSelector(v jsontext.Value) bool {
	profile, _ := v.Get("selector_profile")
	if v.Kind != jsontext.Object || profile.Kind != jsontext.String {
		return false
	}
	if profile.Str != Bytes && profile.Str != NFCText {
		return true
	}
	start, _ := v.Get("start")
	end, _ := v.Get("end")
	return only(v, "selector_profile", "start", "end") && isInt(start) && isInt(end) && 0 <= start.Num && start.Num <= end.Num
}

// IsPayload checks the form of a payload object (KND-SCH-1).
func IsPayload(v jsontext.Value) bool {
	digest, _ := v.Get("digest")
	media, _ := v.Get("media_type")
	size, _ := v.Get("size")
	return only(v, "digest", "media_type", "size") && digest.Kind == jsontext.String && jsontext.IsDigest(digest.Str) &&
		media.Kind == jsontext.String && isInt(size) && size.Num >= 0
}

// IsRanges checks the ranges of an excerpt: one or more ranges in ascending order with no overlap
// (BUN-EXC-1, BUN-EXC-2).
func IsRanges(v jsontext.Value) bool {
	if v.Kind != jsontext.Array || len(v.Arr) == 0 {
		return false
	}
	last := 0.0
	for _, r := range v.Arr {
		start, _ := r.Get("start")
		end, _ := r.Get("end")
		if !only(r, "start", "end") || !isInt(start) || !isInt(end) || last > start.Num || start.Num > end.Num {
			return false
		}
		last = end.Num
	}
	return true
}

func agrees(f kinds.Field, v jsontext.Value) bool {
	switch f.Type {
	case "ref":
		return v.Kind == jsontext.String && jsontext.IsDigest(v.Str)
	case "ref-map":
		if v.Kind != jsontext.Object {
			return false
		}
		for _, m := range v.Obj {
			if !jsontext.ValidString(m.Key) || m.Val.Kind != jsontext.String || !jsontext.IsDigest(m.Val.Str) {
				return false
			}
		}
		return true
	case "payload":
		return IsPayload(v)
	case "string":
		if v.Kind != jsontext.String {
			return false
		}
		if f.Name == "completeness" {
			return IsLabel(v.Str) // LCK-CMPL-1
		}
		return jsontext.ValidString(v.Str) && (f.Name != "logical_id" || v.Str != "") // IDN-LOG-1
	case "selector":
		return IsSelector(v) || (f.Name == "operand_selectors" && v.Kind == jsontext.Null) // KND-SCH-7
	}
	return jsontext.IsIJSON(v) // json: a value that is not I-JSON is not of the type (KND-SCH-4)
}

// IsDimensions checks the parameters of the method `aavanam/hashed-terms/v1`: an object with only the field
// `dimensions`, an integer from 1 to 65536 (IDX-MTH-1).
func IsDimensions(v jsontext.Value) bool {
	n, _ := v.Get("dimensions")
	return only(v, "dimensions") && isInt(n) && 1 <= n.Num && n.Num <= 65536
}

// Normal returns the normal form of a descriptor value for a store of the base profile only. NormalIn has
// the rules.
func Normal(v jsontext.Value) (jsontext.Value, *Refusal) { return NormalIn(v, nil) }

// NormalIn returns the normal form of a descriptor value, or the refusal of step 2 or step 3 of STS-MAL-3.
// The normal form has the fields in the order of the schema, no empty list of a field that can be absent
// (KND-SCH-3), and each set of strings in ascending order (KND-SCH-5). A kind or a field of a profile that
// is not in `declared` is not registered (KND-REG-2, LYR-PRF-1, IDX-PRF-1).
func NormalIn(v jsontext.Value, declared kinds.Profiles) (jsontext.Value, *Refusal) {
	if v.Kind != jsontext.Object {
		return jsontext.Value{}, refuse("field_type", 1)
	}
	kind, has := v.Get("kind")
	switch {
	case !has:
		return jsontext.Value{}, refuse("field_count", 1)
	case kind.Kind != jsontext.String:
		return jsontext.Value{}, refuse("field_type", 1)
	case !kinds.Registered(kind.Str, declared):
		return jsontext.Value{}, refuse("unknown_kind", 1) // KND-REG-3
	}
	schema := kinds.SchemaIn(kind.Str, declared)
	unknown := 0
	for _, m := range v.Obj {
		known := false
		for _, f := range schema {
			known = known || f.Name == m.Key
		}
		if !known {
			unknown++
		}
	}
	if unknown > 0 {
		return jsontext.Value{}, refuse("unknown_field", unknown) // KND-SCH-2
	}
	var normal []jsontext.Member
	counts, types := 0, 0
	for _, f := range schema {
		value, has := v.Get(f.Name)
		if !has {
			if f.Required() {
				counts++
			}
			continue
		}
		if !f.Many() {
			if !agrees(f, value) || (f.Name == "ranges" && kind.Str == "excerpt" && !IsRanges(value)) {
				types++
			}
			normal = append(normal, jsontext.M(f.Name, value))
			continue
		}
		if value.Kind != jsontext.Array {
			types++
			continue
		}
		if len(value.Arr) == 0 {
			if f.Required() {
				counts++
			}
			continue // an empty list and an absent field are the same value (KND-SCH-3)
		}
		strings := true
		seen := make(map[string]struct{}, len(value.Arr))
		for _, x := range value.Arr {
			if !agrees(f, x) {
				types++
			}
			strings = strings && x.Kind == jsontext.String
			seen[string(canon.Ordered(x))] = struct{}{}
		}
		items := value.Arr
		if f.IsSet() {
			if len(seen) != len(items) {
				counts++ // a set has no duplicates (KND-SCH-5)
			}
			if strings {
				items = append([]jsontext.Value(nil), items...)
				sort.Slice(items, func(i, j int) bool { return items[i].Str < items[j].Str })
			}
		}
		normal = append(normal, jsontext.M(f.Name, jsontext.A(items)))
	}
	out := jsontext.O(normal...)
	selectors, has := out.Get("operand_selectors")
	operands, _ := out.Get("operands")
	if kind.Str == "derivation" && has && operands.Kind == jsontext.Array && len(selectors.Arr) != len(operands.Arr) {
		counts++ // KND-SCH-7
	}
	if kind.Str == "retrieval-index" {
		method, _ := out.Get("method")
		parameters, has := out.Get("parameters")
		// A value of `parameters` that is not I-JSON has its reason from the loop.
		if method.Kind == jsontext.String && method.Str == kinds.HashedTerms && has && jsontext.IsIJSON(parameters) && !IsDimensions(parameters) {
			types++ // IDX-MTH-1
		}
	}
	switch {
	case counts > 0:
		return jsontext.Value{}, refuse("field_count", counts)
	case types > 0:
		return jsontext.Value{}, refuse("field_type", types)
	}
	return out, nil
}

// Kind returns the kind of a normal descriptor.
func Kind(desc jsontext.Value) string {
	kind, _ := desc.Get("kind")
	return kind.Str
}

// Reference is one reference of a descriptor and the field that has it.
type Reference struct {
	Field string
	Ref   string
}

// References returns each reference of a normal descriptor, in the order of the schema. These references
// are the only source of dependencies (KND-DEP-1).
func References(desc jsontext.Value) []Reference {
	var out []Reference
	for _, f := range kinds.Schema(Kind(desc)) {
		value, has := desc.Get(f.Name)
		if !f.IsRef() || !has {
			continue
		}
		switch {
		case f.Type == "ref-map":
			for _, m := range value.Obj {
				out = append(out, Reference{f.Name, m.Val.Str})
			}
		case value.Kind == jsontext.Array:
			for _, x := range value.Arr {
				out = append(out, Reference{f.Name, x.Str})
			}
		default:
			out = append(out, Reference{f.Name, value.Str})
		}
	}
	return out
}

// Targets returns the different references of a normal descriptor, in ascending order: its `refs` (KND-DEP-2).
func Targets(desc jsontext.Value) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, r := range References(desc) {
		if _, ok := seen[r.Ref]; !ok {
			seen[r.Ref] = struct{}{}
			out = append(out, r.Ref)
		}
	}
	sort.Strings(out)
	return out
}

// Payload is one payload object of a descriptor.
type Payload struct {
	Digest    string
	MediaType string
	Size      int64
}

// PayloadOf returns the payload object of a field of a normal descriptor.
func PayloadOf(desc jsontext.Value, field string) (Payload, bool) {
	value, has := desc.Get(field)
	if !has {
		return Payload{}, false
	}
	digest, _ := value.Get("digest")
	media, _ := value.Get("media_type")
	size, _ := value.Get("size")
	return Payload{Digest: digest.Str, MediaType: media.Str, Size: int64(size.Num)}, true
}

// Payloads returns the payload objects of a normal descriptor, in the order of the schema (KND-DEP-3).
func Payloads(desc jsontext.Value) []Payload {
	var out []Payload
	for _, f := range kinds.Schema(Kind(desc)) {
		if f.Type != "payload" {
			continue
		}
		if p, ok := PayloadOf(desc, f.Name); ok {
			out = append(out, p)
		}
	}
	return out
}
