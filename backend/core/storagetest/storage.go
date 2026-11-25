// Package storagetest has the tests that each storage of the core must pass: the contract of ops.Reader,
// ops.Tx and ops.Session, and the operations of the core on the storage. A storage package calls Storage
// and Core from its own tests.
package storagetest

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
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
	t.Run("Layers", func(t *testing.T) { layers(t, open(t)) })
	t.Run("ProducerRecords", func(t *testing.T) { producerRecords(t, open(t)) })
	t.Run("LongNames", func(t *testing.T) { longNames(t, open(t)) })
	t.Run("UpdateIsAtomic", func(t *testing.T) { updateIsAtomic(t, open(t)) })
	t.Run("ConcurrentWritesOfOneBatch", func(t *testing.T) { concurrentWrites(t, open(t)) })
	t.Run("DerivedData", func(t *testing.T) { derivedData(t, open(t)) })
}

// derivedData checks the optional parts of a storage that keep derived data: the text index
// (ops.TextIndex) and the row cache (ops.RowCache). A storage with no such part passes.
func derivedData(t *testing.T, s Backing) {
	token := func(n int) string { return fmt.Sprintf("t%016x", uint64(n)*0x9e3779b97f4a7c15) }
	// A column name is a string of a JSON text in UTF-8 (DAT-TBL-1). It can have U+0000.
	column := "a\x00b\u00e9"
	rows := [][]byte{[]byte(`["k0",1]`), []byte(`["k1",null]`), []byte(`["k2","\u0000"]`)}
	update(s, func(tx ops.Tx) {
		tx.PutBlob(refA, []byte("one two two"))
		tx.PutBlob(refB, []byte("three"))
		tx.PutBlob(refC, []byte(""))
		if index, ok := tx.(ops.TextIndex); ok {
			index.PutTextTokens(refA, []string{token(1), token(2), token(2)})
			index.PutTextTokens(refA, []string{token(9)}) // a second call has no effect
			index.PutTextTokens(refC, nil)                // bytes with no word
			var many []string
			for n := 100; n < 5100; n++ {
				many = append(many, token(n))
			}
			index.PutTextTokens(refB, many)
			if !index.TextIndexed(refA) {
				t.Error("an update does not read its own tokens")
			}
		}
		if cache, ok := tx.(ops.RowCache); ok {
			cache.PutTable(refA, []string{"key", column}, rows)
			cache.PutTable(refA, []string{"other"}, nil) // a second call has no effect
			cache.PutTable(refB, []string{"only"}, nil)  // a table with no row keeps its columns
		}
	})
	r := s.Session(context.Background())
	if index, ok := r.(ops.TextIndex); ok {
		all := []string{refA, refB, refC, refD}
		check := func(name string, tokens, digests, candidates, unindexed []string) {
			t.Helper()
			can, not := index.TextCandidates(tokens, digests)
			sort.Strings(can)
			sort.Strings(not)
			if !reflect.DeepEqual(can, candidates) || !reflect.DeepEqual(not, unindexed) {
				t.Errorf("%s: candidates %v, not in the index %v", name, can, not)
			}
		}
		// A candidate set can have more digests, but these tokens are far apart in each implementation.
		check("one token", []string{token(2)}, all, []string{refA}, []string{refD})
		check("two tokens", []string{token(2), token(1)}, all, []string{refA}, []string{refD})
		check("a token of a different digest", []string{token(1), token(100)}, all, nil, []string{refD})
		check("a token of a large digest", []string{token(5099), token(100)}, all, []string{refB}, []string{refD})
		check("the token of the second call", []string{token(9)}, all, nil, []string{refD})
		check("a part of the list", []string{token(1)}, []string{refB, refC}, nil, nil)
		check("an empty list", []string{token(1)}, nil, nil, nil)
		if !index.TextIndexed(refA) || !index.TextIndexed(refC) || index.TextIndexed(refD) {
			t.Error("the indexed digests")
		}
	}
	if cache, ok := r.(ops.RowCache); ok {
		columns, count, has := cache.TableHead(refA)
		if !has || count != 3 || !reflect.DeepEqual(columns, []string{"key", column}) {
			t.Errorf("the head of a table: %q %d %v", columns, count, has)
		}
		if columns, count, has := cache.TableHead(refB); !has || count != 0 || !reflect.DeepEqual(columns, []string{"only"}) {
			t.Errorf("the head of a table with no row: %q %d %v", columns, count, has)
		}
		if _, _, has := cache.TableHead(refC); has {
			t.Error("a digest that the cache does not have")
		}
		for n, want := range rows {
			if got, ok := cache.TableRow(refA, n); !ok || string(got) != string(want) {
				t.Errorf("row %d: %q %v", n, got, ok)
			}
		}
		if _, ok := cache.TableRow(refA, 3); ok {
			t.Error("a row after the last row")
		}
		if _, ok := cache.TableRow(refB, 0); ok {
			t.Error("a row of a table with no row")
		}
		got := cache.TableRows(refA)
		if len(got) != 3 || string(got[0]) != string(rows[0]) || string(got[2]) != string(rows[2]) {
			t.Errorf("the rows of a table: %q", got)
		}
		if len(cache.TableRows(refB)) != 0 {
			t.Error("the rows of a table with no row")
		}
	}
}

