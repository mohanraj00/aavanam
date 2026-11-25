package devicetest

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/kinds"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/reference/device"
)

// declare makes each store declare the layer profile and the retrieval index profile (LYR-PRF-1, IDX-PRF-1).
func declare(stores ...*ops.Store) {
	for _, store := range stores {
		store.Profiles = kinds.Profiles{kinds.LayerProfile: true, kinds.IndexProfile: true}
	}
}

// putUnit writes a unit with a text entry and a logical ID.
func putUnit(t *testing.T, writer *ops.Store, id, text string) string {
	t.Helper()
	artifact := put(t, writer, fields{"kind": "artifact", "artifact_type": "sample/catalog-entry/v1"}, []byte(text))
	binding := put(t, writer, fields{"kind": "binding-set", "bindings": fields{"entry": artifact}}, nil)
	return put(t, writer, fields{"kind": "knowledge-unit", "logical_id": id, "binding_set": binding}, nil)
}

func bundleOf(t *testing.T, result interface{ Value() jsontext.Value }) []byte {
	t.Helper()
	bundle, ok := result.Value().Get("bundle")
	if !ok || bundle.Kind != jsontext.Object {
		t.Fatal("the result has no bundle")
	}
	return canon.Canonical(bundle)
}

func tables(t *testing.T, dir, file string) []string {
	t.Helper()
	rows, err := openSQLiteFile(t, dir, file).Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	return out
}

func statuses(t *testing.T, store *ops.Store, context string, refs ...string) []string {
	t.Helper()
	result := store.Call(t.Context(), jsontext.S("get"), jsonBytes(t, fields{"context": context, "refs": refs}))
	out := make([]string, len(result.Items))
	for n, item := range result.Items {
		out[n] = item.Status
		for _, reason := range item.Reasons {
			out[n] += " " + reason.Code
		}
	}
	return out
}

// LYR-EXP-3: a layered context on the device store has the members of the layered context on the writer
// store, and its lock has the same revision. DEV-LAY-3 and DEV-UPD-3: `open_context` adds one `contexts`
// row and `closures` rows, and no table. The row has the base snapshot and the layer in `roots`, so a
// second open of the files reads the same layered context.
func TestLayeredContextOnTheDeviceStore(t *testing.T) {
	dir, writer, reader := newStores(t)
	declare(writer, reader, reader.Output)
	replaced := putUnit(t, writer, "entry-a", "Maps and atlases.\n")
	kept := putUnit(t, writer, "entry-b", "Field guides.\n")
	local := putUnit(t, writer, "entry-a", "Maps of the north branch.\n")
	added := putUnit(t, writer, "entry-c", "Local history.\n")
	other := putUnit(t, writer, "entry-d", "Notes of the south branch.\n")
	base := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{replaced, kept}}, "ok"))
	layer := put(t, writer, fields{"kind": "layer", "base": catalog, "scope": "branch-north", "units": []string{local, added}}, nil)
	south := put(t, writer, fields{"kind": "layer", "base": catalog, "scope": "branch-south", "units": []string{other}}, nil)
	open := func(store *ops.Store) string {
		t.Helper()
		request := fields{"roots": []string{base}, "layer": fields{"ref": layer, "scope": "branch-north"}}
		return call(t, store, "open_context", request, "ok").Context.Str
	}
	onWriter := open(writer)
	exported := call(t, writer, "export", fields{"context": onWriter, "roots": []string{base, layer}}, "ok")
	call(t, reader, "import", fields{"bundle": json.RawMessage(bundleOf(t, exported))}, "ok")

	before := tables(t, dir, device.DeviceFile)
	onDevice := open(reader)
	if after := tables(t, dir, device.DeviceFile); !reflect.DeepEqual(after, before) {
		t.Errorf("open_context changed the tables of the device file: %v, not %v", after, before)
	}
	var roots []byte
	if err := openSQLiteFile(t, dir, device.DeviceFile).QueryRow(`SELECT roots FROM contexts WHERE id = ?`, onDevice).Scan(&roots); err != nil {
		t.Fatal(err)
	}
	if want := jsonBytes(t, []string{base, layer}); string(roots) != string(want) {
		t.Errorf("the roots of the layered context: %s, not %s", roots, want)
	}

	// The replaced unit is in the store, and it is not a member. The reason does not show that (LYR-ISO-2).
	refs := []string{base, layer, kept, local, added, replaced, south, other}
	want := []string{"ok", "ok", "ok", "ok", "ok", "not_found unknown_ref", "not_found unknown_ref", "not_found unknown_ref"}
	if got := statuses(t, writer, onWriter, refs...); !slices.Equal(got, want) {
		t.Errorf("the writer context: %v", got)
	}
	if got := statuses(t, reader, onDevice, refs...); !slices.Equal(got, want) {
		t.Errorf("the device context: %v", got)
	}
	if got := statuses(t, reader, openContext(t, reader, base), replaced, local); !slices.Equal(got, []string{"ok", "not_found not_in_context"}) {
		t.Errorf("the device context with no layer: %v", got)
	}

	lock := fields{"inputs": []string{kept}, "consumer": "sample-consumer", "completeness": "sample/declared/v1"}
	lockOf := func(store *ops.Store, context string) string {
		t.Helper()
		lock["context"] = context
		ref, _ := call(t, store, "build_inputs", lock, "ok").Value().Get("lock")
		return ref.Str
	}
	layered, plain := lockOf(writer, onWriter), lockOf(writer, openContext(t, writer, base))
	if got := lockOf(reader, onDevice); got != layered || got == plain {
		t.Errorf("the lock of the device context is %s, the lock of the writer context is %s, and the lock with no view is %s", got, layered, plain)
	}

	// A second open of the two files has no state of the first open. One store declares no profile.
	dev, out, err := device.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		out.Close()
		dev.Close()
	})
	for _, declared := range []bool{true, false} {
		second := ops.New(ops.Device, dev, conformance.Registry())
		if declared {
			declare(second)
		}
		if got := statuses(t, second, onDevice, refs...); !slices.Equal(got, want) {
			t.Errorf("the device context in a second open (declared: %v): %v", declared, got)
		}
	}
	if session := dev.Session(t.Context()); !session.HasLayer(catalog) || session.HasLayer("branch-north") || session.HasSnapshot("branch-north") {
		t.Error("the layers of a base collection on the device store are not correct")
	}
}

