package memstore

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
)

func update(s *Store, fn func(tx ops.Tx)) { s.Session(context.Background()).Update(fn) }

// An update gives the store each of its writes or none (STS-MAL-2).
func TestUpdateIsAtomic(t *testing.T) {
	s := New()
	update(s, func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: "a", Kind: "source", Text: []byte(`{"k":1}`)})
	})
	before := s.Fingerprint()
	func() {
		defer func() { recover() }()
		update(s, func(tx ops.Tx) {
			tx.PutObject(ops.Object{Ref: "b", Kind: "reading", Text: []byte(`{}`), Targets: []string{"a"}})
			tx.PutBlob("d", []byte("bytes"))
			tx.PutDeferred("d2", "b")
			tx.Publish(ops.Publication{Candidate: "c", Collection: "col", Head: "b"})
			if _, _, ok := tx.Object("b"); !ok {
				t.Error("an update does not read its own write")
			}
			panic("stop")
		})
	}()
	r := s.Session(context.Background())
	if _, _, ok := r.Object("b"); ok || len(r.Sources("a")) != 0 || s.Fingerprint() != before {
		t.Error("an update that did not commit changed the store")
	}
	if _, ok := r.Blob("d"); ok || r.HasDeferred("b") || len(r.Heads([]string{"col"})) != 0 {
		t.Error("an update that did not commit changed the store")
	}
}

func TestContentAndCatalog(t *testing.T) {
	s := New()
	update(s, func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: "a", Kind: "source", Text: []byte("A")})
		tx.PutObject(ops.Object{Ref: "s", Kind: "snapshot", Text: []byte("S"), Targets: []string{"b", "a"}, Collection: "col"})
		tx.PutObject(ops.Object{Ref: "a", Kind: "other", Text: []byte("changed")}) // IDN-CAN-3: no change
		tx.PutBlob("d", []byte("one"))
		tx.PutBlob("d", []byte("two"))
		tx.PutDeferred("w", "s")
		tx.PutDeferred("w", "a")
		tx.PutCandidate(ops.Candidate{ID: "c1", Snapshot: "s", Collection: "col", Policy: "p"})
		tx.Publish(ops.Publication{Candidate: "c1", Receipt: "r", Collection: "col", Head: "s"})
		tx.Publish(ops.Publication{Candidate: "c2", Receipt: "r", Collection: "other", Head: "x"})
		tx.Publish(ops.Publication{Candidate: "c3", Receipt: "r", Collection: "col", PreviousHead: "s", Head: "s2"})
		tx.PutContext("ctx", ops.ContextRecord{Roots: []string{"s"}, Capabilities: []string{"x"}}, []string{"s", "a"})
	})
	r := s.Session(context.Background())
	if kind, text, _ := r.Object("a"); kind != "source" || string(text) != "A" {
		t.Errorf("a second write changed a descriptor: %s %s", kind, text)
	}
	if data, _ := r.Blob("d"); string(data) != "one" {
		t.Errorf("a second write changed bytes: %s", data)
	}
	if got := r.Targets("s"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("targets: %v", got)
	}
	if got := r.Sources("a"); !reflect.DeepEqual(got, []string{"s"}) || !r.HasSnapshot("col") || r.HasSnapshot("other") {
		t.Errorf("sources: %v", got)
	}
	if got := r.DeferredRefs("w"); !reflect.DeepEqual(got, []string{"a", "s"}) || !r.HasDeferred("s") || r.HasDeferred("x") {
		t.Errorf("deferred: %v", got)
	}
	if got := r.Heads([]string{"col", "none", "other"}); !reflect.DeepEqual(got, map[string]string{"col": "s2", "other": "x"}) {
		t.Errorf("heads: %v", got)
	}
	log := r.Publications("col")
	if len(log) != 2 || log[0].Candidate != "c1" || log[1].PreviousHead != "s" || !r.Published("c2") || r.Published("c9") {
		t.Errorf("publications in the order of the head updates: %+v", log)
	}
	if c, ok := r.Candidate("c1"); !ok || c.Policy != "p" {
		t.Errorf("candidate: %+v", c)
	}
	if record, ok := r.Context("ctx"); !ok || record.Roots[0] != "s" || !r.IsMember("ctx", "a") || r.IsMember("ctx", "b") ||
		!reflect.DeepEqual(r.Members("ctx"), []string{"a", "s"}) {
		t.Errorf("context: %+v", record)
	}
	before := s.Fingerprint()
	update(s, func(tx ops.Tx) {
		tx.ClearDeferred("w")
		tx.PutContext("ctx2", ops.ContextRecord{}, nil)
		tx.DeclareGate("col")
	})
	if r.HasDeferred("s") || len(r.DeferredRefs("w")) != 0 {
		t.Error("ClearDeferred kept a record")
	}
	if s.Fingerprint() == before {
		t.Error("a removed deferred record did not change the fingerprint")
	}
	before = s.Fingerprint()
	update(s, func(tx ops.Tx) { tx.PutContext("ctx3", ops.ContextRecord{}, []string{"a"}) })
	if s.Fingerprint() != before {
		t.Error("a read context changed the fingerprint")
	}
	s.Reset()
	if _, _, ok := r.Object("a"); ok || s.Fingerprint() == before {
		t.Error("Reset kept content")
	}
}

