package kinds

import (
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/internal/doctable"
)

func cells(t *testing.T, document, marker string) [][]string {
	t.Helper()
	rows, err := doctable.Rows(document, marker)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

var names = doctable.Names

// The table of this package is the field table of document 02, with each target group expanded.
func TestTableAgreesWithDocument02(t *testing.T) {
	groups := map[string][]string{}
	for _, row := range cells(t, "02", "The target groups:") {
		groups[strings.Trim(row[0], "`")] = names(row[1])
	}
	var commonRows []Field
	for _, row := range cells(t, "02", "The common fields:") {
		commonRows = append(commonRows, Field{Name: names(row[0])[0], Type: names(row[1])[0], Count: names(row[2])[0]})
	}
	if !reflect.DeepEqual(commonRows, common) {
		t.Errorf("the common fields are %v, and the document has %v", common, commonRows)
	}
	want := map[string][]Field{}
	for _, row := range cells(t, "02", "The field table.") {
		var targets []string
		for _, name := range names(row[4]) {
			if group, ok := groups[name]; ok {
				targets = append(targets, group...)
			} else {
				targets = append(targets, name)
			}
		}
		sort.Strings(targets)
		kind := names(row[0])[0]
		want[kind] = append(want[kind], Field{Name: names(row[1])[0], Type: names(row[2])[0], Count: names(row[3])[0], Targets: targets})
	}
	var registry []string
	for _, row := range cells(t, "02", "| Kind | Purpose | Document |") {
		registry = append(registry, names(row[0])[0])
	}
	sort.Strings(registry)
	if !reflect.DeepEqual(registry, Names()) {
		t.Errorf("the kinds are %v, and the registry of the document has %v", Names(), registry)
	}
	for kind, fields := range own {
		got := make([]Field, len(fields))
		for i, f := range fields {
			got[i] = f
			got[i].Targets = append([]string(nil), f.Targets...)
			sort.Strings(got[i].Targets)
		}
		if !reflect.DeepEqual(got, want[kind]) {
			t.Errorf("kind %s:\n table    %v\n document %v", kind, got, want[kind])
		}
	}
	if len(want) != len(own) {
		t.Errorf("the table has %d kinds, and the document has %d", len(own), len(want))
	}
}

func TestFieldRules(t *testing.T) {
	f, ok := Find("binding-set", "bindings")
	if !ok || !f.IsRef() || f.Many() || f.Required() || !f.Permits("schema") || f.Permits("source") {
		t.Errorf("binding-set.bindings: %+v", f)
	}
	f, _ = Find("derivation", "operands")
	if !f.Many() || f.IsSet() || !f.Required() {
		t.Errorf("derivation.operands: %+v", f)
	}
	f, _ = Find("snapshot", "units")
	if !f.Many() || !f.IsSet() || f.Required() {
		t.Errorf("snapshot.units: %+v", f)
	}
	if Known("workspace") || Schema("workspace") != nil || !Known("activation") {
		t.Error("the known kinds are not correct")
	}
	if Schema("source")[0].Name != "descriptor_profile" || Schema("source")[4].Name != "payload" {
		t.Errorf("the schema of source: %v", Schema("source"))
	}
}

func TestProfileMinorAgreesWithTheSpec(t *testing.T) {
	spec, err := doctable.Spec()
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`The base profile is at minor version (\d+)\.`).FindStringSubmatch(spec)
	if found == nil || found[1] != strconv.Itoa(ProfileMinor) {
		t.Errorf("ProfileMinor is %d, and SPEC.md has %v", ProfileMinor, found)
	}
}

// profileFields returns the rows of the field table of a profile document for each kind, in the order of
// the document, with the targets in ascending order.
func profileFields(t *testing.T, document string) map[string][]Field {
	out := map[string][]Field{}
	for _, row := range cells(t, document, "The field table of the profile:") {
		targets := names(row[4])
		sort.Strings(targets)
		kind := names(row[0])[0]
		out[kind] = append(out[kind], Field{Name: names(row[1])[0], Type: names(row[2])[0], Count: names(row[3])[0], Targets: targets})
	}
	return out
}

