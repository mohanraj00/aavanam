//go:build aix || (solaris && !illumos)

package main

// These systems have no directory lock. Keep an existing socket at the path.
func lockSocketDir(string) (func(), error) {
	return nil, nil
}
