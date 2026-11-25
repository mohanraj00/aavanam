//go:build unix && !aix && (!solaris || illumos)

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSocketLock(t *testing.T) {
	open := func(socket string) error {
		listener, err := listenSocket(socket, 0o600)
		if listener != nil {
			_ = listener.Close()
		}
		return err
	}
	t.Run("release", func(t *testing.T) {
		path := filepath.Join(folder(t), "r.sock.lock")
		unlock, err := lockSocketFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(path)
		unlock()
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) {
			t.Fatalf("the same lock file must stay after release: %v", err)
		}
		unlock, err = lockSocketFile(path)
		if err != nil {
			t.Fatal(err)
		}
		unlock()
	})
	t.Run("busy", func(t *testing.T) {
		socket := filepath.Join(folder(t), "r.sock")
		file, err := os.OpenFile(socket+".lock", os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		started := time.Now()
		go func() { result <- open(socket) }()
		select {
		case err := <-result:
			if !errors.Is(err, syscall.EWOULDBLOCK) {
				t.Fatalf("the lock must be busy: %v", err)
			}
			if elapsed := time.Since(started); elapsed < time.Second {
				t.Errorf("the lock wait ended too soon: %v", elapsed)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the lock wait did not end within five seconds")
		}
	})
	t.Run("symbolic link", func(t *testing.T) {
		dir := folder(t)
		socket := filepath.Join(dir, "r.sock")
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, socket+".lock"); err != nil {
			t.Fatal(err)
		}
		if err := open(socket); err == nil || !strings.Contains(err.Error(), "open the lock file") {
			t.Fatalf("the start must refuse a symbolic link at the lock path: %v", err)
		}
	})
	t.Run("group and other permissions", func(t *testing.T) {
		socket := filepath.Join(folder(t), "r.sock")
		path := socket + ".lock"
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := open(socket); err == nil || !strings.Contains(err.Error(), "group or other permissions") {
			t.Fatalf("the start must refuse a lock file with mode 0644: %v", err)
		}
	})
}
