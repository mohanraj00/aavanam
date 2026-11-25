package storagetest

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/write"
)

// Core runs the operations of the core on a storage: `put`, `create_snapshot`, the deferred checks of a
// batch with withheld bytes, `open_context`, `get`, the publication and the producer section.
func Core(t *testing.T, open Open) {
	t.Run("PutWritesAllOrNone", func(t *testing.T) { putWritesAllOrNone(t, newFixture(t, open(t))) })
	t.Run("CreateSnapshot", func(t *testing.T) { createSnapshot(t, newFixture(t, open(t))) })
	t.Run("DeferredChecks", func(t *testing.T) { deferredChecks(t, newFixture(t, open(t))) })
	t.Run("DeferredBytesArrive", func(t *testing.T) { deferredBytesArrive(t, newFixture(t, open(t))) })
	t.Run("OpenContextAndGet", func(t *testing.T) { openContextAndGet(t, newFixture(t, open(t))) })
	t.Run("SkewAfterConflict", func(t *testing.T) { skewAfterConflict(t, newFixture(t, open(t))) })
	t.Run("LayeredContext", func(t *testing.T) { layeredContext(t, newFixture(t, open(t))) })
	t.Run("LayerOfServingBase", func(t *testing.T) { layerOfServingBase(t, newFixture(t, open(t))) })
	t.Run("Publication", func(t *testing.T) { publication(t, newFixture(t, open(t))) })
	t.Run("GateDuringValidation", func(t *testing.T) { gateDuringValidation(t, newFixture(t, open(t))) })
	t.Run("ConcurrentPublish", func(t *testing.T) { concurrentPublish(t, newFixture(t, open(t))) })
	t.Run("GateWaitsForThePublication", func(t *testing.T) { gateWaits(t, newFixture(t, open(t))) })
	t.Run("Producer", func(t *testing.T) { producer(t, newFixture(t, open(t))) })
	t.Run("ConcurrentProducer", func(t *testing.T) { concurrentProducer(t, newFixture(t, open(t))) })
}

const profile = `"descriptor_profile":"aavanam/base/v1"`

// Registry returns a registry with the operations of this change of the core.
func Registry() *ops.Registry {
	r := ops.NewRegistry()
	read.Register(r)
	write.Register(r)
	return r
}

type fixture struct {
	t       *testing.T
	mem     Backing
	store   *ops.Store
	storage ops.Storage
}

func newFixture(t *testing.T, storage Backing) *fixture {
	return &fixture{t: t, mem: storage, store: ops.New(ops.Writer, storage, Registry()), storage: storage}
}

func (f *fixture) call(operation, request string) envelope.Envelope {
	f.t.Helper()
	return f.store.Call(context.Background(), jsontext.S(operation), []byte(request))
}

// Summary returns the status of the envelope and, for each item, its status and reason codes or its ref.
func Summary(e envelope.Envelope) string {
	parts := []string{e.Status + reasons(e.Reasons)}
	for _, i := range e.Items {
		if i.Status == "ok" {
			parts = append(parts, "ok")
		} else {
			parts = append(parts, i.Status+reasons(i.Reasons))
		}
	}
	return strings.Join(parts, " | ")
}

func reasons(list []envelope.Reason) string {
	var codes []string
	for _, r := range list {
		codes = append(codes, r.Code)
	}
	if len(codes) == 0 {
		return ""
	}
	return ":" + strings.Join(codes, ",")
}

func quote(text string) string { return string(canon.AppendString(nil, text)) }

func revision(t *testing.T, text string) string {
	t.Helper()
	value, err := jsontext.Parse([]byte(text), jsontext.Strict)
	if err != nil {
		t.Fatal(err)
	}
	desc, refusal := check.Normal(value)
	if refusal != nil {
		t.Fatal(refusal)
	}
	return canon.Revision(desc)
}

// Source returns the text of a `source` descriptor of the bytes.
func Source(data string) string {
	return `{` + profile + `,"kind":"source","payload":{"digest":"` + jsontext.Digest([]byte(data)) +
		`","media_type":"text/plain","size":` + jsontext.ESNumber(float64(len(data))) + `}}`
}

