package ops_test

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/internal/doctable"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

func rows(t *testing.T, document, marker string) [][]string {
	t.Helper()
	out, err := doctable.Rows(document, marker)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The tables of this package are the operation table (document 03), the reserved profile table (document
// 16) and the capability table (document 08).
func TestTablesAgreeWithTheDocuments(t *testing.T) {
	var base []string
	reserved := map[string]bool{}
	for _, row := range rows(t, "03", "The operation table:") {
		name, statuses := doctable.Names(row[0])[0], doctable.Names(row[2])
		if len(statuses) == 1 && statuses[0] == "unsupported" {
			reserved[name] = true
			continue
		}
		base = append(base, name)
		if got, want := ops.Permits(name), strings.Contains(row[2], "`partial`"); got != want {
			t.Errorf("%s: Permits is %v, and the row of the document gives %v", name, got, want)
		}
	}
	sort.Strings(base)
	if !reflect.DeepEqual(base, ops.Operations()) {
		t.Errorf("the operations are %v, and the document has %v", ops.Operations(), base)
	}
	profiles := map[string]string{}
	for _, row := range rows(t, "16", "The reserved profile table:") {
		for _, name := range doctable.Names(row[2]) {
			profiles[name] = doctable.Names(row[0])[0]
		}
	}
	if !reflect.DeepEqual(profiles, ops.Reserved) || len(profiles) != len(reserved) {
		t.Errorf("the reserved operations are %v, and the document has %v", ops.Reserved, profiles)
	}
	capabilities := map[string][]string{}
	for _, row := range rows(t, "08", "| Capability | Operations |") {
		capabilities[doctable.Names(row[0])[0]] = doctable.Names(row[1])
	}
	if !reflect.DeepEqual(capabilities, ops.Capabilities) {
		t.Errorf("the capabilities are %v, and the document has %v", ops.Capabilities, capabilities)
	}
}

func store(role ops.Role, handlers map[string]ops.Handler) *ops.Store {
	registry := ops.NewRegistry()
	for name, h := range handlers {
		registry.Handle(name, h)
	}
	return ops.New(role, memstore.New(), registry)
}

func call(s *ops.Store, operation, request string) string {
	op, _ := jsontext.Parse([]byte(operation), jsontext.Lenient)
	e := s.Call(context.Background(), op, []byte(request))
	var codes []string
	for _, r := range e.Reasons {
		codes = append(codes, r.Code)
	}
	context := ""
	if e.Context.Kind == jsontext.String {
		context = " context=" + e.Context.Str
	}
	items := " no items"
	if e.HasItems {
		items = ""
	}
	return e.Status + " " + strings.Join(codes, ",") + context + items
}

// STS-MAL-4: the operation, then the role of the store, then the input.
func TestCheckOrder(t *testing.T) {
	ok := func(c *ops.Call) envelope.Envelope { return c.Result(nil) }
	handlers := map[string]ops.Handler{"put": ok, "get": ok, "producer_get": ok, "item_history": ok}
	writer, device, output := store(ops.Writer, handlers), store(ops.Device, handlers), store(ops.Output, handlers)
	cases := []struct {
		store              *ops.Store
		operation, request string
		want               string
	}{
		{writer, `7`, `{}`, "unsupported unsupported_operation no items"},
		{writer, `null`, `not JSON`, "unsupported unsupported_operation no items"},
		{writer, `"no_such_operation"`, `not JSON`, "unsupported unsupported_operation no items"},
		{writer, `"gc_collect"`, `not JSON`, "unsupported reserved_profile no items"},
		{device, `"gc_collect"`, `{}`, "unsupported reserved_profile no items"},
		{device, `"put"`, `not JSON`, "unsupported read_only_store"}, // BUN-IMP-6: before the input
		{device, `"put"`, `{}`, "unsupported read_only_store"},
		{output, `"put"`, `{}`, "ok "},
		{device, `"producer_get"`, `[]`, "unsupported producer_section_absent"}, // PRD-SEC-1: before the input
		{output, `"producer_get"`, `{}`, "unsupported producer_section_absent"},
		{writer, `"producer_get"`, `{}`, "ok "},
		{writer, `"read"`, `{}`, "unsupported unsupported_operation"}, // no handler in this store
		{device, `"create_candidate"`, `{}`, "unsupported read_only_store"},
		{writer, `"get"`, `not JSON`, "failed malformed_json"},
		{writer, `"get"`, `{"a":1,"a":2,"a":3}`, "failed duplicate_key,duplicate_key"},
		{writer, `"get"`, `[]`, "failed field_type"},
		{writer, `"get"`, `"text"`, "failed field_type"},
		{writer, `"get"`, `{}`, "ok "},
	}
	for _, c := range cases {
		if got := call(c.store, c.operation, c.request); got != c.want {
			t.Errorf("%s %s: %q, not %q", c.operation, c.request, got, c.want)
		}
	}
}

// Rule 1 of STS-ENV-3 and STS-MAL-1: a failure has an empty item list and the context of the request, and
// no raw failure crosses the API.
func TestFailures(t *testing.T) {
	deadline := context.DeadlineExceeded
	handlers := map[string]ops.Handler{
		"get":    func(c *ops.Call) envelope.Envelope { c.Context(); return c.Result(nil) },
		"search": func(c *ops.Call) envelope.Envelope { c.Context(); return c.Result(nil) },
		"read": func(c *ops.Call) envelope.Envelope {
			ops.Raise(errors.New("the disk has a secret name"))
			return c.Result(nil)
		},
		"evidence":     func(c *ops.Call) envelope.Envelope { ops.Raise(deadline); return c.Result(nil) },
		"render":       func(c *ops.Call) envelope.Envelope { ops.Fail("field_type", "field_type"); return c.Result(nil) },
		"item_history": func(c *ops.Call) envelope.Envelope { ops.Fail("field_type"); return c.Result(nil) },
		"compare":      func(c *ops.Call) envelope.Envelope { var m map[string]int; m["x"] = 1; return c.Result(nil) },
	}
	s := store(ops.Writer, handlers)
	for request, want := range map[string]string{
		`{"context":7}`:       "failed field_type",
		`{}`:                  "failed field_type",
		`{"context":"ctx-0"}`: "failed context_expired context=ctx-0",
	} {
		if got := call(s, `"get"`, request); got != want {
			t.Errorf("get %s: %q, not %q", request, got, want)
		}
	}
	for operation, want := range map[string]string{
		`"read"`:         "failed storage_error context=c",
		`"evidence"`:     "failed deadline_exceeded context=c",
		`"item_history"`: "failed field_type", // LIF-ITM-4: this operation has no read context
		`"compare"`:      "failed storage_error context=c",
	} {
		if got := call(s, operation, `{"context":"c"}`); got != want {
			t.Errorf("%s: %q, not %q", operation, got, want)
		}
	}
	op, _ := jsontext.Parse([]byte(`"render"`), jsontext.Lenient)
	text := string(s.Call(context.Background(), op, []byte(`{}`)).JSON())
	if !strings.Contains(text, `"document":null,"document_id":null`) || !strings.Contains(text, `"items":[]`) || strings.Count(text, "field_type") != 2 {
		t.Errorf("the failure of render (RND-DOC-3): %s", text)
	}
	if text := string(s.Call(context.Background(), jsontext.S("read"), []byte(`{}`)).JSON()); strings.Contains(text, "secret") {
		t.Errorf("a storage message crossed the API: %s", text)
	}
	s.Debug = true
	defer func() {
		if recover() == nil {
			t.Error("with Debug, a defect of an operation did not continue")
		}
	}()
	call(s, `"compare"`, `{}`)
}

// A context without a capability refuses the operations that need it (CTX-PRJ-4).
func TestCapabilityOfTheContext(t *testing.T) {
	read := func(c *ops.Call) envelope.Envelope {
		context := c.Context()
		return c.Result(nil).WithContext(context.ID)
	}
	s := store(ops.Writer, map[string]ops.Handler{"get": read, "search": read, "query_data": read})
	s.Storage.Session(context.Background()).Update(func(tx ops.Tx) {
		tx.PutContext("ctx-1", ops.ContextRecord{Capabilities: []string{"aavanam/lexical/v1"}}, []string{"m"})
	})
	for operation, want := range map[string]string{
		`"get"`:        "ok  context=ctx-1",
		`"search"`:     "ok  context=ctx-1",
		`"query_data"`: "unsupported capability_unavailable context=ctx-1",
	} {
		if got := call(s, operation, `{"context":"ctx-1"}`); got != want {
			t.Errorf("%s: %q, not %q", operation, got, want)
		}
	}
}

// PUB-GAT-1, PUB-SEC-1 and PUB-SEC-2.
func TestDeclarations(t *testing.T) {
	ctx := context.Background()
	s := store(ops.Writer, map[string]ops.Handler{"get": func(c *ops.Call) envelope.Envelope { return c.Result(nil) }})
	session := s.Storage.Session(ctx)
	session.Update(func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: "s1", Kind: "snapshot", Text: []byte("{}"), Collection: "old"})
	})
	steps := []struct {
		name string
		err  error
		want bool
	}{
		{"a gate", s.Gate(ctx, "c"), true},
		{"a section", s.Section(ctx, "c", "serving"), true},
		{"the same section again", s.Section(ctx, "c", "serving"), true},
		{"a different section", s.Section(ctx, "c", "evidence"), false},
		{"a name that is not a section", s.Section(ctx, "d", "public"), false},
		{"a collection with a snapshot", s.Section(ctx, "old", "evidence"), false},
		{"a section on a device store", store(ops.Device, nil).Section(ctx, "c", "serving"), false},
	}
	for _, step := range steps {
		if (step.err == nil) != step.want {
			t.Errorf("%s: %v", step.name, step.err)
		}
	}
	if section, _ := session.Section("c"); section != "serving" || !session.Gated("c") || session.Gated("d") {
		t.Error("the store did not keep the declarations")
	}
	if _, declared := session.Section("old"); declared {
		t.Error("a refused declaration changed the store")
	}
	call(s, `"get"`, `{}`)
	if s.Gate(ctx, "late") == nil || s.Section(ctx, "late", "serving") == nil {
		t.Error("a declaration after the first call was accepted")
	}
}

