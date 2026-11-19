package storagetest

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// field returns the text of a field of an item: the string, or the canonical form of a different value.
func field(i envelope.Item, name string) string {
	for _, m := range i.Fields {
		if m.Key == name {
			if m.Val.Kind == jsontext.String {
				return m.Val.Str
			}
			return string(canon.Canonical(m.Val))
		}
	}
	return "absent"
}

// snapshotOf writes a unit with a parameter and returns a snapshot of the collection with that unit.
func snapshotOf(t *testing.T, f *fixture, collection, parameter string) string {
	t.Helper()
	set := `{` + profile + `,"kind":"binding-set","parameters":{"p":"` + parameter + `"}}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + revision(t, set) + `"}`
	if got := Summary(f.call("put", Put([]string{set, unit}))); !strings.HasPrefix(got, "ok") {
		t.Fatal(got)
	}
	e := f.call("create_snapshot", `{"collection":"`+collection+`","units":["`+revision(t, unit)+`"]}`)
	if e.Status != "ok" {
		t.Fatal(Summary(e))
	}
	return field(e.Items[0], "ref")
}

// receiptOf writes a receipt and returns its reference.
func receiptOf(t *testing.T, f *fixture) string {
	t.Helper()
	receipt := `{` + profile + `,"kind":"receipt","producer":"p","payload":{"digest":"` + jsontext.Digest([]byte("r")) +
		`","media_type":"text/plain","size":1}}`
	if got := Summary(f.call("put", Put([]string{receipt}, "r"))); !strings.HasPrefix(got, "ok") {
		t.Fatal(got)
	}
	return revision(t, receipt)
}

// candidateOf makes a candidate and returns its identity. An empty `expected` is no head.
func candidateOf(t *testing.T, f *fixture, snapshot, expected string) string {
	t.Helper()
	head := "null"
	if expected != "" {
		head = quote(expected)
	}
	e := f.call("create_candidate", `{"snapshot":"`+snapshot+`","policy":"pol","expected_head":`+head+`}`)
	if e.Status != "ok" {
		t.Fatal(Summary(e))
	}
	return field(e.Items[0], "candidate")
}

func publish(f *fixture, candidate, receipt string) envelope.Envelope {
	return f.call("publish", `{"candidate":"`+candidate+`","receipt":"`+receipt+`"}`)
}

// PUB-CND-3, PUB-HED-3 to PUB-HED-5, PUB-RCP-2 to PUB-RCP-4 and PUB-LOG-2, PUB-LOG-3.
func publication(t *testing.T, f *fixture) {
	decision, calls := ops.Accepted, 0
	during := func() {}
	f.store.Accept("pol", func(_ context.Context, _, _, _ string) ops.Decision {
		calls++
		during()
		return decision
	})
	s1, s2, s3 := snapshotOf(t, f, "c", "1"), snapshotOf(t, f, "c", "2"), snapshotOf(t, f, "c", "3")
	receipt := receiptOf(t, f)

	first := candidateOf(t, f, s1, "")
	want := canon.Revision(jsontext.O(jsontext.M("format", jsontext.S("aavanam-candidate/v1")), jsontext.M("snapshot", jsontext.S(s1)),
		jsontext.M("checks", jsontext.A(nil)), jsontext.M("policy", jsontext.S("pol")), jsontext.M("expected_head", jsontext.Value{})))
	if first != want {
		t.Errorf("the identity of a candidate (PUB-CND-3): %s, not %s", first, want)
	}

	before := f.mem.Fingerprint()
	decision = ops.NoAnswer
	if got := Summary(publish(f, first, receipt)); got != "failed:deadline_exceeded" {
		t.Errorf("a validator with no answer (PUB-RCP-4): %s", got)
	}
	decision = ops.Rejected
	if got := Summary(publish(f, first, receipt)); got != "incompatible:receipt_rejected | incompatible:receipt_rejected" {
		t.Errorf("a validator that does not accept: %s", got)
	}
	if calls != 2 || f.mem.Fingerprint() != before {
		t.Errorf("a publish with no acceptance: %d validator calls, store changed: %v", calls, f.mem.Fingerprint() != before)
	}
	decision = ops.Accepted
	e := publish(f, first, receipt)
	if got := Summary(e); got != "ok | ok" || field(e.Items[0], "disposition") != "published" || field(e.Items[0], "head") != s1 {
		t.Errorf("the first publication: %s %+v", got, e.Items)
	}
	e = publish(f, first, receipt)
	if got := Summary(e); got != "ok | ok" || field(e.Items[0], "disposition") != "already_published" || calls != 3 {
		t.Errorf("a repeat publish (PUB-HED-5): %s %+v, %d validator calls", got, e.Items, calls)
	}

	// A competing publish changes the head while the validator of the second candidate runs. The store
	// keeps no update open in that time (PUB-RCP-3), so the competing call ends, and the second gets
	// `head_moved` with the current head.
	second, third := candidateOf(t, f, s2, s1), candidateOf(t, f, s3, s1)
	during = func() {
		during = func() {}
		if got := Summary(publish(f, third, receipt)); got != "ok | ok" {
			t.Errorf("the competing publish: %s", got)
		}
	}
	e = publish(f, second, receipt)
	if got := Summary(e); got != "incompatible:head_moved | incompatible:head_moved" || field(e.Items[0], "head") != s3 {
		t.Errorf("a head that moved in the validation: %s %+v", got, e.Items)
	}
	e = publish(f, first, receipt)
	if field(e.Items[0], "disposition") != "superseded" || field(e.Items[0], "head") != s3 {
		t.Errorf("a published candidate after a later publication (PUB-HED-4): %+v", e.Items)
	}

	e = f.call("publications", `{"collection":"c"}`)
	if got := Summary(e); got != "ok | ok | ok" {
		t.Fatalf("the publications: %s", got)
	}
	for i, want := range [][4]string{{first, "null", "null", s1}, {third, "null", s1, s3}} {
		item := e.Items[i]
		got := [4]string{field(item, "candidate"), field(item, "activation"), field(item, "previous_head"), field(item, "head")}
		if got != want || field(item, "receipt") != receipt {
			t.Errorf("publication %d (PUB-LOG-2): %v, not %v", i+1, got, want)
		}
	}
	if got := Summary(f.call("publications", `{"collection":"other"}`)); got != "ok" {
		t.Errorf("a collection with no publication: %s", got)
	}
}

