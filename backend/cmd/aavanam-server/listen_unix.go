//go:build unix

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func listenSocket(path string, mode os.FileMode) (net.Listener, error) {
	return socketListener(path, mode, bindSocket)
}

var socketMask sync.Mutex

func bindSocket(path string, mode os.FileMode) (net.Listener, error) {
	// The file mode mask applies to the process. Set the mask only during the bind.
	socketMask.Lock()
	defer socketMask.Unlock()
	old := syscall.Umask(int(0o777 &^ mode))
	defer syscall.Umask(old)
	return net.Listen("unix", path)
}

func socketListener(path string, mode os.FileMode, listen func(string, os.FileMode) (net.Listener, error)) (net.Listener, error) {
	// On Linux, an abstract socket has no file or mode. Use only a file path.
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return nil, errors.New("the path of the socket must be an absolute file path")
	}
	lockPath := path + ".lock"
	unlock, err := lockSocketFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("the socket lock %q: %w", lockPath, err)
	}
	if unlock != nil {
		defer unlock()
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("the path has a file that is not a socket")
		}
		if unlock == nil {
			return nil, fmt.Errorf("the socket %q already exists. Make sure that no service uses the socket. Then remove the file", path)
		}
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			_ = conn.Close()
			return nil, errors.New("a service that runs has the socket")
		}
		// A permission error or a timeout gives no evidence that the socket is stale.
		if !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, fmt.Errorf("cannot check if the socket is stale: %w", err)
		}
		current, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !os.SameFile(info, current) {
			return nil, errors.New("the socket changed during the check")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := listen(path, mode)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	if info.Mode() != os.ModeSocket|mode {
		_ = listener.Close()
		return nil, fmt.Errorf("the socket file must have mode %04o", mode)
	}
	return listener, nil
}
