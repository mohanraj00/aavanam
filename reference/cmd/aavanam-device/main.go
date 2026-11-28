// Command aavanam-device is the conformance driver of the device store. It connects the Python vector
// runner to a device store and its local output store over the line protocol of package conformance.
// The two stores are the SQLite files of document 21 (package device). A reset deletes the files and
// makes new ones.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mohanraj00/aavanam/backend/conformance"
	"github.com/mohanraj00/aavanam/reference/device"
)

// files keeps the stores that are open, so that a reset can close them.
type files struct {
	dir    string
	stores []*device.Store
}

// remove closes the stores that are open and deletes the two files.
func (f *files) remove() error {
	for _, s := range f.stores {
		s.Close()
	}
	f.stores = nil
	for _, name := range []string{device.DeviceFile, device.OutputFile} {
		path := filepath.Join(f.dir, name)
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

// open closes the stores that are open, deletes the two files and makes new ones (DEV-LAY-1).
func (f *files) open() (map[string]conformance.Backing, error) {
	if err := f.remove(); err != nil {
		return nil, err
	}
	if err := device.Create(f.dir); err != nil {
		return nil, err
	}
	return f.use()
}

// stored replaces the two files with copies of the two files of the folder `from`, which an earlier
// release wrote, and it opens the copies (MIG-VER-1, DEV-MIG-1). The files of `from` do not change.
func (f *files) stored(from string) (map[string]conformance.Backing, error) {
	if err := f.remove(); err != nil {
		return nil, err
	}
	for _, name := range []string{device.DeviceFile, device.OutputFile} {
		data, err := os.ReadFile(filepath.Join(from, name))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(f.dir, name), data, 0o600); err != nil {
			return nil, err
		}
	}
	return f.use()
}

// use opens the two files of the directory.
func (f *files) use() (map[string]conformance.Backing, error) {
	dev, out, err := device.Open(f.dir)
	if err != nil {
		return nil, err
	}
	f.stores = []*device.Store{dev, out}
	return map[string]conformance.Backing{"device": dev, "output": out}, nil
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
	f := &files{dir: *dir}
	s := &conformance.Core{Open: f.open, Stored: f.stored}
	if err := s.Reset(); err != nil {
		fmt.Fprintln(os.Stderr, "aavanam-device:", err)
		os.Exit(2)
	}
	if err := conformance.Serve(os.Stdin, os.Stdout, s, conformance.Hello{Ready: true}); err != nil {
		fmt.Fprintln(os.Stderr, "aavanam-device:", err)
		os.Exit(1)
	}
}
