package transfer_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/envelope"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/memstore"
	"github.com/mohanraj00/aavanam/backend/core/ops"
	"github.com/mohanraj00/aavanam/backend/core/storagetest"
	"github.com/mohanraj00/aavanam/backend/core/transfer"
)

func bundleOf(t *testing.T, e envelope.Envelope) jsontext.Value {
	t.Helper()
	for _, f := range e.Fields {
		if f.Key == "bundle" {
			return f.Val
		}
	}
	t.Fatalf("no bundle: %s", e.JSON())
	return jsontext.Value{}
}

// A raw form of a bundle has the manifest line and then the bytes of each payload (BUN-RAW-1 to BUN-RAW-3).
// An import of it gives the content of the import of the bundle (BUN-RAW-5). Each fault of BUN-RAW-4 gives
// its reason, and the store does not change.
func TestRawBundle(t *testing.T) {
	writer := ops.New(ops.Writer, memstore.New(), registry())
	artifact := func(kind, data string) string {
		return `{` + profile + `,"kind":"artifact","artifact_type":"` + kind + `","payload":{"digest":"` +
			jsontext.Digest([]byte(data)) + `","media_type":"text/plain","size":` + string(rune('0'+len(data))) + `}}`
	}
	// One payload has a line feed byte, and one payload has no bytes.
	first, second := artifact("sample/a/v1", "one\ntwo"), artifact("sample/b/v1", "")
	bindings := `{` + profile + `,"kind":"binding-set","bindings":{"a":"` + ref(t, first) + `","b":"` + ref(t, second) + `"}}`
	unit := `{` + profile + `,"kind":"knowledge-unit","binding_set":"` + ref(t, bindings) + `"}`
	for _, request := range []string{storagetest.Put([]string{first, second}, "one\ntwo", ""), storagetest.Put([]string{bindings}),
		storagetest.Put([]string{unit})} {
		if e := call(t, writer, "put", request); e.Status != "ok" {
			t.Fatalf("put: %s", e.JSON())
		}
	}
	opened := call(t, writer, "open_context", `{"roots":["`+ref(t, unit)+`"]}`)
	exported := call(t, writer, "export", `{"context":"`+opened.Context.Str+`","roots":["`+ref(t, unit)+`"]}`)
	if exported.Status != "ok" {
		t.Fatalf("export: %s", exported.JSON())
	}
	bundle := bundleOf(t, exported)
	var out bytes.Buffer
	if err := transfer.WriteRaw(&out, bundle); err != nil {
		t.Fatal(err)
	}
	raw := out.Bytes()
	line, section, _ := bytes.Cut(raw, []byte("\n"))
	manifest, formError := jsontext.Parse(line, jsontext.Strict)
	if formError != nil {
		t.Fatalf("the manifest line: %v", formError)
	}
	format, _ := manifest.Get("format")
	entries, _ := manifest.Get("payloads")
	if format.Str != "aavanam-bundle-raw/v1" || len(entries.Arr) != 2 || len(section) != len("one\ntwo") {
		t.Fatalf("the raw form: %s", raw)
	}
	for _, name := range []string{"roots", "descriptors"} {
		in, _ := bundle.Get(name)
		got, _ := manifest.Get(name)
		if string(canon.Canonical(in)) != string(canon.Canonical(got)) {
			t.Errorf("the manifest has a different %s", name)
		}
	}
	if err := transfer.WriteRaw(&out, jsontext.O()); err == nil {
		t.Error("a raw form of a value that is not a bundle")
	}

	// The store with the bundle and the store with its raw form have the same content. The reader gives one
	// byte for each read, so the import does not need the raw bundle in one piece.
	plain, device := memstore.New(), memstore.New()
	request := string(canon.Canonical(jsontext.O(jsontext.M("bundle", bundle))))
	if e := call(t, ops.New(ops.Device, plain, registry()), "import", request); e.Status != "ok" {
		t.Fatalf("import of the bundle: %s", e.JSON())
	}
	imported := ops.New(ops.Device, device, registry()).ImportRaw(context.Background(), iotest.OneByteReader(bytes.NewReader(raw)))
	if imported.Status != "ok" || len(imported.Items) != 4 {
		t.Fatalf("import of the raw bundle: %s", imported.JSON())
	}
	if device.Fingerprint() != plain.Fingerprint() {
		t.Fatal("the raw bundle and its bundle gave a different store content")
	}

	text := string(line)
	entry := `{"digest":"` + jsontext.Digest([]byte("one\ntwo")) + `","size":7}`
	if !strings.Contains(text, entry) {
		t.Fatalf("the manifest has no entry %s: %s", entry, text)
	}
	with := func(old, new string) []byte {
		return []byte(strings.Replace(text, old, new, 1) + "\n" + string(section))
	}
	flipped := append([]byte{}, raw...)
	flipped[len(flipped)-1] ^= 1
	huge := strings.Replace(entry, `"size":7`, `"size":1099511627776`, 1)
	cases := []struct {
		name, code string
		raw        []byte
	}{
		{"no line feed byte", "malformed_json", line},
		{"a manifest that is not JSON", "malformed_json", append([]byte("{"), raw...)},
		{"a duplicate key", "duplicate_key", with(`{`, `{"format":"aavanam-bundle-raw/v1",`)},
		{"the bundle format", "field_type", with(`aavanam-bundle-raw/v1`, `aavanam-bundle/v1`)},
		{"a field that is not in the manifest", "field_type", with(`{`, `{"more":1,`)},
		{"a size that is not an integer", "field_type", with(`"size":7`, `"size":7.5`)},
		{"a negative size", "field_type", with(`"size":7`, `"size":-1`)},
		{"an entry with a field more", "field_type", with(`"size":7`, `"size":7,"more":1`)},
		{"two entries with one digest", "field_type", with(entry, entry+","+entry)},
		{"a withheld digest that is an entry", "field_type", with(`{`, `{"withheld":["`+jsontext.Digest([]byte("one\ntwo"))+`"],`)},
		{"a size with more bytes than the input", "field_type", with(entry, huge)},
		{"bytes with a different digest", "digest_mismatch", flipped},
		{"a section that ends early", "field_type", raw[:len(raw)-1]},
		{"a byte after the last entry", "field_type", append(append([]byte{}, raw...), 0)},
	}
	for _, c := range cases {
		storage := memstore.New()
		empty := storage.Fingerprint()
		e := ops.New(ops.Device, storage, registry()).ImportRaw(context.Background(), bytes.NewReader(c.raw))
		if e.Status != "failed" || len(e.Reasons) != 1 || e.Reasons[0].Code != c.code || len(e.Items) != 0 {
			t.Errorf("%s: %s, want failed with %s", c.name, e.JSON(), c.code)
		}
		if storage.Fingerprint() != empty {
			t.Errorf("%s: the store changed", c.name)
		}
	}
	// A reader that fails is a storage fault. Its message does not cross the API (STS-MAL-1).
	failing := ops.New(ops.Device, memstore.New(), registry()).ImportRaw(context.Background(), iotest.ErrReader(errors.New("private text")))
	if failing.Status != "failed" || failing.Reasons[0].Code != "storage_error" || strings.Contains(string(failing.JSON()), "private") {
		t.Errorf("a reader that fails: %s", failing.JSON())
	}
}