// Put returns the request of a `put` with descriptor texts and payload bytes.
func Put(descriptors []string, payloads ...string) string {
	var texts, bytes []string
	for _, d := range descriptors {
		texts = append(texts, quote(d))
	}
	for _, p := range payloads {
		bytes = append(bytes, quote(jsontext.Digest([]byte(p)))+":"+quote(ops.B64([]byte(p))))
	}
	return `{"descriptors":[` + strings.Join(texts, ",") + `],"payloads":{` + strings.Join(bytes, ",") + `}}`
}

// A `put` writes each descriptor or none (STS-MAL-2, STS-ENV-5), and a repeat does not change the store
// (IDN-CAN-3).
func putWritesAllOrNone(t *testing.T, f *fixture) {
	a, b := Source("alpha"), Source("beta")
	empty := f.mem.Fingerprint()
	e := f.call("put", Put([]string{a, `{"kind":"workspace"}`, b, `{"a":1,"a":2}`, `{` + profile + `,"kind":"snapshot"}`}, "alpha", "beta"))
	want := "failed:duplicate_key,field_count | failed:batch_aborted | unsupported:unknown_kind | failed:batch_aborted | failed:duplicate_key | failed:field_count"
	if got := Summary(e); got != want {
		t.Errorf("a batch with refused descriptors:\n got  %s\n want %s", got, want)
	}
	if f.mem.Fingerprint() != empty {
		t.Error("a refused batch changed the store")
	}
	e = f.call("put", Put([]string{a, b, a}, "alpha", "beta"))
	if got := Summary(e); got != "ok | ok | ok | ok" || e.Items[0].Fields[0].Val.Str != revision(t, a) || e.Items[2].Fields[0].Val.Str != revision(t, a) {
		t.Errorf("a correct batch: %s", got)
	}
	once := f.mem.Fingerprint()
	if got := Summary(f.call("put", Put([]string{a}))); got != "ok | ok" || f.mem.Fingerprint() != once {
		t.Errorf("a repeat with the bytes in the store: %s", got)
	}
	if got := Summary(f.call("put", Put([]string{Source("gamma")}))); got != "not_found:payload_missing | not_found:payload_missing" {
		t.Errorf("a descriptor with no bytes: %s", got)
	}
	for request, want := range map[string]string{
		`{}`:                                   "failed:field_type",
		`{"descriptors":[]}`:                   "failed:field_type",
		`{"descriptors":[{}]}`:                 "failed:field_type",
		`{"descriptors":["{}"],"x":1}`:         "failed:field_type",
		`{"descriptors":["{}"],"payloads":[]}`: "failed:field_type",
		`{"descriptors":["{}"],"payloads":{"sha256:x":"not base64!"}}`: "failed:field_type",
	} {
		if got := Summary(f.call("put", request)); got != want {
			t.Errorf("%s: %s, not %s", request, got, want)
		}
	}
}

// PUB-SNP-1, PUB-SNP-2 and PUB-SEC-3.
func createSnapshot(t *testing.T, f *fixture) {
	if err := f.store.Section(context.Background(), "public", "serving"); err != nil {
		t.Fatal(err)
	}
	src := Source("alpha")
	set := `{` + profile + `,"kind":"binding-set"}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + revision(t, set) + `"}`
	observation := `{` + profile + `,"kind":"observation","subjects":["` + revision(t, src) + `"],"observation_type":"t","scope":{},"values":{}}`
	if got := Summary(f.call("put", Put([]string{src, set, unit, observation}, "alpha"))); !strings.HasPrefix(got, "ok") {
		t.Fatal(got)
	}
	e := f.call("create_snapshot", `{"collection":"public","units":["`+revision(t, unit)+`"]}`)
	want := revision(t, `{`+profile+`,"kind":"snapshot","collection":"public","units":["`+revision(t, unit)+`"]}`)
	if got := Summary(e); got != "ok | ok" || e.Items[0].Fields[0].Val.Str != want {
		t.Errorf("a snapshot: %s", got)
	}
	e = f.call("create_snapshot", `{"collection":"public","observations":["`+revision(t, observation)+`"]}`)
	if got := Summary(e); got != "failed:section_violation | failed:section_violation" || e.Items[0].Reasons[0].Refs[0] != revision(t, src) {
		t.Errorf("a source in the closure of a serving snapshot: %s %+v", got, e.Items)
	}
	if got := Summary(f.call("create_snapshot", `{"collection":"private","observations":["`+revision(t, observation)+`"]}`)); got != "ok | ok" {
		t.Errorf("a collection with no section: %s", got)
	}
	for request, want := range map[string]string{
		`{"collection":"c","kind":"snapshot"}`:                                        "failed:unknown_field | failed:unknown_field",
		`{"collection":"c","kind":"snapshot","descriptor_profile":"aavanam/base/v1"}`: "failed:unknown_field,unknown_field | failed:unknown_field,unknown_field",
		`{}`: "failed:field_count | failed:field_count",
		`{"collection":"c","units":["sha256:` + strings.Repeat("0", 64) + `"]}`: "not_found:missing_target | not_found:missing_target",
		`{"collection":"c","units":["` + revision(t, set) + `"]}`:               "unsupported:unsupported_target_kind | unsupported:unsupported_target_kind",
		`{"collection":"c\ud800"}`:                                              "failed:field_type | failed:field_type",
	} {
		if got := Summary(f.call("create_snapshot", request)); got != want {
			t.Errorf("%s: %s, not %s", request, got, want)
		}
	}
}

