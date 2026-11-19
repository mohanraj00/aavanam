// Package storagetest has the tests that each storage of the core must pass: the contract of ops.Reader,
// ops.Tx and ops.Session, and the operations of the core on the storage. A storage package calls Storage
// and Core from its own tests.
package storagetest

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// Backing is a storage with the fingerprint of the conformance driver.
type Backing interface {
	ops.Storage
	Fingerprint() string
}

// Open returns an empty storage. It registers what the test must close with t.Cleanup.
type Open func(t *testing.T) Backing

const (
	refA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	refB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	refC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	refD = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

// A string of a request can have U+0000 and bytes that are not UTF-8. A storage keeps each such string.
const odd = "a\x00b\xed\xa0\x80\xff"

func update(s Backing, fn func(tx ops.Tx)) { s.Session(context.Background()).Update(fn) }

// Fault runs fn and returns the storage fault that stopped it.
func Fault(fn func()) (err error) {
	defer func() {
		if f, ok := recover().(*ops.Fault); ok {
			err = f
		}
	}()
	fn()
	return nil
}

// Storage runs the contract tests of a storage.
func Storage(t *testing.T, open Open) {
	t.Run("ContentAndCatalog", func(t *testing.T) { contentAndCatalog(t, open(t)) })
	t.Run("ProducerRecords", func(t *testing.T) { producerRecords(t, open(t)) })
	t.Run("UpdateIsAtomic", func(t *testing.T) { updateIsAtomic(t, open(t)) })
	t.Run("ConcurrentWritesOfOneBatch", func(t *testing.T) { concurrentWrites(t, open(t)) })
}

func contentAndCatalog(t *testing.T, s Backing) {
	empty := s.Fingerprint()
	update(s, func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: refA, Kind: "source", Text: []byte("A")})
		tx.PutObject(ops.Object{Ref: refC, Kind: "snapshot", Text: []byte("S"), Targets: []string{refB, refA}, Collection: odd})
		tx.PutObject(ops.Object{Ref: refA, Kind: "reading", Text: []byte("changed")}) // IDN-CAN-3: no change
		tx.PutBlob(refD, []byte("one"))
		tx.PutBlob(refD, []byte("two"))
		tx.PutBlob(refB, nil)
		tx.PutDeferred(refD, refC)
		tx.PutDeferred(refD, refA)
		tx.PutCandidate(ops.Candidate{ID: refA, Snapshot: refC, Collection: odd, Policy: odd})
		tx.PutCandidate(ops.Candidate{ID: refB, Snapshot: refC, Collection: odd, Policy: "p", ExpectedHead: refC})
		tx.Publish(ops.Publication{Candidate: refA, Receipt: refB, Collection: odd, Head: refC})
		tx.Publish(ops.Publication{Candidate: refB, Receipt: refB, Collection: "other", Head: refA})
		tx.Publish(ops.Publication{Candidate: refC, Receipt: refB, Activation: refD, Collection: odd, PreviousHead: refC, Head: refD})
		tx.PutContext(odd, ops.ContextRecord{Roots: []string{refC, refA}, Capabilities: []string{"x"}}, []string{refC, refA})
		tx.PutContext("empty", ops.ContextRecord{}, nil)
		tx.DeclareGate(odd)
		tx.DeclareGate(odd)
		tx.DeclareSection(odd, "serving")
		if _, _, ok := tx.Object(refC); !ok {
			t.Error("an update does not read its own write")
		}
	})
	r := s.Session(context.Background())
	if kind, text, ok := r.Object(refA); !ok || kind != "source" || string(text) != "A" {
		t.Errorf("a second write changed a descriptor: %s %s", kind, text)
	}
	if _, _, ok := r.Object("not a reference"); ok {
		t.Error("a string that is not a reference names an object")
	}
	if data, ok := r.Blob(refD); !ok || string(data) != "one" {
		t.Errorf("a second write changed bytes: %s", data)
	}
	if data, ok := r.Blob(refB); !ok || len(data) != 0 {
		t.Errorf("empty bytes: %v %v", data, ok)
	}
	if _, ok := r.Blob(refA); ok {
		t.Error("bytes of a digest that the store does not have")
	}
	if got := r.Targets(refC); !reflect.DeepEqual(got, []string{refA, refB}) {
		t.Errorf("targets: %v", got)
	}
	if got := r.Sources(refA); !reflect.DeepEqual(got, []string{refC}) || !r.HasSnapshot(odd) || r.HasSnapshot("other") {
		t.Errorf("sources: %v", got)
	}
	if got := r.DeferredRefs(refD); !reflect.DeepEqual(got, []string{refA, refC}) || !r.HasDeferred(refC) || r.HasDeferred(refB) {
		t.Errorf("deferred: %v", got)
	}
	if got := r.Heads([]string{odd, "none", "other"}); !reflect.DeepEqual(got, map[string]string{odd: refD, "other": refA}) {
		t.Errorf("heads: %v", got)
	}
	want := []ops.Publication{
		{Candidate: refA, Receipt: refB, Collection: odd, Head: refC},
		{Candidate: refC, Receipt: refB, Activation: refD, Collection: odd, PreviousHead: refC, Head: refD},
	}
	if got := r.Publications(odd); !reflect.DeepEqual(got, want) || !r.Published(refB) || r.Published(refD) {
		t.Errorf("publications in the order of the head updates: %+v", got)
	}
	if c, ok := r.Candidate(refA); !ok || c != (ops.Candidate{ID: refA, Snapshot: refC, Collection: odd, Policy: odd}) {
		t.Errorf("candidate: %+v", c)
	}
	if c, _ := r.Candidate(refB); c.ExpectedHead != refC {
		t.Errorf("candidate with an expected head: %+v", c)
	}
	record, ok := r.Context(odd)
	if !ok || !reflect.DeepEqual(record, ops.ContextRecord{Roots: []string{refC, refA}, Capabilities: []string{"x"}}) {
		t.Errorf("context: %+v", record)
	}
	if !r.IsMember(odd, refA) || r.IsMember(odd, refB) || r.IsMember("empty", refA) || !reflect.DeepEqual(r.Members(odd), []string{refA, refC}) {
		t.Error("the members of a context")
	}
	if record, ok := r.Context("empty"); !ok || len(record.Roots) != 0 || len(r.Members("empty")) != 0 {
		t.Errorf("a context with no member: %+v", record)
	}
	if _, ok := r.Context("none"); ok {
		t.Error("a context that the store does not have")
	}
	if section, ok := r.Section(odd); !ok || section != "serving" || !r.Gated(odd) || r.Gated("other") {
		t.Error("the declarations")
	}
	before := s.Fingerprint()
	if before == empty {
		t.Error("the content did not change the fingerprint")
	}
	update(s, func(tx ops.Tx) {
		tx.PutContext("later", ops.ContextRecord{Roots: []string{refA}}, []string{refA})
		tx.LockCollections([]string{"b", "a", odd})
		tx.LockDigests([]string{refB, refA})
	})
	if s.Fingerprint() != before {
		t.Error("a read context or a lock changed the fingerprint")
	}
	update(s, func(tx ops.Tx) { tx.ClearDeferred(refD) })
	if r.HasDeferred(refC) || s.Fingerprint() == before {
		t.Error("ClearDeferred")
	}
}

