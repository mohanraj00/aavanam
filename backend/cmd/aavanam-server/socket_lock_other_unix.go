//go:build aix || (solaris && !illumos)

package main

// The service keeps an existing socket on these systems.
func lockSocketFile(string) (func(), error) {
	return nil, nil
}
