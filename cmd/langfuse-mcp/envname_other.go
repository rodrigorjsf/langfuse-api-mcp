//go:build !windows

package main

// envName is the key envMap stores a variable under. On Linux and macOS
// variable names are case-sensitive, so a name stays as stored (#62).
func envName(name string) string { return name }