// PUB-GAT-3: an open declares a gate while a publish of a different open runs its validator. The atomic
// step applies the gate check again, and the store does not change.
func gateDuringValidation(t *testing.T, f *fixture) {
	snapshot, receipt := snapshotOf(t, f, "g", "1"), receiptOf(t, f)
	f.store.Accept("pol", func(ctx context.Context, _, _, _ string) ops.Decision {
		if err := ops.New(ops.Writer, f.storage, Registry()).Gate(ctx, "g"); err != nil {
			t.Error(err)
		}
		return ops.Accepted
	})
	candidate := candidateOf(t, f, snapshot, "")
	before := f.mem.Fingerprint()
	if got := Summary(publish(f, candidate, receipt)); got != "incompatible:activation_required | incompatible:activation_required" {
		t.Errorf("a gate that starts in the validation: %s", got)
	}
	if f.mem.Fingerprint() != before {
		t.Error("a refused publish changed the store")
	}
	if got := Summary(f.call("publications", `{"collection":"g"}`)); got != "ok" {
		t.Errorf("a refused publish recorded a publication: %s", got)
	}
}

// Two calls publish two candidates with one expected head at the same time. One call publishes its
// candidate, and the other gets `head_moved` (PUB-HED-3 step 5, PUB-RCP-3).
func concurrentPublish(t *testing.T, f *fixture) {
	f.store.Accept("pol", func(context.Context, string, string, string) ops.Decision { return ops.Accepted })
	receipt := receiptOf(t, f)
	candidates := []string{candidateOf(t, f, snapshotOf(t, f, "c", "1"), ""), candidateOf(t, f, snapshotOf(t, f, "c", "2"), "")}
	got := together(len(candidates), func(n int) string { return Summary(publish(f, candidates[n], receipt)) })
	if got["ok | ok"] != 1 || got["incompatible:head_moved | incompatible:head_moved"] != 1 {
		t.Errorf("two publications with one expected head: %v", got)
	}
	if e := f.call("publications", `{"collection":"c"}`); len(e.Items) != 1 {
		t.Errorf("the store recorded %d publications", len(e.Items))
	}
}

// together runs fn in n calls at the same time and counts each result.
func together(n int, fn func(n int) string) map[string]int {
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := map[string]int{}
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			result := fn(i)
			mu.Lock()
			out[result]++
			mu.Unlock()
		}(i)
	}
	close(start)
	wg.Wait()
	return out
}

