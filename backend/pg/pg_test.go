package pg

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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