// IDX-SRC-7: a read context on the device store with an index root gives the items of the writer store.
// IDX-LCK-3: the device store writes the same lock.
func TestIndexOnTheDeviceStore(t *testing.T) {
	_, writer, reader := newStores(t)
	declare(writer, reader, reader.Output)
	first := putUnit(t, writer, "entry-a", "Maps and atlases of the coast.\n")
	second := putUnit(t, writer, "entry-b", "Field guides and maps.\n")
	snapshot := itemRef(t, call(t, writer, "create_snapshot", fields{"collection": catalog, "units": []string{first, second}}, "ok"))
	index := put(t, writer, fields{"kind": "retrieval-index", "snapshot": snapshot, "method": kinds.HashedTerms,
		"parameters": fields{"dimensions": 4096}}, nil)
	onWriter := openContext(t, writer, index)
	exported := call(t, writer, "export", fields{"context": onWriter, "roots": []string{index}}, "ok")
	call(t, reader, "import", fields{"bundle": json.RawMessage(bundleOf(t, exported))}, "ok")
	onDevice := openContext(t, reader, index)
	search := func(store *ops.Store, context string) string {
		t.Helper()
		result := call(t, store, "semantic_search", fields{"context": context, "index": index, "query": "maps of the coast"}, "ok")
		items := make([]jsontext.Value, len(result.Items))
		for n, item := range result.Items {
			items[n] = item.Value()
		}
		return string(canon.Canonical(jsontext.A(items)))
	}
	want := search(writer, onWriter)
	if got := search(reader, onDevice); got != want {
		t.Errorf("the device store gives %s, and the writer store gives %s", got, want)
	}
	// The first unit has each term of the query, so it has the larger score and comes first (IDX-SRC-6).
	result := call(t, writer, "semantic_search", fields{"context": onWriter, "index": index, "query": "maps of the coast"}, "ok")
	if len(result.Items) != 2 || itemField(result.Items[0].Value(), "ref") != first || itemField(result.Items[1].Value(), "ref") != second {
		t.Errorf("the items of the search: %s", want)
	}
	lock := fields{"inputs": []string{first}, "indexes": []string{index}, "consumer": "sample-consumer", "completeness": "sample/declared/v1"}
	lockOf := func(store *ops.Store, context string) string {
		t.Helper()
		lock["context"] = context
		ref, _ := call(t, store, "build_inputs", lock, "ok").Value().Get("lock")
		return ref.Str
	}
	if got, want := lockOf(reader, onDevice), lockOf(writer, onWriter); got != want || got == "" {
		t.Errorf("the lock of the device context is %q, and the lock of the writer context is %q", got, want)
	}
}

func itemField(item jsontext.Value, name string) string {
	value, _ := item.Get(name)
	return value.Str
}
