// Package userenv lays out each user's private environment on disk:
// a workspace for files, a browser profile, and (in the container) a
// dedicated UID so users cannot read each other's data.
package userenv

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Envs struct {
	root    string
	sandbox bool
	uidBase int

	mu      sync.Mutex
	retired map[int64]bool // deleted users, whose folders Ensure won't make again
}

func New(dataDir string, sandbox bool, uidBase int) *Envs {
	return &Envs{root: filepath.Join(dataDir, "users"), sandbox: sandbox, uidBase: uidBase, retired: map[int64]bool{}}
}

// ErrRemoved is returned by Ensure for a user who has been deleted.
var ErrRemoved = errors.New("this user has been deleted")

// Retire refuses a user being deleted from now on: Ensure fails for them,
// so nothing (a message, a scheduled job, a run that ends and starts the
// next) makes their folders again once they're gone. User ids aren't
// reused, so it holds for good.
func (e *Envs) Retire(userID int64) {
	e.mu.Lock()
	e.retired[userID] = true
	e.mu.Unlock()
}

// Remove retires the user and deletes their folders: their own (the
// workspace, the browser profile, home), their screenshots and the cache
// made from their files.
func (e *Envs) Remove(userID int64) error {
	e.Retire(userID)
	env := e.For(userID)
	var errs []error
	for _, d := range []string{env.Root, env.Shots, env.Cache} {
		errs = append(errs, os.RemoveAll(d))
	}
	return errors.Join(errs...)
}

type Env struct {
	UserID    int64
	Root      string // private root (0700)
	Workspace string // files the agent can read/write
	Downloads string // browser downloads (inside workspace)
	Uploads   string // user uploads (inside workspace)
	Profile   string // Chrome profile
	Home      string
	Shots     string // stored screenshots for task replay
	Cache     string // files the server derives from the user's (attachment previews); only the server can write here
	UID, GID  int
}

func (e *Envs) For(userID int64) *Env {
	root := filepath.Join(e.root, fmt.Sprint(userID))
	env := &Env{
		UserID:    userID,
		Root:      root,
		Workspace: filepath.Join(root, "workspace"),
		Profile:   filepath.Join(root, "browser"),
		Home:      filepath.Join(root, "home"),
		Shots:     filepath.Join(e.root, "..", "shots", fmt.Sprint(userID)),
		Cache:     filepath.Join(e.root, "..", "cache", fmt.Sprint(userID)),
	}
	env.Downloads = filepath.Join(env.Workspace, "downloads")
	env.Uploads = filepath.Join(env.Workspace, "uploads")
	if e.sandbox {
		env.UID = e.uidBase + int(userID)
		env.GID = env.UID
	}
	return env
}

