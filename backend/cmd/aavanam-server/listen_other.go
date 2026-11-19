//go:build !unix

package main

import (
	"errors"
	"net"
)

// listenSocket refuses the write listener: this system has no file mode that keeps a socket for one user.
func listenSocket(string) (net.Listener, error) {
	return nil, errors.New("the write listener needs a Unix system")
}
