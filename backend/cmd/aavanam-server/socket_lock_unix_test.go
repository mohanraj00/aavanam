//go:build unix && !aix && (!solaris || illumos)

package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
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
	// A second start gets no lock while the first listener is open, so it keeps the socket of the first
	// listener. This is also true when the accept queue is full: on macOS, a connection to that socket
	// gives ECONNREFUSED, which is the same result as for a socket that no service has.
	t.Run("held by a listener", func(t *testing.T) {
		socket := filepath.Join(folder(t), "r.sock")
		first, err := listenSocket(socket, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		before, err := os.Lstat(socket)
		if err != nil {
			t.Fatal(err)
		}
		// The first listener accepts no connection, so each connection stays in the accept queue.
		var queue []net.Conn
		defer func() {
			for _, conn := range queue {
				_ = conn.Close()
			}
		}()
		full := false
		for len(queue) < 600 && !full {
			conn, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
			if err != nil {
				full = true
				t.Logf("the accept queue is full after %d connections: %v", len(queue), err)
				break
			}
			queue = append(queue, conn)
		}
		if !full && runtime.GOOS == "darwin" {
			t.Log("the accept queue did not fill, so the test did not get a refused connection")
		}
		if err := open(socket); !errors.Is(err, syscall.EWOULDBLOCK) {
			t.Fatalf("a second start must get no lock while the first listener is open: %v", err)
		}
		after, err := os.Lstat(socket)
		if err != nil || !os.SameFile(before, after) {
			t.Fatalf("the socket of the first listener must stay after the second start: %v", err)
		}
		accepted := make(chan error, 1)
		go func() {
			conn, err := first.Accept()
			if err == nil {
				_ = conn.Close()
			}
			accepted <- err
		}()
		if len(queue) == 0 {
			conn, err := net.DialTimeout("unix", socket, time.Second)
			if err != nil {
				t.Fatalf("the first listener must still get a connection: %v", err)
			}
			queue = append(queue, conn)
		}
		select {
		case err := <-accepted:
			if err != nil {
				t.Fatalf("the first listener must still accept a connection: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the first listener did not accept a connection within five seconds")
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		// The close of the listener releases the lock. A double close has no effect on the lock.
		_ = first.Close()
		if err := open(socket); err != nil {
			t.Fatalf("a start after the close of the first listener: %v", err)
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
