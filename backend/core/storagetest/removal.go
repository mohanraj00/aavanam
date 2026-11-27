package storagetest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/text"
)

// The objects of the removal tests. Each text is a JSON descriptor whose typed reference fields give the
// `refs` of the object, because a device store reads the `refs` from the text (KND-DEP-2, DEV-LAY-4). The
// closure of the snapshot is the snapshot, the unit, the binding set and the artifact.
const (
	refArt   = refA
	refSet   = refB
	refUnit  = refC
	refSnap  = refD
	refLayer = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	digestP  = "sha256:1111111111111111111111111111111111111111111111111111111111111111" // the payload of the artifact
	digestQ  = "sha256:2222222222222222222222222222222222222222222222222222222222222222" // bytes that no object names

	textArt = `{"artifact_type":"note","descriptor_profile":"aavanam/base/v1","kind":"artifact","payload":{"digest":"` + digestP +
		`","media_type":"text/plain","size":7}}`
	textSet   = `{"bindings":{"a":"` + refArt + `"},"descriptor_profile":"aavanam/base/v1","kind":"binding-set"}`
	textUnit  = `{"binding_set":"` + refSet + `","descriptor_profile":"aavanam/base/v1","kind":"knowledge-unit"}`
	textSnap  = `{"collection":"col","descriptor_profile":"aavanam/base/v1","kind":"snapshot","units":["` + refUnit + `"]}`
	textLayer = `{"base":"base-col","descriptor_profile":"aavanam/layer/v1","kind":"layer","scope":"s","units":["` + refUnit + `"]}`
)

var (
	objectArt   = ops.Object{Ref: refArt, Kind: "artifact", Text: []byte(textArt)}
	objectSet   = ops.Object{Ref: refSet, Kind: "binding-set", Text: []byte(textSet), Targets: []string{refArt}}
	objectUnit  = ops.Object{Ref: refUnit, Kind: "knowledge-unit", Text: []byte(textUnit), Targets: []string{refSet}}
	objectSnap  = ops.Object{Ref: refSnap, Kind: "snapshot", Text: []byte(textSnap), Targets: []string{refUnit}, Collection: "col"}
	objectLayer = ops.Object{Ref: refLayer, Kind: "layer", Text: []byte(textLayer), Targets: []string{refUnit}, Collection: "base-col"}
)

// Removal runs the contract tests of the removal methods of a storage (REL-RMV-4, LIF-GC-5, CTX-LIF-2).
// `writer` tells if the storage is the storage of a writer store: it has a catalog and removal records.
// A device store and a local output store have none (REL-RMV-1, BUN-RMV-2).
func Removal(t *testing.T, open Open, writer bool) {
	t.Run("RemovalKeepsTheRefs", func(t *testing.T) { removalKeepsRefs(t, open(t), writer) })
	t.Run("Collection", func(t *testing.T) { collection(t, open(t), writer) })
	t.Run("ContextLife", func(t *testing.T) { contextLife(t, open(t)) })
	t.Run("RemovalIsAtomic", func(t *testing.T) { removalIsAtomic(t, open(t)) })
}

// lock takes the removal lock of a storage whose updates run at the same time (ops.RemovalGate).
func lock(tx ops.Tx) {
	if gate, has := tx.(ops.RemovalGate); has {
		gate.LockRemoval()
	}
}

// fill writes the objects, the bytes and the derived data of the removal tests.
func fill(s Backing) {
	const data = "one two"
	update(s, func(tx ops.Tx) {
		for _, o := range []ops.Object{objectArt, objectSet, objectUnit, objectSnap} { // ascending, as the core writes
			tx.PutObject(o)
		}
		tx.PutBlob(digestP, []byte(data))
		tx.PutBlob(digestQ, []byte("other"))
		tx.PutDeferred(digestQ, refSet)
		tx.PutDeferred(digestQ, refUnit)
		if index, ok := tx.(ops.TextIndex); ok {
			index.PutTextTokens(digestP, text.TokensOf([]byte(data))) // the document of the bytes (DEV-FTS-3)
		}
		if cache, ok := tx.(ops.RowCache); ok {
			cache.PutTable(digestP, []string{"key"}, [][]byte{[]byte(`["k0"]`)})
		}
	})
}

