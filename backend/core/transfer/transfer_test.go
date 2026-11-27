package transfer_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/check"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/read"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
	"github.com/mohanraj00/aavanam/backend/core/transfer"
	"github.com/mohanraj00/aavanam/backend/core/write"
)

// imports is the record of each `ok` import of a device store (DEV-UPD-2).
type imports struct {
	mu    sync.Mutex
	roots [][]string
	fail  bool // the record gives a storage fault
}

func (x *imports) ImportRoots() []string { return nil }

func (x *imports) PutImport(roots []string) {
	if x.fail {
		ops.Raise(errors.New("the record failed"))
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.roots = append(x.roots, roots)
}

// logged is a storage in memory whose Tx has an import record.
type logged struct {
	mem *memstore.Store
	*imports
}

type session struct {
	ops.Session
	*imports
}

type tx struct {
	ops.Tx
	*imports
}

func (s *logged) Session(ctx context.Context) ops.Session {
	return session{s.mem.Session(ctx), s.imports}
}

func (s session) Update(fn func(t ops.Tx)) { s.Session.Update(func(t ops.Tx) { fn(tx{t, s.imports}) }) }

const profile = `"descriptor_profile":"aavanam/base/v1"`

func ref(t *testing.T, desc string) string {
	t.Helper()
	value, err := jsontext.Parse([]byte(desc), jsontext.Strict)
	if err != nil {
		t.Fatal(err)
	}
	normal, refusal := check.Normal(value)
	if refusal != nil {
		t.Fatal(refusal)
	}
	return canon.Revision(normal)
}

func registry() *ops.Registry {
	r := ops.NewRegistry()
	read.Register(r)
	write.Register(r)
	transfer.Register(r)
	return r
}

func call(t *testing.T, store *ops.Store, operation, request string) envelope.Envelope {
	t.Helper()
	return store.Call(context.Background(), jsontext.S(operation), []byte(request))
}

// Each `ok` import of a device store adds one record with the roots of the bundle, also an import of
// objects that the store has. An import that is not `ok` adds no record (DEV-UPD-2, BUN-UPD-6). The record
// is in the update of the import, so a failure of the record leaves the store unchanged (DEV-UPD-1, BUN-IMP-4).
func TestDeviceImportRecord(t *testing.T) {
	writer := ops.New(ops.Writer, memstore.New(), registry())
	bindings := `{` + profile + `,"kind":"binding-set"}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + ref(t, bindings) + `"}`
	snapshot := `{` + profile + `,"kind":"snapshot","collection":"c","units":["` + ref(t, unit) + `"]}`
	var texts []string
	for _, d := range []string{bindings, unit, snapshot} {
		texts = append(texts, string(canon.AppendString(nil, d)))
	}
	if e := call(t, writer, "put", `{"descriptors":[`+strings.Join(texts, ",")+`]}`); e.Status != "ok" {
		t.Fatalf("put: %s", e.JSON())
	}
	opened := call(t, writer, "open_context", `{"roots":["`+ref(t, snapshot)+`"],"capabilities":["aavanam/export/v1"]}`)
	if opened.Status != "ok" {
		t.Fatalf("open_context: %s", opened.JSON())
	}
	exported := call(t, writer, "export", `{"context":"`+opened.Context.Str+`","roots":["`+ref(t, snapshot)+`"]}`)
	if exported.Status != "ok" {
		t.Fatalf("export: %s", exported.JSON())
	}
	var bundle jsontext.Value
	for _, f := range exported.Fields {
		if f.Key == "bundle" {
			bundle = f.Val
		}
	}
	request := string(canon.Canonical(jsontext.O(jsontext.M("bundle", bundle))))
	storage := &logged{mem: memstore.New(), imports: &imports{}}
	device := ops.New(ops.Device, storage, registry())
	for n := 0; n < 2; n++ {
		if e := call(t, device, "import", request); e.Status != "ok" {
			t.Fatalf("import %d: %s", n, e.JSON())
		}
	}
	want := [][]string{{ref(t, snapshot)}, {ref(t, snapshot)}}
	if !reflect.DeepEqual(storage.roots, want) {
		t.Fatalf("records %v, want %v", storage.roots, want)
	}
	incomplete := strings.Replace(request, `"roots":["`, `"roots":["sha256:`+strings.Repeat("0", 64)+`","`, 1)
	if e := call(t, device, "import", incomplete); e.Status == "ok" {
		t.Fatalf("an import with an absent root: %s", e.JSON())
	}
	if len(storage.roots) != 2 {
		t.Fatalf("an import that is not ok added a record: %v", storage.roots)
	}
	broken := &logged{mem: memstore.New(), imports: &imports{fail: true}}
	empty := broken.mem.Fingerprint()
	if e := call(t, ops.New(ops.Device, broken, registry()), "import", request); e.Status != "failed" {
		t.Fatalf("an import whose record failed: %s", e.JSON())
	}
	if broken.mem.Fingerprint() != empty {
		t.Fatal("an import whose record failed changed the store")
	}
}

// LIF-GC-5, BUN-IMP-7: a lock call on a device store checks its inputs, and its update writes the lock to
// the local output store. A different call ends the context and collects the inputs after the checks and
// before that update. The update does not write, and the call runs again.
func TestLockAfterCollect(t *testing.T) {
	writer := ops.New(ops.Writer, memstore.New(), registry())
	bindings := `{` + profile + `,"kind":"binding-set"}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + ref(t, bindings) + `"}`
	snapshot := `{` + profile + `,"kind":"snapshot","collection":"c","units":["` + ref(t, unit) + `"]}`
	var texts []string
	for _, d := range []string{bindings, unit, snapshot} {
		texts = append(texts, string(canon.AppendString(nil, d)))
	}
	if e := call(t, writer, "put", `{"descriptors":[`+strings.Join(texts, ",")+`]}`); e.Status != "ok" {
		t.Fatalf("put: %s", e.JSON())
	}
	opened := call(t, writer, "open_context", `{"roots":["`+ref(t, snapshot)+`"]}`)
	exported := call(t, writer, "export", `{"context":"`+opened.Context.Str+`","roots":["`+ref(t, snapshot)+`"]}`)
	if exported.Status != "ok" {
		t.Fatalf("export: %s", exported.JSON())
	}
	var bundle jsontext.Value
	for _, f := range exported.Fields {
		if f.Key == "bundle" {
			bundle = f.Val
		}
	}
	deviceFile := memstore.New()
	outputFile := deviceFile.Output()
	other := ops.New(ops.Device, deviceFile, registry())
	other.Output = ops.New(ops.Output, outputFile, registry())
	if e := call(t, other, "import", string(canon.Canonical(jsontext.O(jsontext.M("bundle", bundle))))); e.Status != "ok" {
		t.Fatalf("import: %s", e.JSON())
	}
	contextID := call(t, other, "open_context", `{"roots":["`+ref(t, snapshot)+`"]}`).Context.Str
	ran := false
	device := ops.New(ops.Device, deviceFile, registry())
	device.Output = ops.New(ops.Output, storagetest.Racing(outputFile, func() {
		if ran {
			return
		}
		ran = true
		if e := call(t, other, "close_context", `{"context":"`+contextID+`"}`); e.Status != "ok" {
			t.Errorf("close_context: %s", e.JSON())
		}
		if e := call(t, other, "gc_collect", `{"roots":[]}`); e.Status != "ok" || len(e.Items) != 3 {
			t.Errorf("gc_collect: %s", e.JSON())
		}
	}), registry())
	e := call(t, device, "build_inputs", `{"context":"`+contextID+`","inputs":["`+ref(t, unit)+`"],"consumer":"sample/emitter/build/v1","completeness":"sample/declared/v1"}`)
	if got := storagetest.Summary(e); !ran || got != "failed:context_expired" {
		t.Errorf("a lock call after a collection of its inputs: %s", got)
	}
	if listed := outputFile.Session(context.Background()).OwnObjects(); len(listed) != 0 {
		t.Errorf("the output store has %d objects", len(listed))
	}
}