func TestRegistryRefusesAnIncorrectOperation(t *testing.T) {
	ok := func(c *ops.Call) envelope.Envelope { return c.Result(nil) }
	for name, register := range map[string]func(r *ops.Registry){
		"an operation that is not in the table": func(r *ops.Registry) { r.Handle("gc_collect", ok) },
		"two handlers":                          func(r *ops.Registry) { r.Handle("get", ok); r.Handle("get", ok) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			register(ops.NewRegistry())
		}()
	}
}

func TestRequestForms(t *testing.T) {
	form := func(c *ops.Call) envelope.Envelope {
		c.Strict([]ops.Field{ops.Req("producer", ops.IsName(4)), ops.Opt("refs", ops.IsRefs), ops.Opt("data", ops.IsB64),
			ops.Opt("n", ops.IsCount(1)), ops.Opt("log", ops.IsLog)}, []string{"refs"}, []string{"refs"})
		return c.Result(nil)
	}
	s := store(ops.Writer, map[string]ops.Handler{"producer_put": form})
	ref := `"sha256:` + strings.Repeat("a", 64) + `"`
	for request, want := range map[string]string{
		`{"producer":"p"}`:                      "ok ",
		`{"producer":"p","x":1,"refs":[]}`:      "failed unknown_field",
		`{"refs":[` + ref + `]}`:                "failed field_count",
		`{"producer":"p","refs":[]}`:            "failed field_count",
		`{"producer":"p","refs":[1,1]}`:         "failed field_count",
		`{"producer":"p","refs":[1]}`:           "failed field_type",
		`{"producer":"p","refs":[` + ref + `]}`: "ok ",
		`{"producer":"","n":1}`:                 "failed field_type",
		`{"producer":"12345"}`:                  "failed field_type",
		`{"producer":"éééé"}`:                   "ok ", // four characters, eight bytes
		`{"producer":"p\ud800"}`:                "failed field_type",
		`{"producer":"p","n":0}`:                "failed field_type",
		`{"producer":"p","n":1.5}`:              "failed field_type",
		`{"producer":"p","n":2}`:                "ok ",
		`{"producer":"p","data":"YQ=="}`:        "ok ",
		`{"producer":"p","data":""}`:            "ok ",
		`{"producer":"p","data":"YQ"}`:          "failed field_type",
		`{"producer":"p","data":"YQ==\n"}`:      "failed field_type",
		`{"producer":"p","data":"Y Q=="}`:       "failed field_type",
		`{"producer":"p","data":7}`:             "failed field_type",
		`{"producer":"p","log":["a","b"]}`:      "ok ",
		`{"producer":"p","log":[]}`:             "failed field_type",
		`{"producer":"p","log":["a",""]}`:       "failed field_type",
	} {
		if got := call(s, `"producer_put"`, request); got != want {
			t.Errorf("%s: %q, not %q", request, got, want)
		}
	}
	if string(ops.Unb64(jsontext.S(ops.B64([]byte{0, 255, 7})))) != "\x00\xff\x07" {
		t.Error("B64 and Unb64")
	}
}

// The operations of the read binding are the operations of the first sentence of NET-SCP-1. Each has a row
// in the operation table, and none is a write of BUN-IMP-6.
func TestReadBindingAgreesWithDocument22(t *testing.T) {
	clause, err := doctable.Clause("22", "NET-SCP-1")
	if err != nil {
		t.Fatal(err)
	}
	sentence, _, _ := strings.Cut(clause, ". ")
	var want []string
	for _, name := range regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(sentence, -1) {
		want = append(want, name[1])
	}
	sort.Strings(want)
	var got []string
	for _, operation := range ops.Operations() {
		if ops.InReadBinding(operation) {
			got = append(got, operation)
		}
	}
	if len(want) != 18 || !reflect.DeepEqual(got, want) {
		t.Errorf("the read binding has %v, and the clause has %v", got, want)
	}
	for _, operation := range want {
		if ops.Writes(operation) || strings.HasPrefix(operation, "producer_") {
			t.Errorf("%s writes content", operation)
		}
	}
}