func refsOf(list []ops.Listed) []string {
	out := make([]string, len(list))
	for i, o := range list {
		out[i] = o.Ref
	}
	return out
}

// REL-RMV-4 and REL-RMV-8: a removal takes the descriptor, its bytes, its deferred records and the contexts
// that have it as a member. The `refs` of the object stay, so a closure has the same references.
func removalKeepsRefs(t *testing.T, s Backing, writer bool) {
	fill(s)
	update(s, func(tx ops.Tx) { tx.PutContext("before", ops.ContextRecord{Roots: []string{refSnap}}) })
	r := s.Session(context.Background())
	if got := r.ContextsWith(refSet); !reflect.DeepEqual(got, []string{"before"}) || len(r.ContextsWith(digestQ)) != 0 {
		t.Errorf("the contexts with a member: %v", got)
	}
	if got := r.Naming(digestP); !reflect.DeepEqual(got, []string{refArt}) || len(r.Naming(digestQ)) != 0 {
		t.Errorf("the objects whose text has a digest: %v", got)
	}
	if got := r.OwnBlobs(); !reflect.DeepEqual(got, []string{digestP, digestQ}) {
		t.Errorf("the bytes of the store: %v", got)
	}
	filled := s.Fingerprint()
	record := ops.RemovalRecord{Sequence: 1, Ref: refSet, Kind: "binding-set", Time: "2026-01-02T03:04:05Z", Reason: odd}
	update(s, func(tx ops.Tx) {
		lock(tx)
		tx.DeleteObjects([]string{refSet})
		tx.DeleteBlobs([]string{digestP})
		tx.DeleteDeferred([]string{refSet})
		if writer {
			tx.PutRemoval(record)
		}
		tx.DeleteContexts(tx.ContextsWith(refSet))
		if _, _, ok := tx.Object(refSet); ok {
			t.Error("an update reads a descriptor that it removed")
		}
	})
	if _, _, ok := r.Object(refSet); ok {
		t.Error("the store has the removed descriptor")
	}
	if _, _, ok := r.Object(refArt); !ok {
		t.Error("the removal took a different descriptor")
	}
	if got := r.Targets(refSet); !reflect.DeepEqual(got, []string{refArt}) {
		t.Errorf("the refs of the removed object (REL-RMV-8): %v", got)
	}
	if got := r.Sources(refArt); !reflect.DeepEqual(got, []string{refSet}) {
		t.Errorf("the sources of a target of the removed object: %v", got)
	}
	if index, ok := r.(ops.ClosureIndex); ok {
		if got := index.Closure(refSnap); !reflect.DeepEqual(got, []string{refArt, refSet, refUnit, refSnap}) {
			t.Errorf("the closure has different references after the removal: %v", got)
		}
	}
	if _, ok := r.Blob(digestP); ok {
		t.Error("the store has the removed bytes")
	}
	if data, ok := r.Blob(digestQ); !ok || string(data) != "other" {
		t.Error("the removal took different bytes")
	}
	if index, ok := r.(ops.TextIndex); ok && index.TextIndexed(digestP) {
		t.Error("the text index has the removed bytes")
	}
	if cache, ok := r.(ops.RowCache); ok {
		if _, _, has := cache.TableHead(digestP); has {
			t.Error("the row cache has the head of the removed bytes")
		}
		if _, has := cache.TableRow(digestP, 0); has || len(cache.TableRows(digestP)) != 0 {
			t.Error("the row cache has a row of the removed bytes")
		}
	}
	if r.HasDeferred(refSet) || !r.HasDeferred(refUnit) || !reflect.DeepEqual(r.DeferredRefs(digestQ), []string{refUnit}) {
		t.Errorf("the deferred records after the removal: %v", r.DeferredRefs(digestQ))
	}
	if _, ok := r.Context("before"); ok || len(r.Members("before")) != 0 || r.IsMember("before", refSnap) {
		t.Error("the store keeps a context that had the removed object as a member (CTX-LIF-4)")
	}
	want := []ops.Listed{{Ref: refArt, Kind: "artifact"}, {Ref: refUnit, Kind: "knowledge-unit"}, {Ref: refSnap, Kind: "snapshot"}}
	if got := r.OwnObjects(); !reflect.DeepEqual(got, want) {
		t.Errorf("the objects of the store: %v", got)
	}
	if got := r.OwnBlobs(); !reflect.DeepEqual(got, []string{digestQ}) {
		t.Errorf("the bytes of the store after the removal: %v", got)
	}
	if s.Fingerprint() == filled {
		t.Error("the removal did not change the fingerprint")
	}
	if !writer {
		if len(r.Removals()) != 0 || r.HasRemoval(refSet) || len(r.RemovedRefs()) != 0 {
			t.Error("a store that is not a writer store has a removal record (REL-RMV-1)")
		}
		return
	}
	if got := r.Removals(); !reflect.DeepEqual(got, []ops.RemovalRecord{record}) {
		t.Errorf("the removal records: %+v", got)
	}
	if !r.HasRemoval(refSet) || r.HasRemoval(refArt) || !reflect.DeepEqual(r.RemovedRefs(), []string{refSet}) {
		t.Errorf("the removed objects: %v", r.RemovedRefs())
	}
	// REL-RMV-8: a removed object is not a member, and an object that a root reaches only through it is one.
	update(s, func(tx ops.Tx) { tx.PutContext("after", ops.ContextRecord{Roots: []string{refSnap}}) })
	members := []string{refArt, refUnit, refSnap}
	if got := r.Members("after"); !reflect.DeepEqual(got, members) || r.IsMember("after", refSet) || !r.IsMember("after", refArt) {
		t.Errorf("the members of a context with a removed object in its closure: %v", got)
	}
	if len(r.ContextsWith(refSet)) != 0 || !reflect.DeepEqual(r.ContextsWith(refArt), []string{"after"}) {
		t.Errorf("the contexts with the removed object: %v", r.ContextsWith(refSet))
	}
	removed := s.Fingerprint()
	// REL-RMV-11: a second write adds the object again. The record stays, and an open context does not change.
	update(s, func(tx ops.Tx) {
		tx.PutObject(objectSet)
		tx.PutContext("later", ops.ContextRecord{Roots: []string{refSnap}})
	})
	if _, text, ok := r.Object(refSet); !ok || string(text) != textSet || !reflect.DeepEqual(r.Targets(refSet), []string{refArt}) {
		t.Error("a second write did not add the removed object again")
	}
	if !r.HasRemoval(refSet) || len(r.RemovedRefs()) != 0 || !reflect.DeepEqual(r.Removals(), []ops.RemovalRecord{record}) {
		t.Errorf("the removal record after the second write: %v", r.RemovedRefs())
	}
	if got := r.Members("after"); !reflect.DeepEqual(got, members) || r.IsMember("after", refSet) {
		t.Errorf("a second write changed the content of an open context (CTX-OPN-2): %v", got)
	}
	if got := r.Members("later"); !reflect.DeepEqual(got, []string{refArt, refSet, refUnit, refSnap}) || !r.IsMember("later", refSet) {
		t.Errorf("the members of a context after the second write: %v", got)
	}
	if print := s.Fingerprint(); print == removed || print == filled {
		t.Error("the fingerprint does not have the removal record")
	}
}