// commit runs a batch with withheld digests, as an import of a bundle does.
func commit(f *fixture, descriptors []string, data map[string][]byte, withheld ...string) write.Outcome {
	registry := ops.NewRegistry()
	var out write.Outcome
	registry.Handle("import", func(c *ops.Call) envelope.Envelope {
		batch := write.Batch{Data: data, Incomplete: true, Withheld: map[string]bool{}}
		for _, digest := range withheld {
			batch.Withheld[digest] = true
		}
		var values []jsontext.Value
		for _, d := range descriptors {
			values = append(values, jsontext.S(d))
		}
		out = batch.Commit(c, batch.Check(c, values, true), nil)
		return c.Result(nil)
	})
	ops.New(ops.Writer, f.storage, registry).Call(context.Background(), jsontext.S("import"), []byte(`{}`))
	return out
}

func outcome(o write.Outcome) string {
	return Summary(envelope.Envelope{Status: o.Status, Reasons: o.Reasons, Items: o.Items})
}

// Withheld bytes defer the payload checks of a descriptor (BUN-IMP-9). The store keeps a deferred record
// (BUN-IMP-10). When the bytes come, the store applies the checks before it adds the bytes: bytes that
// fail a check do not go into the store, and the descriptor that carries them has the reason.
func deferredChecks(t *testing.T, f *fixture) {
	whole := "the whole reading"
	wd := jsontext.Digest([]byte(whole))
	src := Source(whole)
	reading := func(size string) string {
		return `{` + profile + `,"kind":"reading","source":"` + revision(t, src) + `","producer":"p","coverage_profile":"c/c/v1","payload":{"digest":"` +
			wd + `","media_type":"text/plain","size":` + size + `}}`
	}
	good, bad := reading("17"), reading("99")
	excerpt := `{` + profile + `,"kind":"excerpt","parent":"` + revision(t, good) + `","ranges":[{"start":4,"end":9}],"payload":{"digest":"` +
		jsontext.Digest([]byte("WRONG")) + `","media_type":"text/plain","size":5}}`
	// The source declares the bytes also, so the bundle withholds them for each descriptor.
	if got := outcome(commit(f, []string{src, good, bad}, nil)); !strings.HasPrefix(got, "failed:bundle_incomplete") {
		t.Errorf("absent bytes that are not withheld: %s", got)
	}
	if got := outcome(commit(f, []string{src, good, bad, excerpt}, map[string][]byte{jsontext.Digest([]byte("WRONG")): []byte("WRONG")}, wd)); got != "ok | ok | ok | ok | ok" {
		t.Fatalf("withheld bytes: %s", got)
	}
	r := f.mem.Session(context.Background())
	if got := r.DeferredRefs(wd); len(got) != 4 || !r.HasDeferred(revision(t, bad)) {
		t.Errorf("the deferred records: %v", got)
	}
	before := f.mem.Fingerprint()
	// The bytes come with a new descriptor. The stored reading with the incorrect size and the stored
	// excerpt fail their deferred checks, so the carrier is refused and the store does not change.
	carrier := `{` + profile + `,"kind":"source","logical_id":"copy","payload":{"digest":"` + wd + `","media_type":"text/plain","size":17}}`
	e := f.call("put", Put([]string{carrier}, whole))
	if got := Summary(e); got != "failed:excerpt_mismatch,size_mismatch | failed:excerpt_mismatch,size_mismatch" {
		t.Errorf("bytes that fail a deferred check: %s", got)
	}
	if _, ok := r.Blob(wd); ok || f.mem.Fingerprint() != before {
		t.Error("bytes that fail a deferred check went into the store")
	}
}

