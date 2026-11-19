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