// LIF-GC-5: `gc_collect` takes objects with their `refs`, their stored closures, their deferred records,
// the bytes that no object names and the candidates of the removed snapshots.
func collection(t *testing.T, s Backing, writer bool) {
	fill(s)
	update(s, func(tx ops.Tx) {
		tx.PutObject(objectLayer)
		tx.PutContext("ended", ops.ContextRecord{Roots: []string{refSnap, refLayer}}) // the storage keeps the closures
		if writer {
			tx.PutCandidate(ops.Candidate{ID: refArt, Snapshot: refSnap, Collection: "col", Policy: "p"})
			tx.PutCandidate(ops.Candidate{ID: refSet, Snapshot: refSet, Collection: odd, Policy: "p"})
			tx.Publish(ops.Publication{Candidate: refArt, Receipt: refSet, Collection: "col", Head: refSnap})
			tx.Publish(ops.Publication{Candidate: refUnit, Receipt: refSet, Collection: odd, Head: refSet})
		}
		if log, ok := tx.(ops.ImportLog); ok {
			log.PutImport([]string{refSnap, refArt})
			log.PutImport([]string{refSnap})
			if got := log.ImportRoots(); !reflect.DeepEqual(got, []string{refArt, refSnap}) {
				t.Errorf("the imported roots: %v", got)
			}
		}
	})
	r := s.Session(context.Background())
	if !r.HasSnapshot("col") || !r.HasLayer("base-col") {
		t.Fatal("the store does not have the snapshot or the layer")
	}
	if writer {
		if got := r.AllHeads(); !reflect.DeepEqual(got, map[string]string{"col": refSnap, odd: refSet}) {
			t.Errorf("the heads of the store: %v", got)
		}
		if got := r.Candidates(); len(got) != 2 || got[0].ID != refArt || got[0].Snapshot != refSnap || got[1].Collection != odd {
			t.Errorf("the candidates of the store: %+v", got)
		}
	} else if len(r.AllHeads()) != 0 || len(r.Candidates()) != 0 {
		t.Error("a store with no catalog has a head or a candidate")
	}
	gone := []string{refUnit, refSnap, refLayer}
	update(s, func(tx ops.Tx) {
		lock(tx)
		tx.DeleteContexts([]string{"ended", "none"})
		tx.DeleteObjects(gone)
		tx.DeleteEdges(gone)
		tx.DeleteDeferred(gone)
		tx.DeleteBlobs([]string{digestQ, refArt}) // a digest that the store does not have changes nothing
		tx.DeleteCandidates(nil)
		if writer {
			tx.DeleteCandidates([]string{refArt})
		}
		tx.DeleteObjects(nil)
		tx.DeleteEdges(nil)
		tx.DeleteDeferred(nil)
		tx.DeleteBlobs(nil)
		tx.DeleteContexts(nil)
	})
	if r.HasSnapshot("col") || r.HasLayer("base-col") {
		t.Error("the store has a snapshot or a layer of a collection after their removal")
	}
	for _, ref := range gone {
		if _, _, ok := r.Object(ref); ok || len(r.Targets(ref)) != 0 {
			t.Errorf("the store has %s or its refs", ref)
		}
	}
	if len(r.Sources(refSet)) != 0 || !reflect.DeepEqual(r.Sources(refArt), []string{refSet}) {
		t.Errorf("the sources after the removal of the refs: %v, %v", r.Sources(refSet), r.Sources(refArt))
	}
	if index, ok := r.(ops.ClosureIndex); ok {
		if got := index.Closure(refSnap); !reflect.DeepEqual(got, []string{refSnap}) {
			t.Errorf("the store has a closure of a removed root: %v", got)
		}
		if got := index.Closure(refSet); !reflect.DeepEqual(got, []string{refArt, refSet}) {
			t.Errorf("the closure of a kept object: %v", got)
		}
	}
	if _, ok := r.Blob(digestQ); ok || r.HasDeferred(refUnit) || len(r.DeferredRefs(digestQ)) != 1 {
		t.Errorf("the bytes or the deferred records after the removal: %v", r.DeferredRefs(digestQ))
	}
	if data, ok := r.Blob(digestP); !ok || string(data) != "one two" {
		t.Error("the removal took the bytes of a kept object")
	}
	if got := refsOf(r.OwnObjects()); !reflect.DeepEqual(got, []string{refArt, refSet}) {
		t.Errorf("the objects of the store: %v", got)
	}
	if writer {
		if got := r.Candidates(); len(got) != 1 || got[0].ID != refSet {
			t.Errorf("the candidates after the removal: %+v", got)
		}
		if _, ok := r.Candidate(refArt); ok || !r.Published(refArt) || len(r.Publications("col")) != 1 {
			t.Error("the removal of a candidate took its publication record (PUB-LOG-2)")
		}
	}
	// A later write adds a removed object again, with its refs and a new closure (IDN-CAN-3).
	update(s, func(tx ops.Tx) {
		tx.PutObject(objectUnit)
		tx.PutObject(objectSnap)
		tx.PutContext("again", ops.ContextRecord{Roots: []string{refSnap}})
	})
	if got := r.Members("again"); !reflect.DeepEqual(got, []string{refArt, refSet, refUnit, refSnap}) || !r.HasSnapshot("col") {
		t.Errorf("the members of a context after the second write of the removed objects: %v", got)
	}
}

