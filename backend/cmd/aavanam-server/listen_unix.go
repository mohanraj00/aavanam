//go:build unix

package main

import (
	"net"
	"syscall"
)

// listenSocket opens a Unix socket that only the user of the service can open. The mask applies when the
// system makes the socket file, so no other user can open it at any time. The mask is a value of the
// process. The service opens the socket before its listeners run, and no other part of it makes a file.
func listenSocket(path string) (net.Listener, error) {
	return socketListener(path, func(path string) (net.Listener, error) {
		old := syscall.Umask(0o177)
		defer syscall.Umask(old)
		return net.Listen("unix", path)
	})
}