// When the bytes of a correct descriptor come, the deferred record goes away, and an indexer gets the
// stored descriptor and the bytes in the update.
func deferredBytesArrive(t *testing.T, f *fixture) {
	whole := "the whole reading"
	wd := jsontext.Digest([]byte(whole))
	src := Source(whole)
	if got := outcome(commit(f, []string{src}, nil, wd)); got != "ok | ok" {
		t.Fatal(got)
	}
	registry := Registry()
	var indexed []string
	registry.OnBytes(func(tx ops.Tx, desc jsontext.Value, digest string, data []byte) {
		indexed = append(indexed, check.Kind(desc)+" "+string(data))
		if _, ok := tx.Blob(digest); !ok {
			t.Error("the indexer runs before the bytes are in the update")
		}
	})
	store := ops.New(ops.Writer, f.storage, registry)
	carrier := `{` + profile + `,"kind":"source","logical_id":"copy","payload":{"digest":"` + wd + `","media_type":"text/plain","size":17}}`
	e := store.Call(context.Background(), jsontext.S("put"), []byte(Put([]string{carrier}, whole)))
	if got := Summary(e); got != "ok | ok" {
		t.Fatalf("the bytes of a correct descriptor: %s", got)
	}
	r := f.mem.Session(context.Background())
	if _, ok := r.Blob(wd); !ok || r.HasDeferred(revision(t, src)) {
		t.Error("the bytes are not in the store, or the deferred record stays")
	}
	if len(indexed) != 2 || indexed[0] != "source "+whole {
		t.Errorf("the indexer got %v", indexed)
	}
}

// CTX-OPN-1 to CTX-OPN-5 and QRY-GET-2, on the operations of package read.
func openContextAndGet(t *testing.T, f *fixture) {
	src, other := Source("alpha"), Source("beta")
	set := `{` + profile + `,"kind":"binding-set"}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + revision(t, set) + `"}`
	f.call("put", Put([]string{src, other, set, unit}, "alpha", "beta"))
	snapshot := f.call("create_snapshot", `{"collection":"c","units":["`+revision(t, unit)+`"]}`).Items[0].Fields[0].Val.Str
	for request, want := range map[string]string{
		`{}`:                        "failed:field_count",
		`{"roots":[],"heads":[]}`:   "failed:field_count",
		`{"roots":"x","x":1,"y":2}`: "failed:unknown_field,unknown_field",
		`{"roots":["x",7],"skew_policy":"other","heads":7}`:        "failed:field_type,field_type,field_type,field_type",
		`{"roots":["` + snapshot + `"],"heads":["a","a","a"]}`:     "failed:field_count,field_count",
		`{"roots":["` + snapshot + `"],"capabilities":["x/y/v1"]}`: "unsupported:unsupported_profile",
		`{"roots":["` + revision(t, src) + `"]}`:                   "unsupported:unsupported_target_kind | unsupported:unsupported_target_kind",
		`{"roots":["sha256:` + strings.Repeat("0", 64) + `"]}`:     "not_found:unknown_ref | not_found:unknown_ref",
		`{"heads":["c"]}`: "unresolved:no_head | unresolved:no_head",
		`{"roots":["` + snapshot + `"],"heads":["c"]}`: "unresolved:no_head | ok | unresolved:no_head",
	} {
		e := f.call("open_context", request)
		if got := Summary(e); got != want || e.Context.Kind != jsontext.Null {
			t.Errorf("%s: %s, not %s", request, got, want)
		}
	}
	e := f.call("open_context", `{"roots":["`+snapshot+`"],"capabilities":["aavanam/text/v1","aavanam/data/v1"]}`)
	if got := Summary(e); got != "ok | ok" || !regexp.MustCompile(`^ctx-[0-9a-f]{32}$`).MatchString(e.Context.Str) {
		t.Fatalf("open_context: %s %q", got, e.Context.Str)
	}
	id := e.Context.Str
	record, _ := f.mem.Session(context.Background()).Context(id)
	if strings.Join(record.Capabilities, ",") != "aavanam/data/v1,aavanam/text/v1" || record.Roots[0] != snapshot {
		t.Errorf("the context record: %+v", record)
	}
	if second := f.call("open_context", `{"roots":["`+snapshot+`"]}`).Context.Str; second == id {
		t.Error("two contexts have one identity")
	}
	e = f.call("get", `{"context":"`+id+`","refs":["`+revision(t, set)+`","`+revision(t, other)+`","sha256:`+strings.Repeat("0", 64)+`"]}`)
	if got := Summary(e); got != "not_found:not_in_context,unknown_ref | ok | not_found:not_in_context | not_found:unknown_ref" || e.Context.Str != id {
		t.Errorf("get: %s", got)
	}
	for request, want := range map[string]string{
		`{"context":"` + id + `"}`:                           "failed:field_type",
		`{"context":"` + id + `","refs":[]}`:                 "failed:field_type",
		`{"context":"` + id + `","refs":["x"]}`:              "failed:field_type",
		`{"context":"ctx-none","refs":["` + snapshot + `"]}`: "failed:context_expired",
	} {
		if got := Summary(f.call("get", request)); got != want {
			t.Errorf("%s: %s, not %s", request, got, want)
		}
	}
}