// CTX-LIF-1 and CTX-LIF-2: a storage keeps the end of the declared life of a context and finds the open
// contexts and the ended contexts at a time.
func contextLife(t *testing.T, s Backing) {
	fill(s)
	update(s, func(tx ops.Tx) {
		tx.PutContext("forever", ops.ContextRecord{Roots: []string{refSnap}, Capabilities: []string{"x"}})
		tx.PutContext("short", ops.ContextRecord{Roots: []string{refUnit, refArt}, Expires: 1000})
		tx.PutContext("long", ops.ContextRecord{Roots: []string{refArt}, Expires: 1_800_000_000_000})
	})
	r := s.Session(context.Background())
	for id, expires := range map[string]int64{"forever": 0, "short": 1000, "long": 1_800_000_000_000} {
		if record, ok := r.Context(id); !ok || record.Expires != expires {
			t.Errorf("the end of the life of the context %s: %+v", id, record)
		}
	}
	if got := r.OpenRoots(999); !reflect.DeepEqual(got, []string{refArt, refUnit, refSnap}) {
		t.Errorf("the roots of the open contexts before an end: %v", got)
	}
	if got := r.OpenRoots(1000); !reflect.DeepEqual(got, []string{refArt, refSnap}) { // the life ends at its last instant
		t.Errorf("the roots of the open contexts at an end: %v", got)
	}
	if got := r.OpenRoots(1_800_000_000_000); !reflect.DeepEqual(got, []string{refSnap}) {
		t.Errorf("the roots of the open contexts after each end: %v", got)
	}
	if len(r.EndedContexts(999)) != 0 || !reflect.DeepEqual(r.EndedContexts(1000), []string{"short"}) ||
		!reflect.DeepEqual(r.EndedContexts(1_800_000_000_000), []string{"long", "short"}) {
		t.Errorf("the ended contexts: %v", r.EndedContexts(1_800_000_000_000))
	}
	before := s.Fingerprint()
	update(s, func(tx ops.Tx) { tx.DeleteContexts(tx.EndedContexts(1000)) })
	if _, ok := r.Context("short"); ok || len(r.Members("short")) != 0 || len(r.EndedContexts(1000)) != 0 {
		t.Error("the store keeps an ended context after its removal")
	}
	if _, ok := r.Context("long"); !ok || len(r.Members("forever")) != 4 || !reflect.DeepEqual(r.EndedContexts(1_800_000_000_000), []string{"long"}) {
		t.Error("the removal of a context changed a different context")
	}
	if s.Fingerprint() != before {
		t.Error("the removal of a context changed the fingerprint")
	}
}