// Ensure creates the directories with correct ownership. It runs as root in
// the container, while the user's own processes own everything inside their
// root folder and could put a link where a folder should be; so it never
// follows one. A link (or a file) where one of the user's folders should be
// is moved aside and a real folder made in its place: refusing it instead
// would fail every message the user sends (Send calls Ensure), with no way
// to ask Alexa to put it right.
func (e *Envs) Ensure(userID int64) (*Env, error) {
	e.mu.Lock()
	retired := e.retired[userID]
	e.mu.Unlock()
	if retired {
		return nil, ErrRemoved
	}
	env := e.For(userID)
	// Each user's UID must be able to traverse the shared parents to reach
	// its own directory; 0711 allows that without letting anyone list them.
	for _, d := range []string{filepath.Dir(env.Root)} {
		if err := os.MkdirAll(d, 0o711); err != nil {
			return nil, err
		}
		if env.UID > 0 {
			if err := os.Chmod(d, 0o711); err != nil {
				return nil, err
			}
		}
	}
	// Only the server writes these, in folders the users can't enter.
	for _, d := range []string{env.Shots, env.Cache} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	// The user's root sits in a folder only the server can write, so the
	// user can't swap it; everything inside is opened through it, so a link
	// in there can't lead anywhere else.
	if err := os.Mkdir(env.Root, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	if fi, err := os.Lstat(env.Root); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		// Only the server can write where this is, so it's not the user's
		// doing: stop rather than guess.
		return nil, notFolder(filepath.Base(env.Root))
	}
	if env.UID > 0 {
		if err := os.Lchown(env.Root, env.UID, env.GID); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(env.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, d := range []string{env.Workspace, env.Downloads, env.Uploads, env.Profile, env.Home} {
		rel, err := filepath.Rel(env.Root, d)
		if err != nil {
			return nil, err
		}
		if err := ensureFolder(root, rel); err != nil {
			return nil, err
		}
		if env.UID > 0 {
			if err := root.Lchown(rel, env.UID, env.GID); err != nil {
				return nil, err
			}
		}
	}
	return env, nil
}

// ensureFolder makes rel a plain folder inside root. Whatever else is there
// (a link, a file) is renamed aside, never followed: renaming a link moves
// the link, not what it points to, and the user still has it under its new
// name.
func ensureFolder(root *os.Root, rel string) error {
	for try := 0; ; try++ {
		if err := root.Mkdir(rel, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		// Lstat reports a link as a link, never as a folder.
		fi, err := root.Lstat(rel)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if try > 0 {
			return notFolder(rel)
		}
		aside := fmt.Sprintf("%s.moved-%s", rel, time.Now().Format("20060102-150405.000"))
		if err := root.Rename(rel, aside); err != nil {
			return notFolder(rel)
		}
		slog.Warn("moved aside what was in place of a user folder", "root", root.Name(), "path", rel, "to", aside)
	}
}

// notFolder names the folder relative to the user's own folder: the full
// path is the server's business.
func notFolder(rel string) error {
	return fmt.Errorf("%s isn't a plain folder (a link or a file is in its place), so it won't be used", filepath.ToSlash(rel))
}

// Resolve maps a user-supplied path to its full path in the workspace,
// refusing one that a symlink leads outside of, whether or not the file
// exists yet. It's for display paths and for tools that hand a path to
// another program; reading, writing and deleting go through the workspace
// root instead (Open, Create, Remove), which can't be raced by a link put
// in place after the check.
func (env *Env) Resolve(p string) (string, error) {
	rel := env.Clean(p)
	root, err := env.OpenRoot()
	if err != nil {
		return "", err
	}
	defer root.Close()
	// Stat follows links inside the workspace and fails on one that leads
	// out, even when the last part of the path doesn't exist yet.
	if _, err := root.Stat(rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", pathError(p, err)
	}
	return filepath.Join(env.Workspace, rel), nil
}

// Rel converts an absolute workspace path to a display path.
func (env *Env) Rel(full string) string {
	rel, err := filepath.Rel(env.Workspace, full)
	if err != nil {
		return full
	}
	return rel
}

// MkdirAll creates dir and any missing parents, giving each new directory
// to the user so their sandboxed processes can use it. Inside the workspace
// it goes through the workspace root, so it can't be led outside by a link.
// Elsewhere in the user's folder (the shell's home/tmp) each folder on the
// way is made a plain one as Ensure does, through the user's root: their
// shell can swap home for a link mid-run, and the server, as root, would
// otherwise make (and give them) a folder wherever it led. Nothing outside
// the user's folder is made.
func (env *Env) MkdirAll(dir string) error {
	dir = filepath.Clean(dir)
	if rel, err := filepath.Rel(env.Workspace, dir); err == nil && filepath.IsLocal(rel) {
		root, err := env.OpenRoot()
		if err != nil {
			return err
		}
		defer root.Close()
		return env.mkdirAll(root, rel)
	}
	rel, err := filepath.Rel(env.Root, dir)
	if err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("%s is outside the user's folder", dir)
	}
	root, err := os.OpenRoot(env.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := range parts {
		d := filepath.Join(parts[:i+1]...)
		if err := ensureFolder(root, d); err != nil {
			return err
		}
		if env.UID > 0 {
			if err := root.Lchown(d, env.UID, env.GID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Chown gives a file to the user's UID (no-op outside the sandbox). It
// never follows a link.
func (env *Env) Chown(path string) {
	if env.UID > 0 {
		_ = os.Lchown(path, env.UID, env.GID)
	}
}