// unitOf returns the texts of a binding set and of a unit that refers to it. `n` makes the binding set
// different from each other one. `more` has more fields of the unit, for example `,"logical_id":"a"`.
func unitOf(t *testing.T, n int, more string) (set, unit string) {
	set = `{` + profile + `,"kind":"binding-set","parameters":{"n":` + jsontext.ESNumber(float64(n)) + `}}`
	return set, `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + revision(t, set) + `"` + more + `}`
}

// layerRevision returns the revision of a layer text. The fixture of the test declares the layer profile.
func layerRevision(t *testing.T, text string) string {
	t.Helper()
	value, err := jsontext.Parse([]byte(text), jsontext.Strict)
	if err != nil {
		t.Fatal(err)
	}
	desc, refusal := check.NormalIn(value, kinds.Profiles{kinds.LayerProfile: true})
	if refusal != nil {
		t.Fatal(refusal)
	}
	return canon.Revision(desc)
}

// LYR-PRF-1, LYR-OPN-1 to LYR-OPN-3, LYR-PRE-2, LYR-ISO-2 and LYR-LCK-1 on a storage: a layered context has
// the kept members, the layer members and what they reach, and not a replaced member.
func layeredContext(t *testing.T, f *fixture) {
	shared := `{` + profile + `,"kind":"overlay-set","ext":{"n":1}}`
	sets := `,"overlay_sets":["` + revision(t, shared) + `"]`
	setA, unitA := unitOf(t, 1, `,"logical_id":"a"`+sets) // the layer replaces this unit
	setB, unitB := unitOf(t, 2, `,"logical_id":"b"`+sets) // a kept member
	setA2, unitA2 := unitOf(t, 3, `,"logical_id":"a"`)    // the unit of the layer with the logical ID `a`
	setC, unitC := unitOf(t, 4, `,"logical_id":"c"`)      // a unit that the layer adds
	ref := func(text string) string { return revision(t, text) }
	layerOf := func(scope string, units ...string) string {
		return `{` + profile + `,"kind":"layer","base":"c","scope":` + quote(scope) + `,"units":["` + strings.Join(units, `","`) + `"]}`
	}
	layer, other := layerOf("north", ref(unitA2), ref(unitC)), layerOf("south", ref(unitC))
	descriptors := []string{shared, setA, unitA, setB, unitB, setA2, unitA2, setC, unitC}
	if got := Summary(f.call("put", Put(append(descriptors, layer)))); !strings.HasPrefix(got, "unsupported:unknown_kind") {
		t.Errorf("a layer in a store with no profile: %s", got) // LYR-PRF-1
	}
	snapshotRequest := `{"collection":"c","units":["` + ref(unitA) + `","` + ref(unitB) + `"]}`
	f.store.Profiles = kinds.Profiles{kinds.LayerProfile: true}
	if got := Summary(f.call("put", Put(append(descriptors, layer, other)))); strings.Contains(got, ":") {
		t.Fatalf("put: %s", got)
	}
	base := f.call("create_snapshot", snapshotRequest).Items[0].Fields[0].Val.Str
	layerRef, otherRef := layerRevision(t, layer), layerRevision(t, other)
	open := func(root, layer, scope string) envelope.Envelope {
		return f.call("open_context", `{"roots":["`+root+`"],"layer":{"ref":"`+layer+`","scope":"`+scope+`"}}`)
	}
	for name, c := range map[string]struct {
		e    envelope.Envelope
		want string
	}{
		"a layer with no scope":   {f.call("open_context", `{"roots":["`+base+`"],"layer":{"ref":"`+layerRef+`"}}`), "failed:field_type"},
		"an empty scope":          {open(base, layerRef, ""), "failed:field_type"},
		"two roots":               {f.call("open_context", `{"roots":["`+base+`","`+ref(unitB)+`"],"layer":{"ref":"`+layerRef+`","scope":"north"}}`), "failed:field_count"},
		"an unknown layer":        {open(base, "sha256:"+strings.Repeat("0", 64), "north"), "not_found:unknown_ref | ok | not_found:unknown_ref"},
		"a layer that is a unit":  {open(base, ref(unitC), "north"), "unsupported:unsupported_target_kind | ok | unsupported:unsupported_target_kind"},
		"a different scope":       {open(base, layerRef, "North"), "incompatible:layer_scope_mismatch | ok | incompatible:layer_scope_mismatch"},
		"a root that is a unit":   {open(ref(unitB), layerRef, "north"), "incompatible:layer_base_mismatch | ok | incompatible:layer_base_mismatch"},
		"a root that is unknown":  {open("sha256:"+strings.Repeat("0", 64), layerRef, "north"), "not_found:unknown_ref | not_found:unknown_ref | ok"},
		"a layer as the one root": {f.call("open_context", `{"roots":["`+layerRef+`"]}`), "unsupported:unsupported_target_kind | unsupported:unsupported_target_kind"},
	} {
		if got := Summary(c.e); got != c.want || c.e.Context.Kind != jsontext.Null {
			t.Errorf("%s: %s, not %s", name, got, c.want)
		}
	}
	e := open(base, layerRef, "north")
	if got := Summary(e); got != "ok | ok | ok" || e.Items[0].Fields[0].Val.Str != base || e.Items[1].Fields[0].Val.Str != layerRef {
		t.Fatalf("open_context with a layer: %s", got)
	}
	layered := e.Context.Str
	plain := f.call("open_context", `{"roots":["`+base+`"]}`).Context.Str
	all := []string{base, layerRef, ref(unitB), ref(setB), ref(shared), ref(unitA2), ref(setA2), ref(unitC), ref(setC), // the members
		ref(unitA), ref(setA), otherRef, "sha256:" + strings.Repeat("0", 64)}
	get := func(id string) string {
		return Summary(f.call("get", `{"context":"`+id+`","refs":["`+strings.Join(all, `","`)+`"]}`))
	}
	members, absent := strings.Repeat(" | ok", 9), " | not_found:unknown_ref"
	if got, want := get(layered), "not_found:unknown_ref,unknown_ref,unknown_ref,unknown_ref"+members+strings.Repeat(absent, 4); got != want {
		t.Errorf("get in the layered context:\n got  %s\n want %s", got, want) // LYR-PRE-2, LYR-ISO-2
	}
	outside := " | not_found:not_in_context"
	want := "not_found:not_in_context,not_in_context,not_in_context,not_in_context,not_in_context,not_in_context,unknown_ref | ok" + outside +
		" | ok | ok | ok" + strings.Repeat(outside, 4) + " | ok | ok" + outside + absent
	if got := get(plain); got != want {
		t.Errorf("get in the context with no layer:\n got  %s\n want %s", got, want) // LYR-ISO-1, CTX-OPN-4
	}
	// LYR-OPN-3: the record of the context has the base snapshot and the layer, and the storage has no
	// more state for a layered context.
	record, _ := f.mem.Session(context.Background()).Context(layered)
	if len(record.Roots) != 2 || record.Roots[0] != base || record.Roots[1] != layerRef {
		t.Errorf("the record of the layered context: %+v", record)
	}
}