// LIF-GC-5 and STS-MAL-2: an update with removals gives the store each change or none.
func removalIsAtomic(t *testing.T, s Backing) {
	fill(s)
	update(s, func(tx ops.Tx) { tx.PutContext("kept", ops.ContextRecord{Roots: []string{refSnap}}) })
	before := s.Fingerprint()
	r := s.Session(context.Background())
	index, has := r.(ops.TextIndex)
	indexed := has && index.TextIndexed(digestP) // a local output store has no text index (DEV-OUT-3)
	stopped := errors.New("stop")
	err := Fault(func() {
		update(s, func(tx ops.Tx) {
			lock(tx)
			tx.DeleteObjects([]string{refSnap, refUnit})
			tx.DeleteEdges([]string{refSnap, refUnit})
			tx.DeleteDeferred([]string{refUnit})
			tx.DeleteBlobs([]string{digestP, digestQ})
			tx.DeleteContexts([]string{"kept"})
			ops.Raise(stopped)
		})
	})
	if !errors.Is(err, stopped) {
		t.Fatalf("the panic of the update did not continue: %v", err)
	}
	if _, _, ok := r.Object(refSnap); !ok || !reflect.DeepEqual(r.Targets(refSnap), []string{refUnit}) || !r.HasDeferred(refUnit) {
		t.Error("an update that did not commit removed an object, its refs or a deferred record")
	}
	if _, ok := r.Blob(digestP); !ok || len(r.Members("kept")) != 4 || s.Fingerprint() != before {
		t.Error("an update that did not commit removed bytes or a context")
	}
	if indexed && !index.TextIndexed(digestP) {
		t.Error("an update that did not commit removed a text index row")
	}
}
