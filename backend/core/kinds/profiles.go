// The tables of the profiles that a store declares separately from the base profile: the layer profile
// (document 23) and the retrieval index profile (document 24). The test of this package compares them with
// the tables of the documents.
package kinds

import "sort"

// The profiles that a store can declare (LYR-PRF-1, IDX-PRF-1).
const (
	LayerProfile = "aavanam/layer/v1"
	IndexProfile = "aavanam/semantic-index/v1"
)

// HashedTerms is the method that each store with the retrieval index profile supports (IDX-MTH-1).
const HashedTerms = "aavanam/hashed-terms/v1"

// Profiles is the set of the profiles that a store declares. The base profile is not in the set. A nil
// set is a store of the base profile only.
type Profiles map[string]bool

// DeclaredProfiles returns the set of the given profiles. A name that is not a profile of this module
// gives false.
func DeclaredProfiles(names ...string) (Profiles, bool) {
	out := Profiles{}
	for _, name := range names {
		if _, ok := added[name]; !ok {
			return nil, false
		}
		out[name] = true
	}
	return out, true
}

// Names returns the profiles of the set in ascending order.
func (p Profiles) Names() []string {
	out := make([]string, 0, len(p))
	for name := range p {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// With returns the set with one more profile.
func (p Profiles) With(name string) Profiles {
	out := Profiles{name: true}
	for other := range p {
		out[other] = true
	}
	return out
}

// has tells if the set has the profile of a kind or of a field. The base profile is the empty name.
func (p Profiles) has(profile string) bool { return profile == "" || p[profile] }

// profileTable has the kinds and the fields that one profile adds (KND-REG-2).
type profileTable struct {
	// kinds has the fields of each kind that the profile registers, in the order of the document.
	kinds map[string][]Field
	// fields has the fields that the profile adds to a kind of a different profile.
	fields map[string][]Field
}

// members has the member fields of a snapshot, which a layer also has (LYR-DEF-1).
var members = []Field{
	{Name: "units", Type: "ref", Count: "0..n", Targets: []string{"knowledge-unit"}},
	{Name: "schemas", Type: "ref", Count: "0..n", Targets: []string{"schema"}},
	{Name: "relation_sets", Type: "ref", Count: "0..n", Targets: []string{"relation-set"}},
	{Name: "overlay_sets", Type: "ref", Count: "0..n", Targets: []string{"overlay-set"}},
	{Name: "observations", Type: "ref", Count: "0..n", Targets: []string{"observation"}},
	{Name: "native_mappings", Type: "ref", Count: "0..n", Targets: []string{"native-mapping"}},
}

// added has the tables of each profile (LYR-PRF-2, IDX-PRF-2).
var added = map[string]profileTable{
	LayerProfile: {
		kinds: map[string][]Field{
			"layer": append([]Field{
				{Name: "base", Type: "string", Count: "1"},
				{Name: "scope", Type: "string", Count: "1"},
			}, members...),
		},
		fields: map[string][]Field{
			"execution-lock": {{Name: "view", Type: "ref", Count: "0..n", Targets: []string{"snapshot", "layer"}}},
			"build-lock":     {{Name: "view", Type: "ref", Count: "0..n", Targets: []string{"snapshot", "layer"}}},
		},
	},
	IndexProfile: {
		kinds: map[string][]Field{
			"retrieval-index": {
				{Name: "snapshot", Type: "ref", Count: "1", Targets: []string{"snapshot"}},
				{Name: "method", Type: "string", Count: "1"},
				{Name: "parameters", Type: "json", Count: "1"},
			},
		},
		fields: map[string][]Field{
			"execution-lock": {{Name: "indexes", Type: "ref", Count: "0..n", Targets: []string{"retrieval-index"}}},
			"build-lock":     {{Name: "indexes", Type: "ref", Count: "0..n", Targets: []string{"retrieval-index"}}},
		},
	},
}

// kindProfile has the profile of each kind that a profile registers.
var kindProfile = func() map[string]string {
	out := map[string]string{}
	for profile, table := range added {
		for kind := range table.kinds {
			out[kind] = profile
		}
	}
	return out
}()

// tagged returns the fields with the profile that adds them.
func tagged(fields []Field, profile string) []Field {
	out := make([]Field, len(fields))
	for i, f := range fields {
		f.Profile = profile
		out[i] = f
	}
	return out
}

// Registered tells if the kind is a kind of the base profile or of a declared profile (KND-REG-2,
// KND-REG-3, LYR-PRF-1, IDX-PRF-1).
func Registered(kind string, declared Profiles) bool {
	profile, ok := kindProfile[kind]
	if ok {
		return declared[profile]
	}
	return Known(kind)
}

// SchemaIn returns the fields of a kind for a store that declares the profiles: the schema without each
// field of a profile that is not declared (KND-SCH-2). An unregistered kind gives nil.
func SchemaIn(kind string, declared Profiles) []Field {
	if !Registered(kind, declared) {
		return nil
	}
	all := schemas[kind]
	for _, f := range all {
		if !declared.has(f.Profile) {
			out := make([]Field, 0, len(all))
			for _, g := range all {
				if declared.has(g.Profile) {
					out = append(out, g)
				}
			}
			return out
		}
	}
	return all
}
