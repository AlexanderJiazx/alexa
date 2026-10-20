// Package userenv lays out each user's private environment on disk:
// a workspace for files, a browser profile, and (in the container) a
// dedicated UID so users cannot read each other's data.
package userenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Envs struct {
	root    string
	sandbox bool
	uidBase int
}

func New(dataDir string, sandbox bool, uidBase int) *Envs {
	return &Envs{root: filepath.Join(dataDir, "users"), sandbox: sandbox, uidBase: uidBase}
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
	}
	env.Downloads = filepath.Join(env.Workspace, "downloads")
	env.Uploads = filepath.Join(env.Workspace, "uploads")
	if e.sandbox {
		env.UID = e.uidBase + int(userID)
		env.GID = env.UID
	}
	return env
}

// Ensure creates the directories with correct ownership.
func (e *Envs) Ensure(userID int64) (*Env, error) {
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
	for _, d := range []string{env.Root, env.Workspace, env.Downloads, env.Uploads, env.Profile, env.Home, env.Shots} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
		if env.UID > 0 && d != env.Shots {
			if err := os.Chown(d, env.UID, env.GID); err != nil {
				return nil, err
			}
		}
	}
	return env, nil
}

// Resolve maps a user-supplied path into the workspace, refusing escapes.
func (env *Env) Resolve(p string) (string, error) {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "/workspace")
	if filepath.IsAbs(p) {
		// Allow absolute paths only if they are inside the workspace.
		if rel, err := filepath.Rel(env.Workspace, p); err == nil && !strings.HasPrefix(rel, "..") {
			return p, nil
		}
		p = strings.TrimLeft(p, "/")
	}
	full := filepath.Join(env.Workspace, filepath.Clean("/"+p))
	rel, err := filepath.Rel(env.Workspace, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q is outside the workspace", p)
	}
	// Refuse symlinks pointing outside.
	if real, err := filepath.EvalSymlinks(full); err == nil {
		wsReal, _ := filepath.EvalSymlinks(env.Workspace)
		if r, err := filepath.Rel(wsReal, real); err != nil || strings.HasPrefix(r, "..") {
			return "", fmt.Errorf("path %q resolves outside the workspace", p)
		}
	}
	return full, nil
}

// Rel converts an absolute workspace path to a display path.
func (env *Env) Rel(full string) string {
	rel, err := filepath.Rel(env.Workspace, full)
	if err != nil {
		return full
	}
	return rel
}

// Chown gives a file to the user's UID (no-op outside the sandbox).
// MkdirAll creates dir and any missing parents, giving each new directory
// to the user so their sandboxed processes can use it.
func (env *Env) MkdirAll(dir string) error {
	var missing []string
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Lstat(d); err == nil {
			break
		}
		missing = append(missing, d)
		if d == filepath.Dir(d) {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		env.Chown(missing[i])
	}
	return nil
}

func (env *Env) Chown(path string) {
	if env.UID > 0 {
		_ = os.Lchown(path, env.UID, env.GID)
	}
}