func TestProducerRecords(t *testing.T) {
	s := New()
	update(s, func(tx ops.Tx) {
		tx.PutProducerObject("p", "k", []byte("v"))
		tx.PutProducerObject("p", "gone", []byte("v"))
		tx.DeleteProducerObject("p", "gone")
		tx.PutProducerRemoval("p", ops.Removal{Sequence: 1, Key: "gone", Time: "t", Reason: "r"})
		tx.PutProducerEntry("p", "log", ops.Entry{Sequence: 1, Data: []byte("e1")})
		tx.PutProducerEntry("p", "log", ops.Entry{Sequence: 2, Data: []byte("e2")})
	})
	r := s.Session(context.Background())
	if data, ok := r.ProducerObject("p", "k"); !ok || string(data) != "v" {
		t.Error("producer object")
	}
	if _, ok := r.ProducerObject("p", "gone"); ok || !r.ProducerRemoved("p", "gone") || r.ProducerRemoved("p", "k") || r.ProducerRemoved("q", "gone") {
		t.Error("producer removal")
	}
	if got := r.ProducerRemovals("p"); len(got) != 1 || got[0].Key != "gone" {
		t.Errorf("removals: %+v", got)
	}
	if got := r.ProducerEntries("p", "log"); len(got) != 2 || string(got[1].Data) != "e2" || len(r.ProducerEntries("p", "other")) != 0 {
		t.Errorf("entries: %+v", got)
	}
}

// A local output store reads the content of its device store, and keeps only what the device store does
// not have (BUN-IMP-8).
func TestOutputStoreReadsItsDeviceStore(t *testing.T) {
	device := New()
	output := device.Output()
	update(device, func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: "a", Kind: "source", Text: []byte("A")})
		tx.PutObject(ops.Object{Ref: "b", Kind: "reading", Text: []byte("B"), Targets: []string{"a"}})
		tx.PutBlob("d", []byte("device bytes"))
		tx.PutDeferred("w", "b")
	})
	empty := output.Fingerprint()
	update(output, func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: "a", Kind: "source", Text: []byte("A")})
		tx.PutBlob("d", []byte("device bytes"))
	})
	if output.Fingerprint() != empty {
		t.Error("the output store kept content that its device store has")
	}
	update(output, func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: "lock", Kind: "execution-lock", Text: []byte("L"), Targets: []string{"a"}})
		tx.PutDeferred("w", "lock")
	})
	r := output.Session(context.Background())
	if _, _, ok := r.Object("b"); !ok {
		t.Error("the output store does not read a device descriptor")
	}
	if data, ok := r.Blob("d"); !ok || string(data) != "device bytes" {
		t.Error("the output store does not read device bytes")
	}
	if got := r.Sources("a"); !reflect.DeepEqual(got, []string{"b", "lock"}) {
		t.Errorf("sources over the two stores: %v", got)
	}
	if got := r.Targets("b"); !reflect.DeepEqual(got, []string{"a"}) || !r.HasDeferred("b") {
		t.Errorf("targets of a device descriptor: %v", got)
	}
	if got := r.DeferredRefs("w"); !reflect.DeepEqual(got, []string{"b", "lock"}) {
		t.Errorf("deferred records over the two stores: %v", got)
	}
	if _, _, ok := device.Session(context.Background()).Object("lock"); ok {
		t.Error("the device store has a descriptor of its output store")
	}
}

// Updates of different goroutines do not mix (the race detector checks the locks).
func TestUpdatesFromManyGoroutines(t *testing.T) {
	device := New()
	output := device.Output()
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				ref := string(rune('a'+n)) + string(rune('0'+i%10))
				update(device, func(tx ops.Tx) { tx.PutObject(ops.Object{Ref: ref, Kind: "source", Text: []byte(ref)}) })
				update(output, func(tx ops.Tx) { tx.PutObject(ops.Object{Ref: "o" + ref, Kind: "source", Text: []byte(ref)}) })
				output.Session(context.Background()).Object(ref)
				device.Fingerprint()
			}
		}(n)
	}
	wg.Wait()
	if _, _, ok := output.Session(context.Background()).Object("h9"); !ok {
		t.Error("a write is not in the store")
	}
}

// The contract that each storage of the core passes.
func TestStorageContract(t *testing.T) {
	storagetest.Storage(t, func(*testing.T) storagetest.Backing { return New() })
}
