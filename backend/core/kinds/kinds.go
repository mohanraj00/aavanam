// Package kinds has the kind registry of the base profile (document 02): the schema of each kind, with the
// value type, the count and the target kinds of each field (KND-SCH-1, KND-REF-3).
package kinds

import "sort"

// BaseProfile is the identity of the base profile.
const BaseProfile = "aavanam/base/v1"

// ProfileMinor is the minor version of the base profile that this module implements (SPEC.md).
const ProfileMinor = 12

// Field is one row of the field table.
type Field struct {
	Name    string
	Type    string   // ref, ref-map, payload, string, json or selector
	Count   string   // 1, 0..1, 0..n, 1..n, 0..n list or 1..n list
	Targets []string // the kinds that a ref or ref-map field can name
}

// IsRef tells if the field has references. Only these fields give dependencies (KND-DEP-1).
func (f Field) IsRef() bool { return f.Type == "ref" || f.Type == "ref-map" }

// IsSet tells if the field is a set (KND-SCH-5). A count with `list` keeps its order.
func (f Field) IsSet() bool { return f.Count == "0..n" || f.Count == "1..n" }

// Many tells if the value of the field is a JSON list. The value of a ref-map field is one object (KND-SCH-6).
func (f Field) Many() bool { return f.Count != "1" && f.Count != "0..1" && f.Type != "ref-map" }

// Required tells if the count does not permit an absent field (KND-SCH-3).
func (f Field) Required() bool { return f.Count == "1" || f.Count == "1..n" || f.Count == "1..n list" }

// Permits tells if the field can name a target of the kind (KND-REF-3).
func (f Field) Permits(kind string) bool {
	for _, t := range f.Targets {
		if t == kind {
			return true
		}
	}
	return false
}

var schemas = func() map[string][]Field {
	out := make(map[string][]Field, len(own))
	for kind, fields := range own {
		out[kind] = append(append([]Field(nil), common...), fields...)
	}
	return out
}()

// Known tells if the base profile has the kind (KND-REG-3).
func Known(kind string) bool {
	_, ok := own[kind]
	return ok
}

// Schema returns the fields of a kind: the common fields, then the fields of the kind, in the order of the
// document. The caller does not change the result. An unknown kind gives nil.
func Schema(kind string) []Field { return schemas[kind] }

// Find returns one field of a kind.
func Find(kind, name string) (Field, bool) {
	for _, f := range schemas[kind] {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Names returns the kinds in ascending order.
func Names() []string {
	out := make([]string, 0, len(own))
	for kind := range own {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}