// LYR-DEF-5: a storage finds the layers of a base collection. A layer is not a snapshot of its base.
func layers(t *testing.T, s Backing) {
	update(s, func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: refA, Kind: "snapshot", Text: []byte("S"), Collection: "only-snapshot"})
		tx.PutObject(ops.Object{Ref: refB, Kind: "layer", Text: []byte("L"), Targets: []string{refA}, Collection: odd})
		tx.PutObject(ops.Object{Ref: refC, Kind: "source", Text: []byte("A"), Collection: "not-a-layer"})
		if !tx.HasLayer(odd) {
			t.Error("an update does not read the layer that it wrote")
		}
	})
	r := s.Session(context.Background())
	if !r.HasLayer(odd) || r.HasLayer("only-snapshot") || r.HasLayer("not-a-layer") || r.HasLayer("") {
		t.Error("the layers of a base collection are not correct")
	}
	if r.HasSnapshot(odd) || !r.HasSnapshot("only-snapshot") {
		t.Error("a layer is a snapshot of its base collection")
	}
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
		tx.PutContext(odd, ops.ContextRecord{Roots: []string{refC, refA}, Capabilities: []string{"x"}})
		tx.PutContext("empty", ops.ContextRecord{})
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
	// The content of a context is the closure of its roots (CTX-OPN-2, KND-DEP-3): refC has refA and refB.
	if !r.IsMember(odd, refA) || !r.IsMember(odd, refB) || r.IsMember(odd, refD) || r.IsMember("empty", refA) ||
		!reflect.DeepEqual(r.Members(odd), []string{refA, refB, refC}) {
		t.Errorf("the members of a context: %v", r.Members(odd))
	}
	if index, ok := r.(ops.ClosureIndex); ok {
		if got := index.Closure(refC); !reflect.DeepEqual(got, []string{refA, refB, refC}) {
			t.Errorf("the closure of a root with a stored closure: %v", got)
		}
		if got := index.Closure(refD); !reflect.DeepEqual(got, []string{refD}) {
			t.Errorf("the closure of a root with no object: %v", got)
		}
	}
	if batch, ok := r.(ops.BatchReader); ok {
		texts := batch.Objects([]string{refA, refB, refC, "not a reference"})
		if len(texts) != 2 || string(texts[refA]) != "A" || string(texts[refC]) != "S" {
			t.Errorf("the descriptors of a list: %q", texts)
		}
		if got := batch.Deferred([]string{refA, refB, refC}); !reflect.DeepEqual(got, map[string]bool{refA: true, refC: true}) {
			t.Errorf("the deferred descriptors of a list: %v", got)
		}
		blobs := batch.Blobs([]string{refD, refB, refC, "not a digest"})
		if data, ok := blobs[refB]; len(blobs) != 2 || string(blobs[refD]) != "one" || !ok || len(data) != 0 {
			t.Errorf("the bytes of a list: %q", blobs)
		}
		if len(batch.Objects(nil)) != 0 || len(batch.Deferred(nil)) != 0 || len(batch.Blobs(nil)) != 0 {
			t.Error("a batch read of an empty list")
		}
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
		tx.PutContext("later", ops.ContextRecord{Roots: []string{refA}})
		tx.PutContext("again", ops.ContextRecord{Roots: []string{refC}}) // a second use of a root
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

// long returns a string of 8000 bytes that a compression does not make short. No two seeds give one string.
func long(seed int64) string {
	data := make([]byte, 8000)
	rand.New(rand.NewSource(seed)).Read(data)
	return string(data)
}

// A string of a request has no length bound: a collection name (KND-SCH-1), a policy name, and a producer
// name, key, log name or reason in UTF-8 (PRD-SEC-3). A storage keeps each such string, and two long strings
// with one start are different names.
func longNames(t *testing.T, s Backing) {
	a, b := long(1), long(1)[:7999]+"x"
	update(s, func(tx ops.Tx) {
		tx.LockCollections([]string{a, b})
		tx.PutObject(ops.Object{Ref: refA, Kind: "snapshot", Text: []byte("S"), Collection: a})
		tx.DeclareGate(a)
		tx.DeclareSection(a, "evidence")
		tx.PutCandidate(ops.Candidate{ID: refA, Snapshot: refA, Collection: a, Policy: a})
		tx.Publish(ops.Publication{Candidate: refA, Receipt: refB, Collection: a, Head: refA})
		tx.Publish(ops.Publication{Candidate: refB, Receipt: refB, Collection: b, Head: refB})
		tx.PutProducerObject(a, a, []byte("v"))
		tx.PutProducerObject(a, b, []byte("w"))
		tx.PutProducerObject(a, "gone", []byte("v"))
		tx.DeleteProducerObject(a, "gone")
		tx.PutProducerRemoval(a, ops.Removal{Sequence: 1, Key: "gone", Time: "2026-01-02T03:04:05Z", Reason: a})
		tx.PutProducerEntry(a, a, ops.Entry{Sequence: 1, Data: []byte("e")})
		tx.PutProducerEntry(a, b, ops.Entry{Sequence: 1, Data: []byte("f")})
	})
	r := s.Session(context.Background())
	if !r.HasSnapshot(a) || r.HasSnapshot(b) || !r.Gated(a) || r.Gated(b) {
		t.Error("a snapshot or a gate of a long collection name")
	}
	if section, ok := r.Section(a); !ok || section != "evidence" {
		t.Error("the section of a long collection name")
	}
	if _, ok := r.Section(b); ok {
		t.Error("a section of a different long name")
	}
	if got := r.Heads([]string{a, b, "c"}); len(got) != 2 || got[a] != refA || got[b] != refB {
		t.Errorf("the heads of long collection names: %d", len(got))
	}
	if got := r.Publications(a); len(got) != 1 || got[0].Candidate != refA {
		t.Errorf("the publications of a long collection name: %d", len(got))
	}
	if c, ok := r.Candidate(refA); !ok || c.Collection != a || c.Policy != a {
		t.Error("a candidate with a long collection name and a long policy name")
	}
	if data, ok := r.ProducerObject(a, b); !ok || string(data) != "w" {
		t.Error("a producer object with a long key")
	}
	if _, ok := r.ProducerObject(b, a); ok || !r.ProducerRemoved(a, "gone") || r.ProducerRemoved(b, "gone") {
		t.Error("the producer records of a different long name")
	}
	if got := r.ProducerRemovals(a); len(got) != 1 || got[0].Reason != a {
		t.Errorf("the removal records of a long producer name: %d", len(got))
	}
	if got := r.ProducerEntries(a, b); len(got) != 1 || string(got[0].Data) != "f" || len(r.ProducerEntries(b, a)) != 0 {
		t.Errorf("the entries of a long log name: %d", len(got))
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
