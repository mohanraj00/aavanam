package synth

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

func build(t *testing.T) (*ops.Store, *memstore.Store, *Store) {
	t.Helper()
	backing := memstore.New()
	store := ops.New(ops.Writer, backing, conformance.Registry())
	s, err := Build(context.Background(), store, Tiny, 1)
	if err != nil {
		t.Fatal(err)
	}
	return store, backing, s
}

func call(t *testing.T, store *ops.Store, operation string, request any) map[string]any {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(store.Call(context.Background(), jsontext.S(operation), body).JSON(), &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ok" {
		t.Fatalf("%s: %v", operation, out)
	}
	return out
}

// Two builds with one shape and one seed make the same store, with the declared size.
func TestBuildIsDeterministic(t *testing.T) {
	_, first, a := build(t)
	_, second, b := build(t)
	if first.Fingerprint() != second.Fingerprint() || a.Summary() != b.Summary() {
		t.Fatalf("two builds differ: %+v, %+v", a.Summary(), b.Summary())
	}
	if a.Descriptors != Tiny.Descriptors || a.PayloadBytes != Tiny.PayloadBytes {
		t.Fatalf("the store has %d descriptors and %d bytes, not %d and %d",
			a.Descriptors, a.PayloadBytes, Tiny.Descriptors, Tiny.PayloadBytes)
	}
	if len(a.Heads) != Tiny.Collections || a.TextArtifacts != Tiny.TextArtifacts || len(a.Wide) == 0 || a.Matches == 0 {
		t.Fatalf("the store does not have the declared parts: %+v", a.Summary())
	}
}

// The reads of the bench give the counts that the store description states.
func TestReadsGiveTheStatedCounts(t *testing.T) {
	store, _, s := build(t)
	opened := call(t, store, "open_context", map[string]any{"heads": []string{s.Main()}})
	ctx := opened["context"]
	found := call(t, store, "search_text", map[string]any{"context": ctx, "query": Query})
	if n := len(found["items"].([]any)); n != s.Matches {
		t.Fatalf("search_text gave %d items, not %d", n, s.Matches)
	}
	rows := call(t, store, "query_data", map[string]any{"context": ctx, "resources": []string{s.Table}, "where": map[string]any{"group": Group}})
	item := rows["items"].([]any)[0].(map[string]any)
	if n := len(item["rows"].([]any)); n != s.GroupRows {
		t.Fatalf("query_data gave %d rows, not %d", n, s.GroupRows)
	}
	call(t, store, "read", map[string]any{"context": ctx, "ref": s.Wide[0]})
	call(t, store, "read", map[string]any{"context": ctx, "ref": s.Table,
		"selector": map[string]any{"selector_profile": "aavanam/row/v1", "index": Tiny.TableRows - 1}})
}

// An update and a publication of new units each give a new head of the main collection.
func TestUpdateAndAddUnits(t *testing.T) {
	store, _, s := build(t)
	head := s.Heads[0]
	added, err := Update(context.Background(), store, s)
	if err != nil {
		t.Fatal(err)
	}
	if added == 0 || s.Heads[0] == head {
		t.Fatalf("the update gave %d descriptors and the same head", added)
	}
	timing, err := AddUnits(context.Background(), store, s)
	if err != nil {
		t.Fatal(err)
	}
	if timing.Descriptors != 1000 || timing.Publish <= 0 {
		t.Fatalf("the publication has %d new descriptors", timing.Descriptors)
	}
	opened := call(t, store, "open_context", map[string]any{"heads": []string{s.Main()}})
	found := call(t, store, "search_text", map[string]any{"context": opened["context"], "query": Query})
	if n := len(found["items"].([]any)); n != s.Matches {
		t.Fatalf("search_text gave %d items after the update, not %d", n, s.Matches)
	}
}
