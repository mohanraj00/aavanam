package devicetest

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
	"github.com/mohanraj00/aavanam/reference/device"
)

const catalog = "library-catalog"

type fields map[string]any

func newStores(t *testing.T) (dir string, writer, reader *ops.Store) {
	t.Helper()
	dir = t.TempDir()
	if err := device.Create(dir); err != nil {
		t.Fatal(err)
	}
	dev, out, err := device.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := out.Close(); err != nil {
			t.Error(err)
		}
		if err := dev.Close(); err != nil {
			t.Error(err)
		}
	})
	registry := conformance.Registry()
	writer = ops.New(ops.Writer, memstore.New(), registry)
	reader = ops.New(ops.Device, dev, registry)
	reader.Output = ops.New(ops.Output, out, registry)
	return dir, writer, reader
}

func jsonBytes(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func call(t *testing.T, store *ops.Store, operation string, request fields, status string) envelope.Envelope {
	t.Helper()
	result := store.Call(t.Context(), jsontext.S(operation), jsonBytes(t, request))
	if result.Status != status {
		t.Fatalf("%s: want status %q, got %s", operation, status, result.JSON())
	}
	return result
}

func itemRef(t *testing.T, result envelope.Envelope) string {
	t.Helper()
	if len(result.Items) != 1 || result.Items[0].Status != "ok" {
		t.Fatalf("want one ok item, got %s", result.JSON())
	}
	ref, _ := result.Items[0].Value().Get("ref")
	if ref.Str == "" {
		t.Fatalf("missing item reference in %s", result.JSON())
	}
	return ref.Str
}

func digest(data []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}

func put(t *testing.T, writer *ops.Store, descriptor fields, data []byte) string {
	t.Helper()
	descriptor["descriptor_profile"] = "aavanam/base/v1"
	request := fields{}
	if data != nil {
		key := digest(data)
		descriptor["payload"] = fields{"digest": key, "size": len(data), "media_type": "text/plain"}
		request["payloads"] = map[string][]byte{key: data}
	}
	request["descriptors"] = []string{string(jsonBytes(t, descriptor))}
	return itemRef(t, call(t, writer, "put", request, "ok"))
}

func putEntry(t *testing.T, writer *ops.Store, text string) (artifact, unit string) {
	t.Helper()
	artifact = put(t, writer, fields{"kind": "artifact", "artifact_type": "sample/catalog-entry/v1"}, []byte(text))
	binding := put(t, writer, fields{"kind": "binding-set", "bindings": fields{"entry": artifact}}, nil)
	unit = put(t, writer, fields{"kind": "knowledge-unit", "binding_set": binding}, nil)
	return artifact, unit
}

func openContext(t *testing.T, store *ops.Store, root string) string {
	t.Helper()
	result := call(t, store, "open_context", fields{"roots": []string{root}}, "ok")
	if result.Context.Str == "" {
		t.Fatalf("missing context in %s", result.JSON())
	}
	return result.Context.Str
}

func exportBundle(t *testing.T, writer *ops.Store, root string) json.RawMessage {
	t.Helper()
	contextID := openContext(t, writer, root)
	result := call(t, writer, "export", fields{"context": contextID, "roots": []string{root}}, "ok")
	bundle, ok := result.Value().Get("bundle")
	if !ok || bundle.Kind != jsontext.Object {
		t.Fatalf("missing bundle in %s", result.JSON())
	}
	return canon.Canonical(bundle)
}

func openSQLiteFile(t *testing.T, dir, name string) *sql.DB {
	t.Helper()
	dsn := url.URL{Scheme: "file", Path: filepath.Join(dir, name), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

// BUN-CLS-5: A Serving bundle and its device store have no Evidence material.
func TestServingBundleExcludesEvidence(t *testing.T) {
	dir, writer, reader := newStores(t)
	if err := writer.Section(t.Context(), catalog, "serving"); err != nil {
		t.Fatal(err)
	}
	const notes = "catalog-notes"
	if err := writer.Section(t.Context(), notes, "evidence"); err != nil {
		t.Fatal(err)
	}
	const entry = "The west room has a map collection.\n"
	artifact, unit := putEntry(t, writer, entry)
	source := put(t, writer, fields{"kind": "source"}, []byte("Shelf card for the west room."))
	reading := put(t, writer, fields{
		"kind": "reading", "source": source, "producer": "sample/reader/v1", "coverage_profile": "sample/coverage/v1",
	}, []byte("Map shelf card"))
	excerpt := put(t, writer, fields{
		"kind": "excerpt", "parent": reading, "ranges": []fields{{"start": 0, "end": 3}},
	}, []byte("Map"))
	evidence := put(t, writer, fields{
		"kind": "evidence", "subject": artifact, "supports": excerpt, "origin": "sample/editor/v1",
	}, nil)
	relations := put(t, writer, fields{"kind": "relation-set", "members": []string{evidence}}, nil)
	itemRef(t, call(t, writer, "create_snapshot", fields{"collection": notes, "relation_sets": []string{relations}}, "ok"))
	root := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{unit}}, "ok"))

	bundle := exportBundle(t, writer, root)
	var manifest struct {
		Descriptors []string
		Payloads    map[string][]byte
	}
	if err := json.Unmarshal(bundle, &manifest); err != nil {
		t.Fatal(err)
	}
	evidenceKinds := []string{"source", "reading", "excerpt", "evidence"}
	for _, text := range manifest.Descriptors {
		var descriptor struct{ Kind string }
		if err := json.Unmarshal([]byte(text), &descriptor); err != nil {
			t.Fatal(err)
		}
		if slices.Contains(evidenceKinds, descriptor.Kind) {
			t.Errorf("the Serving bundle has a descriptor of kind %q", descriptor.Kind)
		}
	}
	wantPayloads := map[string][]byte{digest([]byte(entry)): []byte(entry)}
	if !maps.EqualFunc(manifest.Payloads, wantPayloads, bytes.Equal) {
		t.Errorf("Serving bundle payloads = %q, want %q", manifest.Payloads, wantPayloads)
	}
	call(t, reader, "import", fields{"bundle": bundle}, "ok")

	// Inspect the device file for all stored content, including content outside the context.
	db := openSQLiteFile(t, dir, "device.sqlite")
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM objects WHERE kind IN (?, ?, ?, ?)`,
		[]byte("source"), []byte("reading"), []byte("excerpt"), []byte("evidence")).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("the device file has %d Evidence descriptors", count)
	}
	rows, err := db.Query(`SELECT digest, data FROM payloads`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	gotPayloads := map[string][]byte{}
	for rows.Next() {
		var key, data []byte
		if err := rows.Scan(&key, &data); err != nil {
			t.Fatal(err)
		}
		gotPayloads[fmt.Sprintf("sha256:%x", key)] = data
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !maps.EqualFunc(gotPayloads, wantPayloads, bytes.Equal) {
		t.Errorf("device payloads = %q, want %q", gotPayloads, wantPayloads)
	}
}

func readEntry(t *testing.T, store *ops.Store, contextID, ref, want string) envelope.Envelope {
	t.Helper()
	result := call(t, store, "read", fields{"context": contextID, "ref": ref}, "ok")
	if result.Context.Str != contextID || itemRef(t, result) != ref {
		t.Fatalf("read returned a different context or reference: %s", result.JSON())
	}
	data, exists := result.Items[0].Value().Get("data")
	if !exists || data.Kind != jsontext.String {
		t.Fatalf("missing payload in %s", result.JSON())
	}
	got, err := base64.StdEncoding.DecodeString(data.Str)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("entry payload = %q, want %q", got, want)
	}
	return result
}

// BUN-UPD-3 and BUN-UPD-4: An update keeps the old context and gives the new content in a new context.
func TestBundleUpdateKeepsOldContext(t *testing.T) {
	_, writer, reader := newStores(t)
	const oldText = "The maps are in the west room.\n"
	const newText = "The maps are in the east room.\n"
	oldRef, oldUnit := putEntry(t, writer, oldText)
	oldRoot := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{oldUnit}}, "ok"))
	call(t, reader, "import", fields{"bundle": exportBundle(t, writer, oldRoot)}, "ok")
	oldContext := openContext(t, reader, oldRoot)
	before := readEntry(t, reader, oldContext, oldRef, oldText)

	newRef, newUnit := putEntry(t, writer, newText)
	newRoot := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{newUnit}}, "ok"))
	call(t, reader, "import", fields{"bundle": exportBundle(t, writer, newRoot)}, "ok")

	after := readEntry(t, reader, oldContext, oldRef, oldText)
	if !bytes.Equal(before.JSON(), after.JSON()) {
		t.Errorf("the old read changed from %s to %s", before.JSON(), after.JSON())
	}
	excluded := call(t, reader, "read", fields{"context": oldContext, "ref": newRef}, "not_found")
	if len(excluded.Reasons) != 1 || excluded.Reasons[0].Code != "not_in_context" {
		t.Errorf("want not_in_context for the new entry in the old context, got %s", excluded.JSON())
	}
	readEntry(t, reader, openContext(t, reader, newRoot), newRef, newText)
}

// LIF-GC-5, BUN-RMV-2: a `gc_collect` commits after the checks of a different call of the same process and
// before its update. The update does not write, and the call runs again on the content that the store has.
func TestCollectBeforeTheUpdate(t *testing.T) {
	_, writer, reader := newStores(t)
	_, unit := putEntry(t, writer, "The west room has a map collection.\n")
	snapshot := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{unit}}, "ok"))
	call(t, reader, "import", fields{"bundle": exportBundle(t, writer, snapshot)}, "ok")
	for _, target := range []*ops.Store{reader, reader.Output} {
		ran := false
		raced := ops.New(target.Role, storagetest.Racing(target.Storage, func() {
			if !ran {
				ran = true
				call(t, reader, "gc_collect", fields{"roots": []string{}}, "ok")
			}
		}), conformance.Registry())
		result := raced.Call(t.Context(), jsontext.S("open_context"), jsonBytes(t, fields{"roots": []string{snapshot}}))
		if got := storagetest.Summary(result); !ran || got != "not_found:unknown_ref | not_found:unknown_ref" {
			t.Errorf("open_context on a snapshot that a gc_collect removed before the update: %s", got)
		}
		call(t, reader, "import", fields{"bundle": exportBundle(t, writer, snapshot)}, "ok")
	}
}

// DEV-RMV-2: two opens of one device directory, as two programs have. A `gc_collect` of the first open adds
// 1 to the removal count of the device file. The second open reads the count at the start of its next call
// and drops the descriptors that it keeps in memory. An update of the second open that starts after a
// removal of the first open does not write, and its call runs again.
func TestRemovalCountOfTwoOpens(t *testing.T) {
	dir, writer, first := newStores(t)
	dev, out, err := device.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close(); dev.Close() })
	second := ops.New(ops.Device, dev, conformance.Registry())
	second.Output = ops.New(ops.Output, out, conformance.Registry())
	_, unit := putEntry(t, writer, "The west room has a map collection.\n")
	snapshot := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{unit}}, "ok"))
	bundle := exportBundle(t, writer, snapshot)
	unknown := func(result envelope.Envelope, what string) {
		t.Helper()
		if got := storagetest.Summary(result); got != "not_found:unknown_ref | not_found:unknown_ref" {
			t.Errorf("%s: %s", what, got)
		}
	}
	count := func() string {
		t.Helper()
		var value sql.NullString
		db := openSQLiteFile(t, dir, device.DeviceFile)
		if err := db.QueryRow(`SELECT (SELECT value FROM meta WHERE key = 'removals')`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value.String
	}
	roots := jsonBytes(t, fields{"roots": []string{snapshot}})

	// The second open has the descriptors in memory, and no context of it is open.
	call(t, first, "import", fields{"bundle": bundle}, "ok")
	for _, store := range []*ops.Store{second, second.Output} {
		call(t, store, "close_context", fields{"context": openContext(t, store, snapshot)}, "ok")
	}
	if got := count(); got != "" {
		t.Fatalf("the removal count before a removal: %q", got)
	}
	call(t, first, "gc_collect", fields{"roots": []string{}}, "ok")
	if got := count(); got != "1" {
		t.Fatalf("the removal count after one removal: %q", got)
	}
	for _, store := range []*ops.Store{second, second.Output} {
		unknown(store.Call(t.Context(), jsontext.S("open_context"), roots), "open_context after a removal of a different open")
	}

	// The first open removes after the checks of a call of the second open and before its update.
	for n, target := range []*ops.Store{second, second.Output} {
		call(t, first, "import", fields{"bundle": bundle}, "ok")
		ran := false
		raced := ops.New(target.Role, storagetest.Racing(target.Storage, func() {
			if !ran {
				ran = true
				call(t, first, "gc_collect", fields{"roots": []string{}}, "ok")
			}
		}), conformance.Registry())
		unknown(raced.Call(t.Context(), jsontext.S("open_context"), roots), "open_context with a removal of a different open before its update")
		if got := count(); !ran || got != fmt.Sprint(n+2) {
			t.Fatalf("the removal count after removal %d: %q", n+2, got)
		}
	}
	// A `gc_collect` that removes nothing does not change the count.
	call(t, first, "gc_collect", fields{"roots": []string{}}, "ok")
	if got := count(); got != "3" {
		t.Fatalf("the removal count after a call that removed nothing: %q", got)
	}
}

// secondOpen opens the device directory a second time, as a second program does (DEV-RMV-2).
func secondOpen(t *testing.T, dir string) (dev, out *device.Store) {
	t.Helper()
	dev, out, err := device.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close(); dev.Close() })
	return dev, out
}

// objectCount returns the number of descriptors in one file of the device directory.
func objectCount(t *testing.T, dir, file string) int {
	t.Helper()
	var n int
	if err := openSQLiteFile(t, dir, file).QueryRow(`SELECT count(*) FROM objects`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// DEV-RMV-2, LIF-GC-5, BUN-IMP-7: a lock call of the second open reads its inputs. Then the first open ends
// the context and removes the inputs, before the update of the lock call in the output file. The update
// does not write, the call runs again, and the output file has no lock that names a removed object.
func TestLockOfTwoOpens(t *testing.T) {
	dir, writer, first := newStores(t)
	dev, out := secondOpen(t, dir)
	_, unit := putEntry(t, writer, "The west room has a map collection.\n")
	snapshot := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{unit}}, "ok"))
	call(t, first, "import", fields{"bundle": exportBundle(t, writer, snapshot)}, "ok")
	second := ops.New(ops.Device, dev, conformance.Registry())
	contextID := openContext(t, second, snapshot)
	ran := false
	second.Output = ops.New(ops.Output, storagetest.Racing(out, func() {
		if !ran {
			ran = true
			call(t, first, "close_context", fields{"context": contextID}, "ok")
			if removed := call(t, first, "gc_collect", fields{"roots": []string{}}, "ok"); len(removed.Items) == 0 {
				t.Error("the gc_collect of the first open removed no object")
			}
		}
	}), conformance.Registry())
	request := fields{"context": contextID, "inputs": []string{unit}, "consumer": "sample/emitter/build/v1", "completeness": "sample/declared/v1"}
	result := second.Call(t.Context(), jsontext.S("build_inputs"), jsonBytes(t, request))
	if got := storagetest.Summary(result); !ran || got != "failed:context_expired" {
		t.Errorf("a lock call after a removal of its inputs by a different open: %s", got)
	}
	if n := objectCount(t, dir, device.OutputFile); n != 0 {
		t.Errorf("the output file has %d objects", n)
	}
	// With no removal, the same call of the second open writes its lock.
	call(t, first, "import", fields{"bundle": exportBundle(t, writer, snapshot)}, "ok")
	request["context"] = openContext(t, second, snapshot)
	call(t, second, "build_inputs", request, "ok")
	if n := objectCount(t, dir, device.OutputFile); n != 1 {
		t.Errorf("the output file has %d objects after an ok lock call", n)
	}
}

// DEV-RMV-2, BUN-DLT-4, BUN-UPD-6: an import of a delta bundle in the second open finds its base root in the
// store. Then the first open removes the base, before the update of the import. The update does not write,
// and the call runs again: the store does not have the base root, so the result is `bundle_incomplete`.
func TestDeltaImportOfTwoOpens(t *testing.T) {
	dir, writer, first := newStores(t)
	dev, out := secondOpen(t, dir)
	_, unit := putEntry(t, writer, "The west room has a map collection.\n")
	base := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{unit}}, "ok"))
	entry, added := putEntry(t, writer, "The east room has a globe.\n")
	update := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{unit, added}}, "ok"))
	exported := call(t, writer, "export", fields{"context": openContext(t, writer, update), "roots": []string{update},
		"base_context": openContext(t, writer, base)}, "ok")
	bundle, _ := exported.Value().Get("bundle")
	delta := fields{"bundle": json.RawMessage(canon.Canonical(bundle))}
	full := fields{"bundle": exportBundle(t, writer, base)}

	call(t, first, "import", full, "ok")
	ran := false
	second := ops.New(ops.Device, storagetest.Racing(dev, func() {
		if !ran {
			ran = true
			if removed := call(t, first, "gc_collect", fields{"roots": []string{}}, "ok"); len(removed.Items) == 0 {
				t.Error("the gc_collect of the first open removed no object")
			}
		}
	}), conformance.Registry())
	second.Output = ops.New(ops.Output, out, conformance.Registry())
	result := second.Call(t.Context(), jsontext.S("import"), jsonBytes(t, delta))
	if !ran || result.Status != "failed" || len(result.Reasons) == 0 || result.Reasons[0].Code != "bundle_incomplete" {
		t.Errorf("a delta import after a removal of its base by a different open: %s", storagetest.Summary(result))
	}
	if n := objectCount(t, dir, device.DeviceFile); n != 0 {
		t.Errorf("the device file has %d objects after an import that is not ok", n)
	}
	// With no removal, the same import of the second open adds the content of the update.
	call(t, first, "import", full, "ok")
	call(t, second, "import", delta, "ok")
	readEntry(t, first, openContext(t, first, update), entry, "The east room has a globe.\n")
}
