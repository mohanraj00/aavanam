// Command aavanam-device is the conformance driver of the device store. It connects the Python vector
// runner to a device store and its local output store over the line protocol of package conformance.
//
// In this version each call gives `unsupported_operation`. A reset makes the two SQLite files of
// document 21 with their `meta` rows (DEV-PRF-2, DEV-LAY-1). A later change adds the device store.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/mohanraj00/aavanam/backend/conformance"
	// The device store applies the checks of the core. This import keeps the requirements of the module
	// the same when the store comes.
	_ "github.com/mohanraj00/aavanam/backend/core/check"
)

// Layout is the identity of the layout profile (DEV-PRF-1).
const Layout = "aavanam/device-sqlite/v1"

// The files of a device store and their roles (DEV-LAY-1).
var files = map[string]string{"device.sqlite": "device", "output.sqlite": "output"}

type store struct {
	conformance.Null
	dir string
}

// Reset deletes the two files and makes new ones.
func (s store) Reset() error {
	for name, role := range files {
		path := filepath.Join(s.dir, name)
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := create(path, role); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (s store) Call(_ string, operation, _ json.RawMessage, _ conformance.Validate) (json.RawMessage, error) {
	return conformance.Unsupported(operation), nil
}

func create(path, role string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO meta (key, value) VALUES ('layout', ?), ('role', ?)`, Layout, role)
	return err
}

// requirements checks that the SQLite of the driver agrees with DEV-PRF-1: version 3.45 or later, with FTS5.
func requirements() error {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()
	var text string
	var fts5 bool
	if err := db.QueryRow(`SELECT sqlite_version(), sqlite_compileoption_used('ENABLE_FTS5')`).Scan(&text, &fts5); err != nil {
		return err
	}
	var major, minor, patch int
	if _, err := fmt.Sscanf(text, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return fmt.Errorf("the SQLite version %q has no known form", text)
	}
	number := major*1000000 + minor*1000 + patch
	if number < 3045000 {
		return fmt.Errorf("SQLite %s is older than 3.45 (DEV-PRF-1)", text)
	}
	if !fts5 {
		return fmt.Errorf("SQLite has no FTS5 (DEV-PRF-1)")
	}
	return nil
}

func main() {
	dir := flag.String("dir", "", "the directory of device.sqlite and output.sqlite (default: a new temporary directory)")
	flag.Parse()
	if err := requirements(); err != nil {
		fmt.Fprintln(os.Stderr, "aavanam-device:", err)
		os.Exit(2)
	}
	if *dir == "" {
		temp, err := os.MkdirTemp("", "aavanam-device-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "aavanam-device:", err)
			os.Exit(2)
		}
		defer os.RemoveAll(temp)
		*dir = temp
	}
	s := store{dir: *dir}
	if err := s.Reset(); err != nil {
		fmt.Fprintln(os.Stderr, "aavanam-device:", err)
		os.Exit(2)
	}
	if err := conformance.Serve(os.Stdin, os.Stdout, s, conformance.Hello{Ready: true}); err != nil {
		fmt.Fprintln(os.Stderr, "aavanam-device:", err)
		os.Exit(1)
	}
}
