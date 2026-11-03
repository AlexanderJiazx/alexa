package main

import "syscall"

// hideEnviron keeps processes of the server's own UID from reading its
// /proc files: /proc/<pid>/environ shows the environment the server started
// with, the API keys dropSecrets took out of Go's copy included, and
// without SANDBOX_UIDS the shell tool runs as that UID. A process that
// isn't dumpable shows those files (and lets itself be traced) to root
// alone. What it starts is dumpable again once it execs.
func hideEnviron() error {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0); errno != 0 {
		return errno
	}
	return nil
}
