package envelope

import (
	"reflect"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/internal/doctable"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
)

func TestReasonTableAgreesWithDocument03(t *testing.T) {
	rows, err := doctable.Rows("03", "The reason table:")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, row := range rows {
		want[doctable.Names(row[0])[0]] = doctable.Names(row[1])[0]
	}
	if !reflect.DeepEqual(reasonStatus, want) {
		t.Errorf("the reason table is different from the document:\n code     %v\n document %v", reasonStatus, want)
	}
}

func TestEnvelopeText(t *testing.T) {
	e := New(jsontext.Value{}, "unsupported", R("unsupported_operation"))
	want := `{"context":null,"operation":null,"operation_profile":"aavanam/base/v1","reasons":[{"capabilities":[],"code":"unsupported_operation","refs":[]}],"status":"unsupported"}`
	if got := string(e.JSON()); got != want {
		t.Errorf("an envelope of STS-MAL-4:\n got  %s\n want %s", got, want)
	}
	e = New(jsontext.S("get"), "ok").WithItems(nil).WithContext("ctx-\xed\xa0\x80")
	want = `{"context":"ctx-\ud800","items":[],"operation":"get","operation_profile":"aavanam/base/v1","reasons":[],"status":"ok"}`
	if got := string(e.JSON()); got != want {
		t.Errorf("an envelope with a context:\n got  %s\n want %s", got, want)
	}
	item := Refused([]string{"unknown_ref"}, "sha256:aa")
	want = `{"reasons":[{"capabilities":[],"code":"unknown_ref","refs":["sha256:aa"]}],"status":"not_found"}`
	if got := string(New(jsontext.S("get"), "ok").WithItems([]Item{item}).JSON()); !contains(got, want) {
		t.Errorf("an item:\n got  %s\n want %s", got, want)
	}
}

func contains(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

// Rules 4 to 6 of STS-ENV-3.
func TestSummary(t *testing.T) {
	ok, missing, bad := Ref("sha256:aa"), Refused([]string{"unknown_ref"}, "r1"), Refused([]string{"field_type", "field_type"})
	cases := []struct {
		name    string
		items   []Item
		partial bool
		status  string
		codes   []string
	}{
		{"no item", nil, false, "ok", nil},
		{"each item ok", []Item{ok, ok}, true, "ok", nil},
		{"partial permitted", []Item{ok, missing}, true, "partial", []string{"items_not_ok"}},
		{"partial not permitted", []Item{ok, missing}, false, "not_found", []string{"unknown_ref"}},
		{"no ok item", []Item{missing, missing}, true, "not_found", []string{"unknown_ref", "unknown_ref"}},
		{"first status in the order", []Item{missing, bad, missing}, false, "failed", []string{"field_type", "field_type"}},
	}
	for _, c := range cases {
		status, reasons := Summary(c.items, c.partial)
		var codes []string
		for _, r := range reasons {
			codes = append(codes, r.Code)
		}
		if status != c.status || !reflect.DeepEqual(codes, c.codes) {
			t.Errorf("%s: %s %v, not %s %v", c.name, status, codes, c.status, c.codes)
		}
	}
}

func TestJoinedAndFailure(t *testing.T) {
	item := Joined([]Item{Refused([]string{"unknown_ref"}, "a"), Refused([]string{"field_type"}), Refused([]string{"unknown_ref"}, "b")})
	if item.Status != "failed" || len(item.Reasons) != 1 || item.Reasons[0].Code != "field_type" {
		t.Errorf("joined: %+v", item)
	}
	item = Joined([]Item{Refused([]string{"unknown_ref"}, "a"), Refused([]string{"not_in_context"}, "c"), Refused([]string{"unknown_ref"}, "b")})
	want := []Reason{{Code: "unknown_ref", Refs: []string{"a", "b"}}, {Code: "not_in_context", Refs: []string{"c"}}}
	if item.Status != "not_found" || !reflect.DeepEqual(item.Reasons, want) {
		t.Errorf("joined: %+v", item)
	}
	f := FailRefs([]string{"r"}, "read_only_store", "field_type")
	if f.Status != "unsupported" || len(f.Reasons) != 2 || f.Reasons[1].Refs[0] != "r" || f.Error() != "unsupported: read_only_store" {
		t.Errorf("failure: %+v", f)
	}
	defer func() {
		if recover() == nil {
			t.Error("a code that is not in the reason table did not panic")
		}
	}()
	StatusOf("no_such_code")
}

// The reason codes of each profile are the reason table of its document (LYR-PRF-3, IDX-PRF-3).
func TestProfileReasonTablesAgreeWithDocuments23And24(t *testing.T) {
	documents := map[string]string{kinds.LayerProfile: "23", kinds.IndexProfile: "24"}
	if len(profileReasons) != len(documents) {
		t.Errorf("the profiles with reasons are %v", profileReasons)
	}
	for profile, document := range documents {
		rows, err := doctable.Rows(document, "The reason table of the profile:")
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{}
		for _, row := range rows {
			code := doctable.Names(row[0])[0]
			want[code] = doctable.Names(row[1])[0]
			if _, base := reasonStatus[code]; base {
				t.Errorf("%s: the code %s is also in the reason table of document 03", profile, code)
			}
			if StatusOf(code) != want[code] {
				t.Errorf("%s: StatusOf(%s) is %s", profile, code, StatusOf(code))
			}
		}
		if !reflect.DeepEqual(profileReasons[profile], want) {
			t.Errorf("%s: the reasons are %v, and the document has %v", profile, profileReasons[profile], want)
		}
	}
}