// LYR-DEF-5: a layer whose base is a `serving` collection has no Evidence material, and a collection that
// is the base of a layer gets no first section declaration.
func layerOfServingBase(t *testing.T, f *fixture) {
	ctx := context.Background()
	f.store.Profiles = kinds.Profiles{kinds.LayerProfile: true}
	if err := f.store.Section(ctx, "served", "serving"); err != nil {
		t.Fatal(err)
	}
	src := Source("alpha")
	observation := `{` + profile + `,"kind":"observation","subjects":["` + revision(t, src) + `"],"observation_type":"t","scope":1,"values":1}`
	layerOf := func(base string) string {
		return `{` + profile + `,"kind":"layer","base":"` + base + `","scope":"north","observations":["` + revision(t, observation) + `"]}`
	}
	e := f.call("put", Put([]string{src, observation, layerOf("served")}, "alpha"))
	if got := Summary(e); got != "failed:section_violation | failed:batch_aborted | failed:batch_aborted | failed:section_violation" {
		t.Errorf("a layer of a serving base with a source: %s", got)
	}
	if got := Summary(f.call("put", Put([]string{src, observation, layerOf("free")}, "alpha"))); got != "ok | ok | ok | ok" {
		t.Fatalf("a layer of a base with no section: %s", got)
	}
	// The declaration comes before the first call, so a second open of the store makes it.
	second := ops.New(ops.Writer, f.storage, Registry())
	if err := second.Section(ctx, "free", "serving"); err == nil || !strings.Contains(err.Error(), "LYR-DEF-5") {
		t.Errorf("a first section declaration for the base of a layer: %v", err)
	}
	if _, declared := f.mem.Session(ctx).Section("free"); declared {
		t.Error("a refused declaration changed the store")
	}
	if err := second.Section(ctx, "served", "serving"); err != nil {
		t.Errorf("the same section again: %v", err)
	}
}

