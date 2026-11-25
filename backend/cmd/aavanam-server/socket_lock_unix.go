//go:build unix && !aix && (!solaris || illumos)

package main

import (
	"os"
	"syscall"
)

// Lock the directory so two starts cannot replace the same stale socket at the same time.
func lockSocketDir(dir string) (func(), error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
