package read_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/write"
)

const profile = `"descriptor_profile":"aavanam/base/v1"`

func ref(t *testing.T, desc string) string {
	t.Helper()
	value, err := jsontext.Parse([]byte(desc), jsontext.Strict)
	if err != nil {
		t.Fatal(err)
	}
	normal, refusal := check.Normal(value)
	if refusal != nil {
		t.Fatal(refusal)
	}
	return canon.Revision(normal)
}

// A selector bound beyond int64 is beyond each payload: it gives `selector_range` and not a slice of the
// bytes (CRD-SEL-3, CHG-SEL-3). A row index beyond int64 is a correct form (CRD-SEL-1).
func TestSelectorBeyondInt64(t *testing.T) {
	r := ops.NewRegistry()
	read.Register(r)
	write.Register(r)
	store := ops.New(ops.Writer, memstore.New(), r)
	call := func(operation, request string) string {
		return string(store.Call(context.Background(), jsontext.S(operation), []byte(request)).JSON())
	}
	data := "abc"
	digest := jsontext.Digest([]byte(data))
	artifact := `{` + profile + `,"kind":"artifact","artifact_type":"note","payload":{"digest":"` + digest +
		`","media_type":"text/plain","size":3}}`
	bindings := `{` + profile + `,"kind":"binding-set"}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + ref(t, bindings) + `","narratives":["` + ref(t, artifact) + `"]}`
	snapshot := `{` + profile + `,"kind":"snapshot","collection":"c","units":["` + ref(t, unit) + `"]}`
	var texts []string
	for _, d := range []string{artifact, bindings, unit, snapshot} {
		texts = append(texts, string(canon.AppendString(nil, d)))
	}
	put := `{"descriptors":[` + strings.Join(texts, ",") + `],"payloads":{"` + digest + `":"` + ops.B64([]byte(data)) + `"}}`
	if out := store.Call(context.Background(), jsontext.S("put"), []byte(put)); out.Status != "ok" {
		t.Fatalf("put: %s", out.JSON())
	}
	opened := store.Call(context.Background(), jsontext.S("open_context"), []byte(`{"roots":["`+ref(t, snapshot)+`"]}`))
	if opened.Status != "ok" {
		t.Fatalf("open_context: %s", opened.JSON())
	}
	target := `"context":"` + opened.Context.Str + `"`
	for _, start := range []string{"0", "1", "100000000000000000000"} {
		selector := `{"selector_profile":"aavanam/bytes/v1","start":` + start + `,"end":100000000000000000000}`
		out := call("read", `{`+target+`,"ref":"`+ref(t, artifact)+`","selector":`+selector+`}`)
		if !strings.Contains(out, `"selector_range"`) {
			t.Errorf("read from %s: %s", start, out)
		}
		out = call("compare", `{`+target+`,"before":"`+ref(t, artifact)+`","after":"`+ref(t, artifact)+`","selector":`+selector+`}`)
		if !strings.Contains(out, `"selector_range"`) {
			t.Errorf("compare from %s: %s", start, out)
		}
	}
	row := `{"selector_profile":"aavanam/row/v1","index":100000000000000000000}`
	if out := call("read", `{`+target+`,"ref":"`+ref(t, artifact)+`","selector":`+row+`}`); !strings.Contains(out, `"unsupported_selector"`) {
		t.Errorf("read with a row index: %s", out)
	}
}