// PRD-OBJ-1 to PRD-OBJ-4, PRD-LOG-1 to PRD-LOG-3 and PRD-RMV-1 to PRD-RMV-4, with the store clock.
func producer(t *testing.T, f *fixture) {
	f.store.Clock = func() time.Time { return time.Date(2026, 3, 4, 1, 6, 7, 999, time.FixedZone("east", 5*3600)) }
	data := func(text string) string { return quote(ops.B64([]byte(text))) }
	steps := []struct{ operation, request, want, name, value string }{
		{"producer_put", `{"producer":"p","key":"k","data":` + data("one") + `}`, "ok | ok", "size", "3"},
		{"producer_put", `{"producer":"p","key":"k","data":` + data("one") + `}`, "ok | ok", "size", "3"},
		{"producer_put", `{"producer":"p","key":"k","data":` + data("two") + `}`, "incompatible:producer_key_conflict", "", ""},
		{"producer_get", `{"producer":"p","key":"k"}`, "ok | ok", "data", ops.B64([]byte("one"))},
		{"producer_get", `{"producer":"q","key":"k"}`, "not_found:unknown_producer_key", "", ""},
		{"producer_forget", `{"producer":"p","key":"k","reason":"a request"}`, "ok | ok", "time", "2026-03-03T20:06:07Z"},
		{"producer_forget", `{"producer":"p","key":"k","reason":"a request"}`, "not_found:producer_removed", "", ""},
		{"producer_get", `{"producer":"p","key":"k"}`, "not_found:producer_removed", "", ""},
		{"producer_put", `{"producer":"p","key":"k","data":` + data("two") + `}`, "ok | ok", "size", "3"},
		{"producer_forget", `{"producer":"p","key":"k","reason":"again"}`, "ok | ok", "sequence", "2"},
		{"producer_removals", `{"producer":"p"}`, "ok | ok | ok", "reason", "a request"},
		{"producer_removals", `{"producer":"q"}`, "ok", "", ""},
		{"producer_append", `{"producer":"p","log":["a","b"],"sequence":2,"entry":` + data("e") + `}`, "incompatible:log_moved", "", ""},
		{"producer_append", `{"producer":"p","log":["a","b"],"sequence":1,"entry":` + data("e") + `}`, "ok | ok", "sequence", "1"},
		{"producer_append", `{"producer":"p","log":["a","b"],"sequence":1,"entry":` + data("e") + `}`, "incompatible:log_moved", "", ""},
		{"producer_append", `{"producer":"p","log":["a","b"],"sequence":2,"entry":` + data("") + `}`, "ok | ok", "sequence", "2"},
		{"producer_append", `{"producer":"p","log":["a,b"],"sequence":1,"entry":` + data("x") + `}`, "ok | ok", "sequence", "1"},
		{"producer_append", `{"producer":"p","log":["a"],"sequence":1,"entry":` + data("y") + `}`, "ok | ok", "sequence", "1"},
		{"producer_read", `{"producer":"p","log":["a","b"]}`, "ok | ok | ok", "entry", ops.B64([]byte("e"))},
		{"producer_read", `{"producer":"p","log":["b","a"]}`, "ok", "", ""},
	}
	for i, step := range steps {
		e := f.call(step.operation, step.request)
		if got := Summary(e); got != step.want {
			t.Errorf("step %d, %s %s: %s, not %s", i+1, step.operation, step.request, got, step.want)
		} else if step.name != "" && field(e.Items[0], step.name) != step.value {
			t.Errorf("step %d, %s: the field %s is %s, not %s", i+1, step.operation, step.name, field(e.Items[0], step.name), step.value)
		}
	}
}

// Two writers of one producer at the same time: only one adds a sequence number (PRD-LOG-2), only one
// adds an object of a key (PRD-OBJ-2), and only one removes it (PRD-RMV-1). No call gives a storage fault.
func concurrentProducer(t *testing.T, f *fixture) {
	const writers = 6
	for round := 1; round <= 3; round++ {
		got := together(writers, func(int) string {
			return Summary(f.call("producer_append", `{"producer":"p","log":["l"],"sequence":`+jsontext.ESNumber(float64(round))+`,"entry":"ZQ=="}`))
		})
		if got["ok | ok"] != 1 || got["incompatible:log_moved"] != writers-1 {
			t.Errorf("appends of sequence %d at the same time: %v", round, got)
		}
	}
	got := together(writers, func(n int) string {
		return Summary(f.call("producer_put", `{"producer":"p","key":"k","data":`+quote(ops.B64([]byte{byte(n)}))+`}`))
	})
	if got["ok | ok"] != 1 || got["incompatible:producer_key_conflict"] != writers-1 {
		t.Errorf("puts of one key with different bytes at the same time: %v", got)
	}
	got = together(writers, func(int) string {
		return Summary(f.call("producer_forget", `{"producer":"p","key":"k","reason":"r"}`))
	})
	if got["ok | ok"] != 1 || got["not_found:producer_removed"] != writers-1 {
		t.Errorf("removals of one key at the same time: %v", got)
	}
	if e := f.call("producer_removals", `{"producer":"p"}`); len(e.Items) != 1 || field(e.Items[0], "sequence") != "1" {
		t.Errorf("the removal records: %+v", e.Items)
	}
}