// CTX-SKW-4: the store checks skew only if each item is `ok` after the collection conflict check
// (CTX-CMP-1). Two snapshots of one collection give `ambiguous`, and two other snapshots with skew stay `ok`.
func skewAfterConflict(t *testing.T, f *fixture) {
	snapshot := func(collection, profileID string) string {
		profile1 := `{` + profile + `,"kind":"runtime-profile","logical_id":"runtime","profile_id":"` + profileID + `"}`
		set := `{` + profile + `,"kind":"binding-set","runtime_profiles":["` + revision(t, profile1) + `"]}`
		unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + revision(t, set) + `"}`
		if got := Summary(f.call("put", Put([]string{profile1, set, unit}))); !strings.HasPrefix(got, "ok") {
			t.Fatal(got)
		}
		return f.call("create_snapshot", `{"collection":"`+collection+`","units":["`+revision(t, unit)+`"]}`).Items[0].Fields[0].Val.Str
	}
	x, y := snapshot("x", "a/a/v1"), snapshot("y", "b/b/v1")
	z1, z2 := snapshot("z", "a/a/v1"), snapshot("z", "b/b/v1")
	roots := func(refs ...string) string { return `{"roots":["` + strings.Join(refs, `","`) + `"]}` }
	if got := Summary(f.call("open_context", roots(x, y))); got != "incompatible:binding_skew,binding_skew | incompatible:binding_skew | incompatible:binding_skew" {
		t.Errorf("two snapshots with skew: %s", got)
	}
	if got := Summary(f.call("open_context", roots(x, y)[:len(roots(x, y))-1]+`,"skew_policy":"permit"}`)); got != "ok | ok | ok" {
		t.Errorf("the policy permit: %s", got)
	}
	want := "ambiguous:collection_conflict,collection_conflict | ambiguous:collection_conflict | ambiguous:collection_conflict | ok | ok"
	if got := Summary(f.call("open_context", roots(z1, z2, x, y))); got != want {
		t.Errorf("a collection conflict and skew:\n got  %s\n want %s", got, want)
	}
}
