package write_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/write"
)

const profile = `"descriptor_profile":"aavanam/base/v1"`

func registry() *ops.Registry {
	r := ops.NewRegistry()
	read.Register(r)
	write.Register(r)
	return r
}

type fixture struct {
	t       *testing.T
	mem     *memstore.Store
	store   *ops.Store
	storage ops.Storage
}

func newFixture(t *testing.T) *fixture {
	mem := memstore.New()
	return &fixture{t: t, mem: mem, store: ops.New(ops.Writer, mem, registry()), storage: mem}
}

func (f *fixture) call(operation, request string) envelope.Envelope {
	f.t.Helper()
	return f.store.Call(context.Background(), jsontext.S(operation), []byte(request))
}

// summary returns the status of the envelope and, for each item, its status and reason codes or its ref.
func summary(e envelope.Envelope) string {
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

func source(data string) string {
	return `{` + profile + `,"kind":"source","payload":{"digest":"` + jsontext.Digest([]byte(data)) +
		`","media_type":"text/plain","size":` + jsontext.ESNumber(float64(len(data))) + `}}`
}

func put(descriptors []string, payloads ...string) string {
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
func TestPutWritesAllOrNone(t *testing.T) {
	f := newFixture(t)
	a, b := source("alpha"), source("beta")
	empty := f.mem.Fingerprint()
	e := f.call("put", put([]string{a, `{"kind":"workspace"}`, b, `{"a":1,"a":2}`, `{` + profile + `,"kind":"snapshot"}`}, "alpha", "beta"))
	want := "failed:duplicate_key,field_count | failed:batch_aborted | unsupported:unknown_kind | failed:batch_aborted | failed:duplicate_key | failed:field_count"
	if got := summary(e); got != want {
		t.Errorf("a batch with refused descriptors:\n got  %s\n want %s", got, want)
	}
	if f.mem.Fingerprint() != empty {
		t.Error("a refused batch changed the store")
	}
	e = f.call("put", put([]string{a, b, a}, "alpha", "beta"))
	if got := summary(e); got != "ok | ok | ok | ok" || e.Items[0].Fields[0].Val.Str != revision(t, a) || e.Items[2].Fields[0].Val.Str != revision(t, a) {
		t.Errorf("a correct batch: %s", got)
	}
	once := f.mem.Fingerprint()
	if got := summary(f.call("put", put([]string{a}))); got != "ok | ok" || f.mem.Fingerprint() != once {
		t.Errorf("a repeat with the bytes in the store: %s", got)
	}
	if got := summary(f.call("put", put([]string{source("gamma")}))); got != "not_found:payload_missing | not_found:payload_missing" {
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
		if got := summary(f.call("put", request)); got != want {
			t.Errorf("%s: %s, not %s", request, got, want)
		}
	}
}

// PUB-SNP-1, PUB-SNP-2 and PUB-SEC-3.
func TestCreateSnapshot(t *testing.T) {
	f := newFixture(t)
	if err := f.store.Section(context.Background(), "public", "serving"); err != nil {
		t.Fatal(err)
	}
	src := source("alpha")
	set := `{` + profile + `,"kind":"binding-set"}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + revision(t, set) + `"}`
	observation := `{` + profile + `,"kind":"observation","subjects":["` + revision(t, src) + `"],"observation_type":"t","scope":{},"values":{}}`
	if got := summary(f.call("put", put([]string{src, set, unit, observation}, "alpha"))); !strings.HasPrefix(got, "ok") {
		t.Fatal(got)
	}
	e := f.call("create_snapshot", `{"collection":"public","units":["`+revision(t, unit)+`"]}`)
	want := revision(t, `{`+profile+`,"kind":"snapshot","collection":"public","units":["`+revision(t, unit)+`"]}`)
	if got := summary(e); got != "ok | ok" || e.Items[0].Fields[0].Val.Str != want {
		t.Errorf("a snapshot: %s", got)
	}
	e = f.call("create_snapshot", `{"collection":"public","observations":["`+revision(t, observation)+`"]}`)
	if got := summary(e); got != "failed:section_violation | failed:section_violation" || e.Items[0].Reasons[0].Refs[0] != revision(t, src) {
		t.Errorf("a source in the closure of a serving snapshot: %s %+v", got, e.Items)
	}
	if got := summary(f.call("create_snapshot", `{"collection":"private","observations":["`+revision(t, observation)+`"]}`)); got != "ok | ok" {
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
		if got := summary(f.call("create_snapshot", request)); got != want {
			t.Errorf("%s: %s, not %s", request, got, want)
		}
	}
}

// faulty is a storage whose updates fail when they write a descriptor.
type faulty struct{ ops.Storage }

type faultySession struct{ ops.Session }

type faultyTx struct{ ops.Tx }

func (f faulty) Session(ctx context.Context) ops.Session {
	return faultySession{f.Storage.Session(ctx)}
}

func (s faultySession) Update(fn func(tx ops.Tx)) {
	s.Session.Update(func(tx ops.Tx) { fn(faultyTx{tx}) })
}

func (faultyTx) PutObject(ops.Object) { ops.Raise(errors.New("the connection is not there")) }

// A storage fault in the update gives `storage_error`, and each descriptor has `batch_aborted` (STS-ENV-5).
func TestStorageFaultAbortsTheBatch(t *testing.T) {
	mem := memstore.New()
	store := ops.New(ops.Writer, faulty{mem}, registry())
	before := mem.Fingerprint()
	e := store.Call(context.Background(), jsontext.S("put"), []byte(put([]string{source("alpha"), source("beta")}, "alpha", "beta")))
	if got := summary(e); got != "failed:storage_error | failed:batch_aborted | failed:batch_aborted" {
		t.Errorf("a storage fault: %s", got)
	}
	if mem.Fingerprint() != before {
		t.Error("a failed update changed the store")
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
	return summary(envelope.Envelope{Status: o.Status, Reasons: o.Reasons, Items: o.Items})
}

// Withheld bytes defer the payload checks of a descriptor (BUN-IMP-9). The store keeps a deferred record
// (BUN-IMP-10). When the bytes come, the store applies the checks before it adds the bytes: bytes that
// fail a check do not go into the store, and the descriptor that carries them has the reason.
func TestDeferredChecks(t *testing.T) {
	f := newFixture(t)
	whole := "the whole reading"
	wd := jsontext.Digest([]byte(whole))
	src := source(whole)
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
	e := f.call("put", put([]string{carrier}, whole))
	if got := summary(e); got != "failed:excerpt_mismatch,size_mismatch | failed:excerpt_mismatch,size_mismatch" {
		t.Errorf("bytes that fail a deferred check: %s", got)
	}
	if _, ok := r.Blob(wd); ok || f.mem.Fingerprint() != before {
		t.Error("bytes that fail a deferred check went into the store")
	}
}

// When the bytes of a correct descriptor come, the deferred record goes away, and an indexer gets the
// stored descriptor and the bytes in the update.
func TestDeferredBytesArrive(t *testing.T) {
	f := newFixture(t)
	whole := "the whole reading"
	wd := jsontext.Digest([]byte(whole))
	src := source(whole)
	if got := outcome(commit(f, []string{src}, nil, wd)); got != "ok | ok" {
		t.Fatal(got)
	}
	registry := registry()
	var indexed []string
	registry.OnBytes(func(tx ops.Tx, desc jsontext.Value, digest string, data []byte) {
		indexed = append(indexed, check.Kind(desc)+" "+string(data))
		if _, ok := tx.Blob(digest); !ok {
			t.Error("the indexer runs before the bytes are in the update")
		}
	})
	store := ops.New(ops.Writer, f.storage, registry)
	carrier := `{` + profile + `,"kind":"source","logical_id":"copy","payload":{"digest":"` + wd + `","media_type":"text/plain","size":17}}`
	e := store.Call(context.Background(), jsontext.S("put"), []byte(put([]string{carrier}, whole)))
	if got := summary(e); got != "ok | ok" {
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
func TestOpenContextAndGet(t *testing.T) {
	f := newFixture(t)
	src, other := source("alpha"), source("beta")
	set := `{` + profile + `,"kind":"binding-set"}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + revision(t, set) + `"}`
	f.call("put", put([]string{src, other, set, unit}, "alpha", "beta"))
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
		if got := summary(e); got != want || e.Context.Kind != jsontext.Null {
			t.Errorf("%s: %s, not %s", request, got, want)
		}
	}
	e := f.call("open_context", `{"roots":["`+snapshot+`"],"capabilities":["aavanam/text/v1","aavanam/data/v1"]}`)
	if got := summary(e); got != "ok | ok" || !regexp.MustCompile(`^ctx-[0-9a-f]{32}$`).MatchString(e.Context.Str) {
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
	if got := summary(e); got != "not_found:not_in_context,unknown_ref | ok | not_found:not_in_context | not_found:unknown_ref" || e.Context.Str != id {
		t.Errorf("get: %s", got)
	}
	for request, want := range map[string]string{
		`{"context":"` + id + `"}`:                           "failed:field_type",
		`{"context":"` + id + `","refs":[]}`:                 "failed:field_type",
		`{"context":"` + id + `","refs":["x"]}`:              "failed:field_type",
		`{"context":"ctx-none","refs":["` + snapshot + `"]}`: "failed:context_expired",
	} {
		if got := summary(f.call("get", request)); got != want {
			t.Errorf("%s: %s, not %s", request, got, want)
		}
	}
}