func producerRecords(t *testing.T, s Backing) {
	update(s, func(tx ops.Tx) {
		tx.PutProducerObject(odd, odd, []byte("v"))
		tx.PutProducerObject(odd, "empty", nil)
		tx.PutProducerObject(odd, "gone", []byte("v"))
		tx.DeleteProducerObject(odd, "gone")
		tx.PutProducerRemoval(odd, ops.Removal{Sequence: 1, Key: "gone", Time: "2026-01-02T03:04:05Z", Reason: odd})
		tx.PutProducerEntry(odd, odd, ops.Entry{Sequence: 1, Data: []byte("e1")})
		tx.PutProducerEntry(odd, odd, ops.Entry{Sequence: 2})
	})
	r := s.Session(context.Background())
	if data, ok := r.ProducerObject(odd, odd); !ok || string(data) != "v" {
		t.Error("producer object")
	}
	if data, ok := r.ProducerObject(odd, "empty"); !ok || len(data) != 0 {
		t.Error("an empty producer object")
	}
	if _, ok := r.ProducerObject(odd, "gone"); ok || !r.ProducerRemoved(odd, "gone") || r.ProducerRemoved(odd, odd) || r.ProducerRemoved("q", "gone") {
		t.Error("producer removal")
	}
	want := []ops.Removal{{Sequence: 1, Key: "gone", Time: "2026-01-02T03:04:05Z", Reason: odd}}
	if got := r.ProducerRemovals(odd); !reflect.DeepEqual(got, want) {
		t.Errorf("removals: %+v", got)
	}
	got := r.ProducerEntries(odd, odd)
	if len(got) != 2 || string(got[0].Data) != "e1" || len(got[1].Data) != 0 || len(r.ProducerEntries(odd, "other")) != 0 {
		t.Errorf("entries: %+v", got)
	}
	if counts, ok := r.(ops.ProducerCounts); ok {
		if counts.ProducerEntryCount(odd, odd) != 2 || counts.ProducerEntryCount(odd, "other") != 0 ||
			counts.ProducerRemovalCount(odd) != 1 || counts.ProducerRemovalCount("q") != 0 {
			t.Error("the counts of the producer records")
		}
	}
}