// The tables of each profile are the kind table and the field table of its document (LYR-PRF-2, IDX-PRF-2).
func TestProfileTablesAgreeWithDocuments23And24(t *testing.T) {
	for profile, document := range map[string]string{LayerProfile: "23", IndexProfile: "24"} {
		table := added[profile]
		var registry, got []string
		for _, row := range cells(t, document, "The kind table of the profile:") {
			registry = append(registry, names(row[0])[0])
			if names(row[2])[0] != document {
				t.Errorf("%s: the kind row %v names a different document", profile, row)
			}
		}
		for kind := range table.kinds {
			got = append(got, kind)
		}
		sort.Strings(registry)
		sort.Strings(got)
		if !reflect.DeepEqual(got, registry) {
			t.Errorf("%s: the kinds are %v, and the document has %v", profile, got, registry)
		}
		want := profileFields(t, document)
		rows := 0
		for _, part := range []map[string][]Field{table.kinds, table.fields} {
			for kind, fields := range part {
				sorted := make([]Field, len(fields))
				for i, f := range fields {
					sorted[i] = f
					sorted[i].Targets = append([]string(nil), f.Targets...)
					sort.Strings(sorted[i].Targets)
				}
				if !reflect.DeepEqual(sorted, want[kind]) {
					t.Errorf("%s, kind %s:\n table    %v\n document %v", profile, kind, sorted, want[kind])
				}
				rows++
			}
		}
		if rows != len(want) {
			t.Errorf("%s: the table has fields for %d kinds, and the document has %d", profile, rows, len(want))
		}
		for kind := range table.kinds {
			if Known(kind) || groupHas(t, kind) {
				t.Errorf("%s: the kind %s is a kind of the base profile or is in a target group", profile, kind)
			}
		}
	}
}

// groupHas tells if a target group of document 02 has the kind. A kind of a profile is in no target group.
func groupHas(t *testing.T, kind string) bool {
	for _, row := range cells(t, "02", "The target groups:") {
		for _, name := range names(row[1]) {
			if name == kind {
				return true
			}
		}
	}
	return false
}

// A store has a kind or a field of a profile only if it declares the profile (LYR-PRF-1, IDX-PRF-1).
func TestProfileDeclaration(t *testing.T) {
	layers, ok := DeclaredProfiles(LayerProfile)
	if !ok || !reflect.DeepEqual(layers.Names(), []string{LayerProfile}) {
		t.Fatalf("the declared profiles: %v", layers)
	}
	if _, ok := DeclaredProfiles(LayerProfile, "aavanam/workspace/v1"); ok {
		t.Error("a profile that this module does not have was declared")
	}
	both := layers.With(IndexProfile)
	if len(layers) != 1 || !reflect.DeepEqual(both.Names(), []string{LayerProfile, IndexProfile}) {
		t.Errorf("With changed its set or gave %v", both.Names())
	}
	has := func(kind, name string, declared Profiles) bool {
		for _, f := range SchemaIn(kind, declared) {
			if f.Name == name {
				return true
			}
		}
		return false
	}
	cases := []struct {
		kind, field string
		declared    Profiles
		want        bool
	}{
		{"layer", "scope", nil, false},
		{"layer", "scope", layers, true},
		{"retrieval-index", "method", layers, false},
		{"retrieval-index", "method", both, true},
		{"build-lock", "inputs", nil, true},
		{"build-lock", "view", nil, false},
		{"build-lock", "view", layers, true},
		{"build-lock", "indexes", layers, false},
		{"execution-lock", "indexes", both, true},
		{"execution-lock", "view", Profiles{IndexProfile: true}, false},
	}
	for _, c := range cases {
		if got := has(c.kind, c.field, c.declared); got != c.want {
			t.Errorf("%s.%s with %v: %v, not %v", c.kind, c.field, c.declared.Names(), got, c.want)
		}
	}
	if Registered("layer", nil) || !Registered("layer", layers) || Registered("retrieval-index", layers) ||
		!Registered("source", nil) || Registered("workspace", both) || Known("layer") {
		t.Error("the registered kinds are not correct")
	}
	// A reader of a stored descriptor has each field, with no declaration.
	if f, ok := Find("layer", "units"); !ok || !f.IsRef() || f.Profile != LayerProfile || Schema("layer")[0].Name != "descriptor_profile" {
		t.Errorf("the schema of layer: %v", Schema("layer"))
	}
	if f, ok := Find("execution-lock", "view"); !ok || !f.Permits("layer") || !f.Permits("snapshot") || f.Permits("source") {
		t.Errorf("execution-lock.view: %+v", f)
	}
}
