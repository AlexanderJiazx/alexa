package browser

import "syscall"

// dieWithParent has the kernel kill Chrome if the server exits without
// closing it (a crash, an OOM kill), as chromedp's own command setup does,
// so a restarted server doesn't find the old Chrome still holding the
// profile. It survives the exec through env and the switch to the user's
// UID: Go sets it after changing credentials, and env isn't set-user-ID.
func dieWithParent(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}
