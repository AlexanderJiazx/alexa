//go:build !linux

package main

// hideEnviron does nothing here: only Linux lets a process hide the
// environment it started with from its own UID. The container runs Linux.
func hideEnviron() error { return nil }
