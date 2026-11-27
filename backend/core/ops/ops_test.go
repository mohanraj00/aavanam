package ops_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/internal/doctable"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
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

// The tables of a profile are the operation table and the capability table of its document (IDX-PRF-4).
// The layer profile adds no operation and no capability (document 23).
func TestProfileTablesAgreeWithDocument24(t *testing.T) {
	declared := store(ops.Writer, nil)
	declared.Profiles = kinds.Profiles{kinds.IndexProfile: true}
	base := store(ops.Writer, nil)
	var operations []string
	for _, row := range rows(t, "24", "The operation table of the profile:") {
		name := doctable.Names(row[0])[0]
		operations = append(operations, name)
		if doctable.Names(row[1])[0] != "24" {
			t.Errorf("%s: the row names a different document", name)
		}
		if got, want := ops.Permits(name), strings.Contains(row[2], "`partial`"); got != want {
			t.Errorf("%s: Permits is %v, and the row of the document gives %v", name, got, want)
		}
		// RSV-LST-4: the row stays in the reserved profile table for a store without the profile.
		if ops.Reserved[name] != kinds.IndexProfile || ops.Known(name) || base.Supports(name) || !declared.Supports(name) {
			t.Errorf("%s: the operation is not an operation of the profile only", name)
		}
	}
	sort.Strings(operations)
	if got := ops.ProfileOperations(kinds.IndexProfile); !reflect.DeepEqual(got, operations) {
		t.Errorf("the operations of the profile are %v, and the document has %v", got, operations)
	}
	if got := ops.ProfileOperations(kinds.LayerProfile); len(got) != 0 || len(ops.ProfileCapabilities[kinds.LayerProfile]) != 0 {
		t.Errorf("the layer profile has the operations %v", got)
	}
	capabilities := map[string][]string{}
	for _, row := range rows(t, "24", "The capability table of the profile:") {
		capabilities[doctable.Names(row[0])[0]] = doctable.Names(row[1])
	}
	if !reflect.DeepEqual(capabilities, ops.ProfileCapabilities[kinds.IndexProfile]) {
		t.Errorf("the capabilities of the profile are %v, and the document has %v", ops.ProfileCapabilities[kinds.IndexProfile], capabilities)
	}
	// CTX-PRJ-2: the capability table of a store has the rows of its profiles, and only of them.
	if _, has := base.Capabilities()[kinds.IndexProfile]; has || len(declared.Capabilities()) != len(ops.Capabilities)+1 {
		t.Errorf("the capability tables: %v and %v", base.Capabilities(), declared.Capabilities())
	}
}

// RSV-LST-4 and IDX-PRF-1: `semantic_search` is a reserved operation for a store that does not declare the
// retrieval index profile. A store that declares it runs the operation, with the capability of IDX-PRF-4.
func TestProfileOperation(t *testing.T) {
	read := func(c *ops.Call) envelope.Envelope {
		context := c.Context()
		return c.Result(nil).WithContext(context.ID)
	}
	handlers := map[string]ops.Handler{"semantic_search": read}
	base, declared := store(ops.Writer, handlers), store(ops.Writer, handlers)
	declared.Profiles = kinds.Profiles{kinds.IndexProfile: true}
	layers := store(ops.Writer, handlers)
	layers.Profiles = kinds.Profiles{kinds.LayerProfile: true}
	declared.Storage.Session(context.Background()).Update(func(tx ops.Tx) {
		tx.PutContext("with", ops.ContextRecord{Capabilities: []string{kinds.IndexProfile}})
		tx.PutContext("without", ops.ContextRecord{Capabilities: []string{"aavanam/text/v1"}})
	})
	cases := []struct {
		store   *ops.Store
		request string
		want    string
	}{
		{base, `not JSON`, "unsupported reserved_profile no items"},
		{layers, `{"context":"with"}`, "unsupported reserved_profile no items"},
		{declared, `not JSON`, "failed malformed_json"},
		{declared, `{"context":"with"}`, "ok  context=with"},
		{declared, `{"context":"without"}`, "unsupported capability_unavailable context=without"},
	}
	for _, c := range cases {
		if got := call(c.store, `"semantic_search"`, c.request); got != c.want {
			t.Errorf("semantic_search %s with %v: %q, not %q", c.request, c.store.Profiles.Names(), got, c.want)
		}
	}
	if got := call(declared, `"lease_acquire"`, `{}`); got != "unsupported reserved_profile no items" {
		t.Errorf("a reserved operation of a different profile: %q", got)
	}
}

