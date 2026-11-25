package device

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/core/ops"
)

func open(t *testing.T) (string, *Store, *Store) {
	t.Helper()
	dir := t.TempDir()
	if err := Create(dir); err != nil {
		t.Fatal(err)
	}
	dev, out, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dev.Close(); out.Close() })
	return dir, dev, out
}

func ref(n byte) string { return "sha256:" + strings.Repeat(string("0123456789abcdef"[n]), 64) }

// A file with a different `meta` row is not a store of the layout (DEV-PRF-2).
func TestOpenChecksMeta(t *testing.T) {
	dir := t.TempDir()
	if err := Create(dir); err != nil {
		t.Fatal(err)
	}
	if err := Create(dir); err == nil {
		t.Fatal("a second Create of one directory did not fail")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, OutputFile))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE meta SET value = 'device' WHERE key = 'role'`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(dir); err == nil {
		t.Fatal("Open used an output file with the role device")
	}
}

// The local output store reads the objects and the bytes of the device file, and writes only to the
// output file (DEV-OUT-2, BUN-IMP-8).
func TestOutputReadsTheDevice(t *testing.T) {
	_, dev, out := open(t)
	ctx := context.Background()
	desc := []byte(`{"descriptor_profile":"aavanam/base/v1","kind":"binding-set"}`)
	dev.Session(ctx).Update(func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: ref(1), Kind: "binding-set", Text: desc})
		tx.PutBlob(ref(2), []byte("abc"))
	})
	reader := out.Session(ctx)
	if kind, text, ok := reader.Object(ref(1)); !ok || kind != "binding-set" || string(text) != string(desc) {
		t.Fatalf("output Object: %q %q %v", kind, text, ok)
	}
	if data, ok := reader.Blob(ref(2)); !ok || string(data) != "abc" {
		t.Fatalf("output Blob: %q %v", data, ok)
	}
	before := dev.Fingerprint()
	out.Session(ctx).Update(func(tx ops.Tx) {
		tx.PutObject(ops.Object{Ref: ref(1), Kind: "binding-set", Text: desc}) // the device has it
		tx.PutBlob(ref(3), []byte("xyz"))
	})
	if dev.Fingerprint() != before {
		t.Fatal("a write of the local output store changed the device file")
	}
	if _, ok := dev.Session(ctx).Blob(ref(3)); ok {
		t.Fatal("the device store reads the bytes of the output file")
	}
	var n int
	if err := out.db.QueryRow(`SELECT count(*) FROM main.objects`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the output file has %d objects, error %v", n, err)
	}
}

// The text index gives each digest whose document has the tokens as one phrase (DEV-FTS-2, DEV-FTS-4).
// The output store has no index of its own (DEV-OUT-1).
func TestTextIndex(t *testing.T) {
	_, dev, out := open(t)
	ctx := context.Background()
	dev.Session(ctx).Update(func(tx ops.Tx) {
		index := tx.(ops.TextIndex)
		index.PutTextTokens(ref(1), []string{"ta", "tb", "tc"})
		index.PutTextTokens(ref(2), []string{"tb", "ta"})
		index.PutTextTokens(ref(1), []string{"tz"}) // a second call has no effect
		index.PutTextTokens(ref(3), nil)
	})
	for _, s := range []*Store{dev, out} {
		index := s.Session(ctx).(ops.TextIndex)
		all := []string{ref(1), ref(2), ref(3), ref(4)}
		check := func(tokens, digests, candidates, unindexed []string) {
			t.Helper()
			can, not := index.TextCandidates(tokens, digests)
			sort.Strings(can)
			if !reflect.DeepEqual(can, candidates) || !reflect.DeepEqual(not, unindexed) {
				t.Errorf("%s: %v in %d digests: candidates %v, not in the index %v", s.role, tokens, len(digests), can, not)
			}
		}
		check([]string{"ta", "tb"}, all, []string{ref(1)}, []string{ref(4)})
		check([]string{"tb"}, all, []string{ref(1), ref(2)}, []string{ref(4)})
		check([]string{"tb"}, []string{ref(2), ref(3)}, []string{ref(2)}, nil) // only the digests of the list
		check([]string{"tz"}, all, nil, []string{ref(4)})
		check([]string{"ta"}, nil, nil, nil)
		if !index.TextIndexed(ref(3)) || index.TextIndexed(ref(4)) {
			t.Errorf("%s: TextIndexed", s.role)
		}
	}
	out.Session(ctx).Update(func(tx ops.Tx) { tx.(ops.TextIndex).PutTextTokens(ref(4), []string{"ta"}) })
	if out.Session(ctx).(ops.TextIndex).TextIndexed(ref(4)) {
		t.Fatal("the local output store added a document to the text index")
	}
}

// Each `ok` import adds one `imports` row, and a failed update adds none (DEV-UPD-1, DEV-UPD-2).
func TestImportRows(t *testing.T) {
	_, dev, out := open(t)
	ctx := context.Background()
	for range 2 {
		dev.Session(ctx).Update(func(tx ops.Tx) { tx.(ops.ImportLog).PutImport([]string{ref(1)}) })
	}
	func() {
		defer func() { recover() }()
		dev.Session(ctx).Update(func(tx ops.Tx) {
			tx.(ops.ImportLog).PutImport([]string{ref(2)})
			panic("the import is not ok")
		})
	}()
	rows, err := dev.db.Query(`SELECT seq, roots FROM imports ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var seq int
		var roots []byte
		if err := rows.Scan(&seq, &roots); err != nil {
			t.Fatal(err)
		}
		got = append(got, string(rune('0'+seq))+string(roots))
	}
	want := []string{`1["` + ref(1) + `"]`, `2["` + ref(1) + `"]`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imports %v, want %v", got, want)
	}
	out.Session(ctx).Update(func(tx ops.Tx) {
		if _, ok := tx.(ops.ImportLog); ok {
			t.Error("the local output store has an import record")
		}
	})
}

// A relative directory gives the same stores as an absolute one: the attachment of the device file has
// no URI authority (DEV-OUT-2).
func TestOpenRelativeDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("store", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create("store"); err != nil {
		t.Fatal(err)
	}
	dev, out, err := Open("store")
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	defer out.Close()
	ctx := context.Background()
	dev.Session(ctx).Update(func(tx ops.Tx) { tx.PutBlob(ref(2), []byte("abc")) })
	if data, ok := out.Session(ctx).Blob(ref(2)); !ok || string(data) != "abc" {
		t.Fatalf("output Blob: %q %v", data, ok)
	}
}
