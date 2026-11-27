//go:build unix && !aix && (!solaris || illumos)

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// Take the lock before the socket check and bind. The listener holds the lock until it closes.
// Keep the file when the lock is released.
func lockSocketFile(path string) (func(), error) {
	// Use a nonblocking open in case the path has a special file.
	flags := syscall.O_RDWR | syscall.O_CREAT | syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_NONBLOCK
	// Wait for any socket bind to restore the process file mode mask.
	socketMask.Lock()
	fd, err := syscall.Open(path, flags, 0o600)
	socketMask.Unlock()
	if err != nil {
		return nil, fmt.Errorf("open the lock file: %w", err)
	}
	if err := takeSocketLock(fd); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	return func() { _ = syscall.Close(fd) }, nil
}

func takeSocketLock(fd int) error {
	var info syscall.Stat_t
	if err := syscall.Fstat(fd, &info); err != nil {
		return fmt.Errorf("get the lock file status: %w", err)
	}
	if info.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return errors.New("the lock file must be a regular file")
	}
	if int(info.Uid) != os.Geteuid() {
		return errors.New("the service user must own the lock file")
	}
	if info.Mode&0o077 != 0 {
		return errors.New("the lock file must have no group or other permissions")
	}
	const wait = 2 * time.Second
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EINTR {
			return fmt.Errorf("get the lock: %w", err)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("the lock wait ended after %s: %w", wait, err)
		}
		time.Sleep(min(25*time.Millisecond, remaining))
	}
}
