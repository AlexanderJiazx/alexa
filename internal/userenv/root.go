package userenv

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Workspace I/O. Every read, write and delete of a user's files goes through
// an os.Root opened on their workspace: the kernel checks each part of the
// path as it's opened, so no path (absolute, relative, through a symlink, or
// to a file that doesn't exist yet) can reach the server's data or another
// user's, and there's no gap between a check and the use. The server runs
// as root in the container, so this is what keeps a link the user (or a
// page that tricked the agent) made from turning into a read or a write as
// root.

// ErrOutside is returned for a path that leads outside the workspace.
var ErrOutside = errors.New("outside the workspace")

// Clean turns a user- or model-supplied path into one relative to the
// workspace, "." for the workspace itself. It only looks at the text: an
// absolute path inside the workspace is made relative, and anything else is
// taken as relative to the workspace, so "../x" and "/etc/x" stay inside.
func (env *Env) Clean(p string) string {
	p = strings.TrimSpace(p)
	if p == "/workspace" || strings.HasPrefix(p, "/workspace/") {
		p = strings.TrimPrefix(p, "/workspace")
	}
	if filepath.IsAbs(p) {
		if rel, err := filepath.Rel(env.Workspace, filepath.Clean(p)); err == nil && filepath.IsLocal(rel) {
			p = rel
		}
	}
	rel := strings.TrimPrefix(filepath.Clean("/"+p), "/")
	if rel == "" {
		return "."
	}
	return rel
}

