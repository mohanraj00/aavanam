//go:build !unix

package main

import (
	"errors"
	"net"
	"os"
)

func listenSocket(string, os.FileMode) (net.Listener, error) {
	return nil, errors.New("a socket listener with a file mode needs a Unix system")
}
