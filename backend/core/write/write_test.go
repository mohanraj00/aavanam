package write_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
)

// The operations of the core on the storage in memory. Package storagetest has the tests, so that each
// storage runs the same ones.
func TestCoreOnTheStorageInMemory(t *testing.T) {
	storagetest.Core(t, func(*testing.T) storagetest.Backing { return memstore.New() })
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
	store := ops.New(ops.Writer, faulty{mem}, storagetest.Registry())
	before := mem.Fingerprint()
	e := store.Call(context.Background(), jsontext.S("put"), []byte(storagetest.Put([]string{storagetest.Source("alpha"), storagetest.Source("beta")}, "alpha", "beta")))
	if got := storagetest.Summary(e); got != "failed:storage_error | failed:batch_aborted | failed:batch_aborted" {
		t.Errorf("a storage fault: %s", got)
	}
	if mem.Fingerprint() != before {
		t.Error("a failed update changed the store")
	}
}