// naive returns the members of a layered context by the rules of LYR-PRE-1 and LYR-PRE-2 as they are
// written: a walk from each kept member and each layer member that does not go through a replaced member.
func naive(objects map[string]ops.Object, ids map[string]string, base, layer string) []string {
	given := map[string]bool{}
	keys := map[string]bool{}
	for _, ref := range objects[layer].Targets {
		given[ref] = true
		if id, has := ids[ref]; has {
			keys[objects[ref].Kind+" "+id] = true
		}
	}
	replaced := map[string]bool{}
	for _, ref := range objects[base].Targets {
		if id, has := ids[ref]; has && !given[ref] && keys[objects[ref].Kind+" "+id] {
			replaced[ref] = true
		}
	}
	seen := map[string]bool{base: true, layer: true}
	todo := append(append([]string(nil), objects[base].Targets...), objects[layer].Targets...)
	for len(todo) > 0 {
		ref := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if seen[ref] || replaced[ref] {
			continue
		}
		seen[ref] = true
		todo = append(todo, objects[ref].Targets...)
	}
	out := make([]string, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

// LYR-PRE-2: the members of a layered context, for many small graphs. The read context reads only the
// closures of the replaced members, and the result is the result of the walk of the clause.
func TestLayeredMembers(t *testing.T) {
	var got []string
	var located map[string]string
	var base, layer string
	s := store(ops.Writer, map[string]ops.Handler{"get": func(c *ops.Call) envelope.Envelope {
		rc := c.Context()
		got = rc.Members()
		located = map[string]string{}
		for _, ref := range c.Store.Storage.Session(c.Ctx).Members(rc.ID) {
			located[ref] = rc.Locate(ref)
		}
		located["absent"] = rc.Locate("absent")
		if view := rc.View(); len(view) != 2 || view[0] != base || view[1] != layer || len(rc.Roots) != 1 || rc.Layer != layer {
			t.Errorf("the view is %v, and the roots are %v", view, rc.Roots)
		}
		return c.Result(nil)
	}})
	rng := rand.New(rand.NewSource(23))
	replacedSome := 0
	for round := 0; round < 400; round++ {
		n := 2 + rng.Intn(14)
		objects := map[string]ops.Object{}
		ids := map[string]string{}
		name := func(i int) string { return fmt.Sprintf("r%d-n%02d", round, i) }
		var baseMembers, layerMembers []string
		for i := 0; i < n; i++ {
			kind := []string{"knowledge-unit", "schema"}[rng.Intn(2)]
			text := `{"kind":"` + kind + `"`
			if rng.Intn(3) > 0 {
				ids[name(i)] = []string{"a", "b", "c"}[rng.Intn(3)]
				text += `,"logical_id":"` + ids[name(i)] + `"`
			}
			var targets []string
			for j := i + 1; j < n; j++ { // a reference goes to a later object, so the graph has no cycle
				if rng.Intn(4) == 0 {
					targets = append(targets, name(j))
				}
			}
			objects[name(i)] = ops.Object{Ref: name(i), Kind: kind, Text: []byte(text + "}"), Targets: targets}
			switch rng.Intn(5) {
			case 0, 1:
				baseMembers = append(baseMembers, name(i))
			case 2:
				layerMembers = append(layerMembers, name(i))
			case 3:
				if rng.Intn(3) == 0 { // one object is a base member and a layer member
					baseMembers, layerMembers = append(baseMembers, name(i)), append(layerMembers, name(i))
				}
			}
		}
		id := fmt.Sprintf("ctx-%d", round)
		base, layer = fmt.Sprintf("r%d-base", round), fmt.Sprintf("r%d-layer", round)
		objects[base] = ops.Object{Ref: base, Kind: "snapshot", Text: []byte(`{"kind":"snapshot"}`), Targets: baseMembers}
		objects[layer] = ops.Object{Ref: layer, Kind: "layer", Text: []byte(`{"kind":"layer"}`), Targets: layerMembers}
		s.Storage.Session(context.Background()).Update(func(tx ops.Tx) {
			for _, o := range objects {
				tx.PutObject(o)
			}
			tx.PutContext(id, ops.ContextRecord{Roots: []string{base, layer}})
		})
		want := naive(objects, ids, base, layer)
		got, located = nil, nil
		e := s.Call(context.Background(), jsontext.S("get"), []byte(`{"context":"`+id+`"}`))
		if e.Status != "ok" {
			t.Fatalf("round %d: %s %v", round, e.Status, e.Reasons)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d: the members are %v, not %v", round, got, want)
		}
		member := map[string]bool{}
		for _, ref := range want {
			member[ref] = true
		}
		for ref, code := range located {
			if wantCode := map[bool]string{true: "", false: "unknown_ref"}[member[ref]]; code != wantCode { // LYR-ISO-2
				t.Fatalf("round %d: Locate(%s) is %q, not %q", round, ref, code, wantCode)
			}
		}
		if len(located)-1 > len(want) {
			replacedSome++
		}
	}
	if replacedSome < 40 {
		t.Errorf("only %d graphs have an object that is not a member", replacedSome)
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
	handlers := map[string]ops.Handler{"put": ok, "get": ok, "producer_get": ok, "item_history": ok, "remove": ok, "removals": ok,
		"gc_collect": ok}
	writer, device, output := store(ops.Writer, handlers), store(ops.Device, handlers), store(ops.Output, handlers)
	cases := []struct {
		store              *ops.Store
		operation, request string
		want               string
	}{
		{writer, `7`, `{}`, "unsupported unsupported_operation no items"},
		{writer, `null`, `not JSON`, "unsupported unsupported_operation no items"},
		{writer, `"no_such_operation"`, `not JSON`, "unsupported unsupported_operation no items"},
		{writer, `"lease_acquire"`, `not JSON`, "unsupported reserved_profile no items"},
		{device, `"lease_release"`, `{}`, "unsupported reserved_profile no items"},
		{device, `"remove"`, `not JSON`, "unsupported unsupported_role"}, // REL-RMV-1: before the input
		{output, `"remove"`, `{}`, "unsupported unsupported_role"},
		{device, `"removals"`, `[]`, "unsupported unsupported_role"},
		{output, `"removals"`, `{}`, "unsupported unsupported_role"},
		{output, `"gc_collect"`, `not JSON`, "unsupported unsupported_role"}, // LIF-GC-1: before the input
		{device, `"gc_collect"`, `{}`, "ok "},
		{writer, `"gc_collect"`, `not JSON`, "failed malformed_json"},
		{writer, `"remove"`, `{}`, "ok "},
		{writer, `"removals"`, `{}`, "ok "},
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
		tx.PutContext("ctx-1", ops.ContextRecord{Capabilities: []string{"aavanam/lexical/v1"}})
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
		"an operation that is not in the table": func(r *ops.Registry) { r.Handle("lease_acquire", ok) },
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
	if len(want) != 19 || !reflect.DeepEqual(got, want) {
		t.Errorf("the read binding has %v, and the clause has %v", got, want)
	}
	for _, operation := range want {
		if ops.Writes(operation) || strings.HasPrefix(operation, "producer_") {
			t.Errorf("%s writes content", operation)
		}
	}
}

// epochStorage is a storage with a removal epoch that a test sets (ops.RemovalEpoch).
type epochStorage struct {
	ops.Storage
	epoch *atomic.Int64
}

type epochSession struct {
	ops.Session
	epoch *atomic.Int64
}

func (s epochStorage) Session(ctx context.Context) ops.Session {
	return epochSession{s.Storage.Session(ctx), s.epoch}
}

func (s epochSession) RemovalEpoch() int64 { return s.epoch.Load() }

// A store keeps parsed descriptors in memory (IDN-CAN-1). If a different open of the storage removes an
// object, the removal epoch changes, and the store drops what it kept (REL-RMV-7, LIF-GC-5).
func TestRemovalEpochDropsTheDescriptors(t *testing.T) {
	const ref = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	mem := memstore.New()
	var epoch atomic.Int64
	var found bool
	registry := ops.NewRegistry()
	registry.Handle("get", func(c *ops.Call) envelope.Envelope {
		_, found = c.Descriptor(ref)
		return c.Result(nil)
	})
	s := ops.New(ops.Writer, epochStorage{mem, &epoch}, registry)
	update := func(fn func(tx ops.Tx)) { mem.Session(context.Background()).Update(fn) }
	update(func(tx ops.Tx) { tx.PutObject(ops.Object{Ref: ref, Kind: "source", Text: []byte(`{"kind":"source"}`)}) })
	if call(s, `"get"`, `{}`); !found {
		t.Fatal("the store does not read a stored descriptor")
	}
	update(func(tx ops.Tx) { tx.DeleteObjects([]string{ref}) }) // a removal that a different open made
	if call(s, `"get"`, `{}`); !found {
		t.Fatal("the store did not keep the descriptor, so the test shows nothing")
	}
	epoch.Add(1)
	if call(s, `"get"`, `{}`); found {
		t.Error("the store reads a removed descriptor after a change of the removal epoch")
	}
	update(func(tx ops.Tx) { tx.PutObject(ops.Object{Ref: ref, Kind: "source", Text: []byte(`{"kind":"source"}`)}) })
	if call(s, `"get"`, `{}`); !found {
		t.Error("the store does not read a descriptor that a later write added again (REL-RMV-11)")
	}
}

// A removal that commits after the start of a call stops the update of the call (ops.ErrRemoved). The
// dispatch runs the call again. After the last attempt, the fault is a `storage_error` (STS-MAL-1).
func TestCallRunsAgainAfterARemoval(t *testing.T) {
	for stops, want := range map[int]string{0: "ok  1", 3: "ok  4", 4: "failed storage_error context=c 4", 9: "failed storage_error context=c 4"} {
		runs := 0
		s := store(ops.Writer, map[string]ops.Handler{"get": func(c *ops.Call) envelope.Envelope {
			if runs++; runs <= stops {
				ops.Raise(fmt.Errorf("the update stopped: %w", ops.ErrRemoved))
			}
			return c.Result(nil)
		}})
		if got := call(s, `"get"`, `{"context":"c"}`) + fmt.Sprint(" ", runs); got != want {
			t.Errorf("%d stops: %q, not %q", stops, got, want)
		}
	}
}
