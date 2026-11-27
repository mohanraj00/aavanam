package pg

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
)

const (
	refA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	refB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	refC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// open returns an empty store in the database of AAVANAM_PG_DSN. The tests remove each row of that
// database, so they run only if the variable is set.
func open(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("AAVANAM_PG_DSN")
	if dsn == "" {
		t.Skip("AAVANAM_PG_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Truncate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func update(s *Store, fn func(tx ops.Tx)) { s.Session(context.Background()).Update(fn) }

// The contract of a storage, and the operations of the core on PostgreSQL.
func TestStorageContract(t *testing.T) {
	storagetest.Storage(t, func(t *testing.T) storagetest.Backing { return open(t) })
	storagetest.Core(t, func(t *testing.T) storagetest.Backing { return open(t) })
	storagetest.Removal(t, func(t *testing.T) storagetest.Backing { return open(t) }, true)
}

func TestMigrateIsIdempotent(t *testing.T) {
	s := open(t)
	for n := 0; n < 2; n++ {
		if err := Migrate(context.Background(), s.pool); err != nil {
			t.Fatal(err)
		}
	}
	var version int
	if err := s.pool.QueryRow(context.Background(), `SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != len(migrations) {
		t.Errorf("schema version %d, %v", version, err)
	}
	if _, err := Open(context.Background(), ""); err == nil {
		t.Error("an empty DSN gave no error")
	}
	if _, err := Open(context.Background(), "postgres://user:the-password@127.0.0.1:1/none?connect_timeout=1"); err == nil || strings.Contains(err.Error(), "the-password") {
		t.Errorf("a DSN that does not connect: %v", err)
	}
}

// The content tables take inserts only. A trigger refuses a change also for the owner of the tables.
func TestContentTablesRefuseUpdateAndDelete(t *testing.T) {
	s := open(t)
	update(s, func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: refA, Kind: "source", Text: []byte("A"), Targets: []string{refB}})
		tx.PutBlob(refC, []byte("bytes"))
		tx.PutCandidate(ops.Candidate{ID: refA, Snapshot: refA, Collection: "c", Policy: "p"})
		tx.Publish(ops.Publication{Candidate: refA, Receipt: refB, Collection: "c", Head: refA})
		tx.DeclareGate("c")
		tx.DeclareSection("c", "serving")
		tx.PutProducerEntry("p", "l", ops.Entry{Sequence: 1})
		tx.PutProducerRemoval("p", ops.Removal{Sequence: 1, Key: "k", Time: "t", Reason: "r"})
	})
	before := s.Fingerprint()
	for _, statement := range []string{
		`UPDATE objects SET canonical = 'x'`, `DELETE FROM objects`, `DELETE FROM edges`, `UPDATE payloads SET data = 'x'`,
		`DELETE FROM payloads`, `UPDATE candidates SET policy = 'x'`, `DELETE FROM publications`, `DELETE FROM gates`,
		`UPDATE sections SET section = 'evidence'`, `DELETE FROM producer_entries`, `UPDATE producer_removals SET reason = 'x'`,
		`DELETE FROM candidates`, `UPDATE removals SET reason = 'x'`, `DELETE FROM removals`,
	} {
		if _, err := s.pool.Exec(context.Background(), statement); err == nil || !strings.Contains(err.Error(), "takes inserts only") {
			t.Errorf("%s: %v", statement, err)
		}
	}
	if s.Fingerprint() != before {
		t.Error("a refused statement changed the store")
	}
}

// A statement that does not end before the deadline of the call is a storage fault with the deadline, so
// the dispatch gives `deadline_exceeded`.
func TestDeadlineIsAFault(t *testing.T) {
	s := open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := storagetest.Fault(func() { s.Session(ctx).(session).exec(`SELECT pg_sleep(5)`) })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a statement after the deadline: %v", err)
	}
	if got := ops.FaultFailure(&ops.Fault{Err: err}).Reasons[0].Code; got != "deadline_exceeded" {
		t.Errorf("the reason of a deadline: %s", got)
	}
	if _, _, ok := s.Session(context.Background()).Object(refA); ok {
		t.Error("the pool is not usable after a deadline")
	}
}

// Two updates that lock one collection, one digest or one producer run one after the other. The second
// reads the commit of the first (PUB-SEC-2, PUB-HED-3, BUN-IMP-10, PRD-LOG-2).
func TestLocksMakeUpdatesWait(t *testing.T) {
	s := open(t)
	for name, lock := range map[string]func(tx ops.Tx){
		"a collection lock": func(tx ops.Tx) { tx.LockCollections([]string{"c"}) },
		"a digest lock":     func(tx ops.Tx) { tx.LockDigests([]string{refA}) },
		"a producer lock":   func(tx ops.Tx) { tx.(ops.ProducerLock).LockProducer("p") },
	} {
		if err := s.Truncate(context.Background()); err != nil {
			t.Fatal(err)
		}
		inside := make(chan struct{})
		var wg sync.WaitGroup
		var saw [2]bool
		for n := 0; n < 2; n++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				if n == 1 {
					<-inside // the first update has the lock now
				}
				update(s, func(tx ops.Tx) {
					lock(tx)
					_, saw[n] = tx.Section("c")
					if n == 0 {
						close(inside)
						time.Sleep(300 * time.Millisecond)
					}
					tx.DeclareSection("c", "serving")
				})
			}(n)
		}
		wg.Wait()
		if saw[0] || !saw[1] {
			t.Errorf("%s: the second update did not wait for the first: %v", name, saw)
		}
	}
}

// A read context has a life. After its end, the store does not keep the context for a new call, and its
// content stays for a call in progress until ExpireContexts removes the context. A context is not content.
func TestContextsExpire(t *testing.T) {
	s := open(t)
	update(s, func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: refB, Kind: "source", Text: []byte("B")})
		tx.PutObject(ops.Object{Ref: refA, Kind: "knowledge-unit", Text: []byte("A"), Targets: []string{refB}})
		tx.PutContext("old", ops.ContextRecord{Roots: []string{refA}, Capabilities: []string{}})
	})
	if n, err := s.ExpireContexts(context.Background(), 0); n != 0 || err != nil {
		t.Errorf("a store with no context life removed %d contexts: %v", n, err)
	}
	before := s.Fingerprint()
	s.SetContextLife(400 * time.Millisecond)
	defer s.SetContextLife(0)
	if _, ok := s.Session(context.Background()).Context("old"); !ok {
		t.Fatal("the store does not keep a context before the end of its life")
	}
	time.Sleep(600 * time.Millisecond)
	update(s, func(tx ops.Tx) {
		tx.PutContext("new", ops.ContextRecord{Roots: []string{refB}, Capabilities: []string{}})
	})
	r := s.Session(context.Background())
	if _, ok := r.Context("old"); ok {
		t.Error("the store keeps a context after the end of its life")
	}
	if !r.IsMember("old", refB) || len(r.Members("old")) != 2 {
		t.Error("the content of a call in progress is not there")
	}
	if n, err := s.ExpireContexts(context.Background(), time.Hour); n != 0 || err != nil {
		t.Errorf("a context in its grace time: %d removed, %v", n, err)
	}
	if n, err := s.ExpireContexts(context.Background(), 0); n != 1 || err != nil {
		t.Errorf("the removal of the old context: %d removed, %v", n, err)
	}
	r = s.Session(context.Background())
	if len(r.Members("old")) != 0 || r.IsMember("old", refA) || len(r.Members("new")) != 1 {
		t.Error("the content of the contexts after the removal")
	}
	if _, ok := r.Context("new"); !ok || s.Fingerprint() != before {
		t.Error("the removal changed a different context or the content of the store")
	}
}

// Two updates that write the closures of the same roots do not stop each other: each one writes the rows
// in ascending order, whatever the sequence of its roots (KND-DEP-3). The first update writes the closure
// of A and then that of B, as one statement for the roots A and B does. The second update has the roots B
// and A. If it wrote B first, each update would wait for a row of the other.
func TestClosureWritesHaveOneOrder(t *testing.T) {
	s := open(t)
	inside := make(chan struct{})
	var wg sync.WaitGroup
	var faults [2]error
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			faults[n] = storagetest.Fault(func() {
				if n == 1 {
					<-inside // the first update has the rows of A now
					update(s, func(tx ops.Tx) { tx.PutContext("second", ops.ContextRecord{Roots: []string{refB, refA}}) })
					return
				}
				update(s, func(tx ops.Tx) {
					tx.PutContext("first-a", ops.ContextRecord{Roots: []string{refA}})
					close(inside)
					time.Sleep(300 * time.Millisecond) // the second update waits for a row now
					tx.PutContext("first-b", ops.ContextRecord{Roots: []string{refB}})
				})
			})
		}(n)
	}
	wg.Wait()
	if faults[0] != nil || faults[1] != nil {
		t.Fatalf("an update did not write its closures: %v", faults)
	}
	members := s.Session(context.Background()).Members("second")
	if len(members) != 2 || members[0] != refA || members[1] != refB {
		t.Errorf("the members of the context with two roots: %v", members)
	}
}

// A removal increments the removal epoch of the database. A different open of the database then drops the
// descriptors that it kept in memory, so it gives `removed` for the object (REL-RMV-7, ops.RemovalEpoch).
func TestRemovalEpochOfTwoOpens(t *testing.T) {
	first := open(t)
	ctx := context.Background()
	second, err := Open(ctx, os.Getenv("AAVANAM_PG_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	a := ops.New(ops.Writer, first, storagetest.Registry())
	b := ops.New(ops.Writer, second, storagetest.Registry())
	call := func(s *ops.Store, operation, request string) string {
		return storagetest.Summary(s.Call(ctx, jsontext.S(operation), []byte(request)))
	}
	refOf := func(descriptor string) string {
		value, err := jsontext.Parse([]byte(descriptor), jsontext.Strict)
		if err != nil {
			t.Fatal(err)
		}
		return jsontext.Digest(canon.Canonical(value))
	}
	source := storagetest.Source("alpha")
	ref := refOf(source)
	observation := `{"descriptor_profile":"aavanam/base/v1","kind":"observation","subjects":["` + ref +
		`"],"observation_type":"t","scope":{},"values":{}}`
	put := a.Call(ctx, jsontext.S("put"), []byte(storagetest.Put([]string{source, observation}, "alpha")))
	if storagetest.Summary(put) != "ok | ok | ok" {
		t.Fatalf("put: %s", storagetest.Summary(put))
	}
	// A snapshot is a root, and its closure has the source through the observation.
	snapshot := a.Call(ctx, jsontext.S("create_snapshot"), []byte(`{"collection":"notes","observations":["`+refOf(observation)+`"]}`))
	if storagetest.Summary(snapshot) != "ok | ok" {
		t.Fatalf("create_snapshot: %s", storagetest.Summary(snapshot))
	}
	roots := `{"roots":["` + refOf(`{"descriptor_profile":"aavanam/base/v1","kind":"snapshot","collection":"notes","observations":["`+
		refOf(observation)+`"]}`) + `"]}`
	get := func(context envelope.Envelope) string {
		id, _ := context.Context.Text()
		return call(b, "get", `{"context":"`+id+`","refs":["`+ref+`"]}`)
	}
	opened := b.Call(ctx, jsontext.S("open_context"), []byte(roots))
	if got := get(opened); storagetest.Summary(opened) != "ok | ok" || got != "ok | ok" {
		t.Fatalf("a read on the second open: %s, %s", storagetest.Summary(opened), got)
	}
	before := second.Session(ctx).(ops.RemovalEpoch).RemovalEpoch()
	if got := call(a, "remove", `{"ref":"`+ref+`","reason":"a test"}`); got != "ok | ok" {
		t.Fatalf("remove: %s", got)
	}
	if after := second.Session(ctx).(ops.RemovalEpoch).RemovalEpoch(); after != before+1 {
		t.Errorf("the removal epoch is %d after a removal, and it was %d", after, before)
	}
	if got := get(opened); got != "failed:context_expired" { // CTX-LIF-4
		t.Errorf("a read in a context that had the removed object: %s", got)
	}
	opened = b.Call(ctx, jsontext.S("open_context"), []byte(roots))
	if got := get(opened); storagetest.Summary(opened) != "ok | ok" || got != "not_found:removed | not_found:removed" {
		t.Errorf("a read on the second open after the removal: %s, %s", storagetest.Summary(opened), got)
	}
	// The permission to delete content ends with the update of the removal.
	if _, err := first.pool.Exec(ctx, `DELETE FROM objects`); err == nil || !strings.Contains(err.Error(), "takes inserts only") {
		t.Errorf("a DELETE after the update of a removal: %v", err)
	}
	// An update of a call that started before a removal does not write: its checks read content that the
	// removal changed (ops.ErrRemoved).
	stale := first.Session(ctx)
	stale.(ops.RemovalEpoch).RemovalEpoch()
	update(second, func(tx ops.Tx) {
		tx.(ops.RemovalGate).LockRemoval()
		tx.DeleteBlobs([]string{refC})
		tx.(ops.RemovalGate).AdvanceEpoch()
	})
	fault := storagetest.Fault(func() {
		stale.Update(func(tx ops.Tx) { tx.PutObject(ops.Object{Ref: refA, Kind: "source", Text: []byte("A")}) })
	})
	if !errors.Is(fault, ops.ErrRemoved) {
		t.Errorf("an update that started before a removal: %v", fault)
	}
	if _, _, has := first.Session(ctx).Object(refA); has {
		t.Error("an update that a removal stopped wrote an object")
	}
	if got := call(a, "put", storagetest.Put([]string{source}, "alpha")); got != "ok | ok" {
		t.Errorf("a put after the removals: %s", got)
	}
}
