package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestRequirements(t *testing.T) {
	if err := requirements(); err != nil {
		t.Fatal(err) // DEV-PRF-1: SQLite 3.45 or later, with FTS5
	}
}

func TestResetMakesTheTwoFiles(t *testing.T) {
	s := store{dir: t.TempDir()}
	for range 2 { // a second reset deletes the files and makes them again
		if err := s.Reset(); err != nil {
			t.Fatal(err)
		}
	}
	for name, role := range files {
		db, err := sql.Open("sqlite", filepath.Join(s.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		rows := map[string]string{}
		found, err := db.Query(`SELECT key, value FROM meta`)
		if err != nil {
			t.Fatal(err)
		}
		for found.Next() {
			var key, value string
			if err := found.Scan(&key, &value); err != nil {
				t.Fatal(err)
			}
			rows[key] = value
		}
		found.Close()
		db.Close()
		if len(rows) != 2 || rows["layout"] != Layout || rows["role"] != role {
			t.Fatalf("%s: meta rows %v (DEV-PRF-2)", name, rows)
		}
	}
	result, err := s.Call("device", []byte(`"get"`), []byte(`{}`), nil)
	if err != nil || string(result) != string(conformanceUnsupported) {
		t.Fatalf("result %s, error %v", result, err)
	}
}

const conformanceUnsupported = `{"status":"unsupported","reasons":[{"code":"unsupported_operation","refs":[],"capabilities":[]}],"operation":"get","operation_profile":"aavanam/base/v1","context":null}`
