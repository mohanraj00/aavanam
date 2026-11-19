package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/reference/device"
)

func TestRequirements(t *testing.T) {
	if err := requirements(); err != nil {
		t.Fatal(err) // DEV-PRF-1: SQLite 3.45 or later, with FTS5
	}
}

func TestResetMakesTheTwoFiles(t *testing.T) {
	f := &files{dir: t.TempDir()}
	s := &conformance.Core{Open: f.open}
	for range 2 { // a second reset closes the stores, deletes the files and makes them again
		if err := s.Reset(); err != nil {
			t.Fatal(err)
		}
	}
	for name, role := range map[string]string{device.DeviceFile: "device", device.OutputFile: "output"} {
		db, err := sql.Open("sqlite", filepath.Join(f.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var layout, got string
		err = db.QueryRow(`SELECT (SELECT value FROM meta WHERE key = 'layout'), (SELECT value FROM meta WHERE key = 'role')`).
			Scan(&layout, &got)
		db.Close()
		if err != nil || layout != device.Layout || got != role {
			t.Fatalf("%s: meta rows %q %q, error %v (DEV-PRF-2)", name, layout, got, err)
		}
	}
	// BUN-IMP-6: the device store refuses `put`.
	result, err := s.Call("device", []byte(`"put"`), []byte(`{"descriptors":[]}`), nil)
	if err != nil || !strings.Contains(string(result), `"read_only_store"`) {
		t.Fatalf("result %s, error %v", result, err)
	}
}