// An update gives the store each of its writes or none (STS-MAL-2). The fingerprint is equal before and
// after an update that did not commit.
func updateIsAtomic(t *testing.T, s Backing) {
	update(s, func(tx ops.Tx) { tx.PutObject(ops.Object{Ref: refA, Kind: "source", Text: []byte("A")}) })
	before := s.Fingerprint()
	stopped := errors.New("stop")
	err := Fault(func() {
		update(s, func(tx ops.Tx) {
			tx.PutObject(ops.Object{Ref: refB, Kind: "reading", Text: []byte("B"), Targets: []string{refA}})
			tx.PutBlob(refC, []byte("bytes"))
			tx.Publish(ops.Publication{Candidate: refA, Receipt: refB, Collection: "c", Head: refB})
			ops.Raise(stopped)
		})
	})
	if !errors.Is(err, stopped) {
		t.Fatalf("the panic of the update did not continue: %v", err)
	}
	r := s.Session(context.Background())
	if _, _, ok := r.Object(refB); ok || len(r.Sources(refA)) != 0 || len(r.Heads([]string{"c"})) != 0 || s.Fingerprint() != before {
		t.Error("an update that did not commit changed the store")
	}
}

// Updates of many goroutines that write the same descriptors and bytes all commit, and the store has each
// row one time.
func concurrentWrites(t *testing.T, s Backing) {
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for n := range errs {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			errs[n] = Fault(func() {
				update(s, func(tx ops.Tx) {
					tx.LockDigests([]string{refC, refD})
					for _, ref := range []string{refA, refB} { // ascending, as the core writes
						tx.PutObject(ops.Object{Ref: ref, Kind: "source", Text: []byte(ref), Targets: []string{refC}})
					}
					tx.PutBlob(refC, []byte("c"))
					tx.PutBlob(refD, []byte("d"))
				})
			})
		}(n)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	r := s.Session(context.Background())
	if _, text, ok := r.Object(refB); !ok || string(text) != refB || !reflect.DeepEqual(r.Sources(refC), []string{refA, refB}) {
		t.Error("a descriptor or an edge is not in the store")
	}
	if data, ok := r.Blob(refD); !ok || string(data) != "d" {
		t.Error("bytes are not in the store")
	}
}
