//go:build !linux

package browser

import "syscall"

// dieWithParent does nothing here: only Linux can tie a process's life to
// its parent's, and chromedp doesn't try elsewhere either.
func dieWithParent(*syscall.SysProcAttr) {}
