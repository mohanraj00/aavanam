//go:build aix || (solaris && !illumos)

package main

// The service uses no socket lock on these systems. It keeps an existing socket file.
func lockSocketFile(string) (func(), error) {
	return nil, nil
}
