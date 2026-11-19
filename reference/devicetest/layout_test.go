package devicetest

import (
	"database/sql"
	"encoding/hex"
	"maps"
	"slices"
	"strings"
	"testing"
	"unicode"
)

type column struct {
	sqlType string
	key     bool
}

// DEV-PRF-2, DEV-LAY-1 to DEV-LAY-3 and DEV-OUT-1 specify the files and tables.
func TestSQLiteDeviceLayout(t *testing.T) {
	dir, writer, reader := newStores(t)
	const entry = "Maps and atlases.\n"
	_, unit := putEntry(t, writer, entry)
	call(t, reader, "import", fields{"bundle": exportBundle(t, writer, unit)}, "ok")

	common := map[string]map[string]column{
		"meta":       {"key": {"TEXT", true}, "value": {"TEXT", false}},
		"objects":    {"ref": {"BLOB", true}, "kind": {}, "logical_id": {"BLOB", false}, "collection": {"BLOB", false}, "canonical": {"BLOB", false}},
		"edges":      {"src": {"BLOB", true}, "dst": {"BLOB", true}, "field": {"", true}},
		"payloads":   {"digest": {"BLOB", true}, "size": {}, "data": {"BLOB", false}},
		"deferred":   {"digest": {"BLOB", true}, "ref": {"BLOB", true}},
		"contexts":   {"id": {"TEXT", true}, "roots": {"BLOB", false}, "capabilities": {"BLOB", false}},
		"closures":   {"root": {"BLOB", true}, "member": {"BLOB", true}},
		"table_rows": {"digest": {"BLOB", false}, "idx": {}, "row": {"BLOB", false}},
	}
	deviceTables := maps.Clone(common)
	deviceTables["imports"] = map[string]column{"seq": {"INTEGER", true}, "roots": {"BLOB", false}}
	deviceTables["text_docs"] = map[string]column{"id": {"INTEGER", true}, "digest": {"BLOB", false}}
	deviceTables["text_index"] = map[string]column{"tokens": {}}

	dev := openSQLiteFile(t, dir, "device.sqlite")
	out := openSQLiteFile(t, dir, "output.sqlite")
	t.Run("Device", func(t *testing.T) {
		checkLayout(t, dev, "device", deviceTables)
		checkTextIndex(t, dev, entry)
	})
	t.Run("Output", func(t *testing.T) {
		checkLayout(t, out, "output", common)
	})
	t.Run("KindEncoding", func(t *testing.T) {
		checkKindEncoding(t, dev, out)
	})
}

func checkLayout(t *testing.T, db *sql.DB, role string, want map[string]map[string]column) {
	t.Helper()
	var layout, storedRole string
	if err := db.QueryRow(`SELECT
		(SELECT value FROM meta WHERE key = 'layout'),
		(SELECT value FROM meta WHERE key = 'role')`).Scan(&layout, &storedRole); err != nil {
		t.Fatal(err)
	}
	if layout != "aavanam/device-sqlite/v1" || storedRole != role {
		t.Errorf("layout = %q, role = %q, want aavanam/device-sqlite/v1 and %q", layout, storedRole, role)
	}

	rows, err := db.Query(`SELECT name FROM pragma_table_list
		WHERE schema = 'main' AND type IN ('table', 'virtual') AND name NOT GLOB 'sqlite_*'
		ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if expected := slices.Sorted(maps.Keys(want)); !slices.Equal(tables, expected) {
		t.Fatalf("tables = %v, want %v", tables, expected)
	}
	for _, table := range tables {
		checkColumns(t, db, table, want[table])
	}
}

func checkColumns(t *testing.T, db *sql.DB, table string, want map[string]column) {
	t.Helper()
	rows, err := db.Query(`SELECT name, upper(type), pk FROM pragma_table_xinfo(?, 'main') WHERE hidden != 1`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]column{}
	for rows.Next() {
		var name, sqlType string
		var pk int
		if err := rows.Scan(&name, &sqlType, &pk); err != nil {
			t.Fatal(err)
		}
		got[name] = column{sqlType, pk != 0}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Errorf("%s columns = %v, want %v", table, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
	for name, expected := range want {
		actual, ok := got[name]
		if !ok {
			t.Errorf("%s.%s is missing", table, name)
			continue
		}
		if expected.sqlType != "" && actual.sqlType != expected.sqlType {
			t.Errorf("%s.%s type = %q, want %q", table, name, actual.sqlType, expected.sqlType)
		}
		if table != "table_rows" && actual.key != expected.key {
			t.Errorf("%s.%s primary key membership = %v, want %v", table, name, actual.key, expected.key)
		}
	}
}

// DEV-LAY-2 rule 2 applies to the kind string of a descriptor.
func checkKindEncoding(t *testing.T, dev, out *sql.DB) {
	t.Helper()
	var types [2]string
	for i, db := range []*sql.DB{dev, out} {
		if err := db.QueryRow(`SELECT upper(type) FROM pragma_table_info('objects', 'main') WHERE name = 'kind'`).Scan(&types[i]); err != nil {
			t.Fatal(err)
		}
	}
	if types != [2]string{"BLOB", "BLOB"} {
		t.Errorf("objects.kind types in device and output files = %q, want BLOB in both files", types)
	}
}

// DEV-FTS-1 to DEV-FTS-3 specify the index and its tokens.
func checkTextIndex(t *testing.T, db *sql.DB, entry string) {
	t.Helper()
	var ddl string
	if err := db.QueryRow(`SELECT sql FROM sqlite_schema WHERE name = 'text_index'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	compact := strings.Join(strings.FieldsFunc(strings.ToLower(ddl), unicode.IsSpace), "")
	const want = "createvirtualtabletext_indexusingfts5(tokens,tokenize='ascii',detail='full',content='')"
	if strings.TrimSuffix(compact, ";") != want {
		t.Errorf("unexpected text index declaration: %s", ddl)
	}

	token := func(word string) string { return "t" + strings.TrimPrefix(digest([]byte(word)), "sha256:")[:16] }
	phrase := `"` + token("maps") + " " + token("and") + " " + token("atlases") + `"`
	var indexedDigest []byte
	var tokens sql.NullString
	if err := db.QueryRow(`SELECT d.digest, x.tokens FROM text_index x
		JOIN text_docs d ON d.id = x.rowid WHERE text_index MATCH ?`, phrase).Scan(&indexedDigest, &tokens); err != nil {
		t.Fatal(err)
	}
	if strings.TrimPrefix(digest([]byte(entry)), "sha256:") != hex.EncodeToString(indexedDigest) || tokens.Valid {
		t.Errorf("index match has digest %x and tokens %v, want the entry digest and null tokens", indexedDigest, tokens)
	}
}