// OpenRoot opens the workspace as an os.Root. The caller closes it.
func (env *Env) OpenRoot() (*os.Root, error) {
	root, err := os.OpenRoot(env.Workspace)
	if err != nil {
		return nil, err
	}
	// The user's processes own the folder the workspace is in and could put
	// a link in its place, which os.OpenRoot would follow. Checking after
	// opening that what was opened is the plain folder at that path catches
	// a swap in between too.
	fi, err := os.Lstat(env.Workspace)
	if err == nil {
		var opened fs.FileInfo
		if opened, err = root.Stat("."); err == nil && (!fi.IsDir() || !os.SameFile(fi, opened)) {
			err = notFolder("workspace")
		}
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

// Stat describes a workspace file, following links that stay inside.
func (env *Env) Stat(p string) (fs.FileInfo, error) {
	root, err := env.OpenRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	fi, err := root.Stat(env.Clean(p))
	if err != nil {
		return nil, pathError(p, err)
	}
	return fi, nil
}

// Open opens a workspace file (or folder) for reading. The caller closes
// it.
func (env *Env) Open(p string) (*os.File, error) {
	root, err := env.OpenRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := env.openFile(root, env.Clean(p), p, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	if err := env.checkOwner(f, p); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// openFile opens rel in root without waiting on what isn't a plain file.
// Opening a FIFO blocks until something opens its other end, and the
// user's shell can make one where a file should be: a read or a write of it
// would hang the run, or a download request, for good. So it's opened
// non-blocking, and anything but a regular file (or, to read, a folder) is
// refused before it's used.
func (env *Env) openFile(root *os.Root, rel, p string, flag int, perm fs.FileMode) (*os.File, error) {
	f, err := root.OpenFile(rel, flag|syscall.O_NONBLOCK, perm)
	if errors.Is(err, syscall.ENXIO) { // a FIFO no one reads, or a socket
		return nil, notRegular(env.Clean(p))
	}
	if err != nil {
		return nil, pathError(p, err)
	}
	fi, err := f.Stat()
	if err == nil && !fi.Mode().IsRegular() && !(fi.IsDir() && flag&(os.O_WRONLY|os.O_RDWR) == 0) {
		err = notRegular(env.Clean(p))
	}
	if err == nil {
		err = blocking(f)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// blocking clears O_NONBLOCK from a regular file or folder opened with it.
func blocking(f *os.File) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) { serr = syscall.SetNonblock(int(fd), false) }); err != nil {
		return err
	}
	return serr
}

func notRegular(rel string) error {
	return fmt.Errorf("%s isn't a regular file", rel)
}

// ReadFile reads a workspace file of at most max bytes.
func (env *Env) ReadFile(p string, max int64) ([]byte, error) {
	f, err := env.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.IsDir() {
		return nil, fmt.Errorf("%s is a folder", env.Clean(p))
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s is too large (over %d MB)", env.Clean(p), max>>20)
	}
	return data, nil
}

// Create opens a workspace file for writing, creating it and any missing
// folders (all given to the user). flag adds to O_WRONLY|O_CREATE: O_EXCL
// for a new file only, O_TRUNC to overwrite, O_APPEND to add. The caller
// closes it.
func (env *Env) Create(p string, flag int) (*os.File, error) {
	rel := env.Clean(p)
	if rel == "." {
		return nil, errors.New("give the file a name")
	}
	root, err := env.OpenRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := env.mkdirAll(root, filepath.Dir(rel)); err != nil {
		return nil, pathError(p, err)
	}
	// Truncating waits until the file is known to be the user's own (see
	// checkOwner), so a hard link can't be used to wipe someone else's.
	trunc := flag&os.O_TRUNC != 0
	f, err := env.openFile(root, rel, p, (flag&^os.O_TRUNC)|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := env.checkOwner(f, p); err != nil {
		f.Close()
		return nil, err
	}
	if trunc {
		if err := f.Truncate(0); err != nil {
			f.Close()
			return nil, err
		}
	}
	if env.UID > 0 {
		// On the open file, so it's this file whatever the path is now.
		_ = f.Chown(env.UID, env.GID)
	}
	return f, nil
}

// Remove deletes a workspace file or folder with everything in it. A link
// is removed, never what it points to. The workspace itself and its uploads
// and downloads folders can't be removed.
func (env *Env) Remove(p string) error {
	rel := env.Clean(p)
	for _, keep := range []string{env.Workspace, env.Uploads, env.Downloads} {
		if k, err := filepath.Rel(env.Workspace, keep); err == nil && rel == k {
			return fmt.Errorf("%s can't be deleted", rel)
		}
	}
	root, err := env.OpenRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err := root.Lstat(rel); err != nil {
		return pathError(p, err)
	}
	if err := root.RemoveAll(rel); err != nil {
		return pathError(p, err)
	}
	return nil
}

// mkdirAll creates a folder and its missing parents inside root, giving
// each new one to the user.
func (env *Env) mkdirAll(root *os.Root, rel string) error {
	if rel == "." || rel == "" {
		return nil
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := range parts {
		d := filepath.Join(parts[:i+1]...)
		err := root.Mkdir(d, 0o700)
		if err == nil {
			if env.UID > 0 {
				_ = root.Lchown(d, env.UID, env.GID)
			}
			continue
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
	}
	return nil
}

// checkOwner refuses, in the sandbox, a file the user doesn't own that has
// another name somewhere: a hard link the user made to a file of the
// server's (the database, the vault key) or another user's. os.Root can't
// see hard links, and the server would otherwise read or overwrite the file
// as root. Files the server writes for the user are given to them, so a
// file of someone else's with a single name is an old one, and is allowed.
func (env *Env) checkOwner(f *os.File, p string) error {
	if env.UID <= 0 {
		return nil
	}
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || fi.IsDir() {
		return nil
	}
	if int(st.Uid) != env.UID && uint64(st.Nlink) > 1 {
		return fmt.Errorf("%s is a link to a file that isn't yours", env.Clean(p))
	}
	return nil
}

// pathError says plainly when a path leads outside the workspace; other
// errors (a file that doesn't exist, say) keep their type.
func pathError(p string, err error) error {
	if err != nil && strings.Contains(err.Error(), "escapes from parent") {
		return fmt.Errorf("path %q is %w", strings.TrimSpace(p), ErrOutside)
	}
	return err
}
