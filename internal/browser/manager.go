// Package browser runs one Chrome instance per user (own profile directory,
// optionally own UID) with one tab per task. Tabs support agent actions
// (DOM-indexed and coordinate-based), live JPEG screencasting, and human
// takeover via forwarded input events.
package browser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

type Options struct {
	ChromePath string
	Headless   bool
	Width      int
	Height     int
	IdleAfter  time.Duration
	// RestoreURL, if set, gives the last known URL of a user's tab so one
	// recreated after a restart or idle shutdown reopens where it was.
	RestoreURL func(userID int64, key string) string
	// KeepAlive, if set, reports whether a user's browser must stay up
	// however long it has been idle: the user is in control of it, or Alexa
	// is waiting for them (a question, a take-over, a sign-in).
	KeepAlive func(userID int64) bool
	// Downloaded, if set, is called when a file a page downloaded has
	// landed in the user's downloads folder.
	Downloaded func(userID int64)
}

// UserEnv describes the on-disk environment of a user.
type UserEnv struct {
	UserID int64
	// Root is the user's own folder, which holds the others; the profile's
	// parent if not given.
	Root        string
	ProfileDir  string
	DownloadDir string
	HomeDir     string
	UID, GID    int // 0 = don't switch
}

type Manager struct {
	opts     Options
	mu       sync.Mutex
	browsers map[int64]*Browser
	pending  map[int64]*pendingLaunch // launches in progress, so they don't block other users
	removed  map[int64]bool           // deleted users: no browser starts for them again (Remove)
	envFn    func(userID int64) UserEnv
	onEvent  func(userID int64, taskID string, ev TabEvent)
}

// ErrRemoved is returned by Get for a user who has been deleted.
var ErrRemoved = errors.New("this user has been deleted")

// TabEvent is emitted for navigation/cursor changes so the UI can follow along.
type TabEvent struct {
	Type  string  `json:"type"` // nav | cursor | closed | crashed
	URL   string  `json:"url,omitempty"`
	Title string  `json:"title,omitempty"`
	X     float64 `json:"x,omitempty"`
	Y     float64 `json:"y,omitempty"`
	Click bool    `json:"click,omitempty"`
}

func NewManager(opts Options, envFn func(int64) UserEnv, onEvent func(int64, string, TabEvent)) *Manager {
	m := &Manager{opts: opts, browsers: map[int64]*Browser{}, pending: map[int64]*pendingLaunch{}, removed: map[int64]bool{}, envFn: envFn, onEvent: onEvent}
	go m.reaper()
	return m
}

func (m *Manager) reaper() {
	for range time.Tick(time.Minute) {
		m.reap()
	}
}

// reap closes the browsers no one has used for IdleAfter. One someone is
// watching stays up (idleFor), and so does one the user must come back to
// (KeepAlive): closing it would lose the page they're on, and an open live
// view would only start it again, reloaded.
func (m *Manager) reap() {
	m.mu.Lock()
	idle := map[int64]*Browser{}
	for id, b := range m.browsers {
		if b.idleFor() > m.opts.IdleAfter {
			idle[id] = b
		}
	}
	m.mu.Unlock()
	for id, b := range idle {
		if m.opts.KeepAlive != nil && m.opts.KeepAlive(id) {
			continue
		}
		m.mu.Lock()
		still := m.browsers[id] == b && b.idleFor() > m.opts.IdleAfter
		if still {
			delete(m.browsers, id)
		}
		m.mu.Unlock()
		if still {
			slog.Info("closing idle browser", "user", id)
			go b.Close()
		}
	}
}

type pendingLaunch struct {
	done chan struct{}
	b    *Browser
	err  error
}

// Get returns the user's browser, launching it if needed. Concurrent callers
// for the same user share one launch; other users are never blocked by it.
func (m *Manager) Get(userID int64) (*Browser, error) {
	m.mu.Lock()
	if m.removed[userID] {
		m.mu.Unlock()
		return nil, ErrRemoved
	}
	if b, ok := m.browsers[userID]; ok {
		if b.alive() {
			b.touch()
			m.mu.Unlock()
			return b, nil
		}
		// Chrome died (the crash watch in launch closes it too): closing it
		// ends its live views, which reconnect to the browser launched now.
		delete(m.browsers, userID)
		go b.Close()
	}
	if p, ok := m.pending[userID]; ok {
		m.mu.Unlock()
		<-p.done
		return p.b, p.err
	}
	p := &pendingLaunch{done: make(chan struct{})}
	m.pending[userID] = p
	m.mu.Unlock()

	p.b, p.err = m.launch(m.envFn(userID))

	m.mu.Lock()
	delete(m.pending, userID)
	gone := p.err == nil && m.removed[userID]
	if p.err == nil && !gone {
		m.browsers[userID] = p.b
	}
	m.mu.Unlock()
	if gone {
		// Removed while it started: it goes at once, before Remove
		// returns.
		p.b.Close()
		p.b, p.err = nil, ErrRemoved
	}
	close(p.done)
	return p.b, p.err
}

// Prewarm starts the user's browser in the background so the first browser
// action of a task doesn't wait for Chrome to launch.
func (m *Manager) Prewarm(userID int64) {
	go func() {
		if _, err := m.Get(userID); err != nil {
			slog.Warn("browser prewarm failed", "user", userID, "err", err)
		}
	}()
}

// Existing returns the browser for a user only if it is running.
func (m *Manager) Existing(userID int64) *Browser {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.browsers[userID]; ok && b.alive() {
		return b
	}
	return nil
}

// Remove closes a deleted user's browser for good: one being launched is
// closed as soon as it's up, and none starts for them again, so nothing
// makes their profile folder again once it's deleted. It returns once
// their Chrome is gone.
func (m *Manager) Remove(userID int64) {
	m.mu.Lock()
	m.removed[userID] = true
	p := m.pending[userID]
	m.mu.Unlock()
	if p != nil {
		<-p.done
	}
	m.CloseUser(userID)
}

func (m *Manager) CloseUser(userID int64) {
	m.mu.Lock()
	b := m.browsers[userID]
	delete(m.browsers, userID)
	m.mu.Unlock()
	if b != nil {
		b.Close()
	}
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	bs := m.browsers
	m.browsers = map[int64]*Browser{}
	m.mu.Unlock()
	for _, b := range bs {
		b.Close()
	}
}

func (m *Manager) launch(env UserEnv) (*Browser, error) {
	if err := prepareDirs(env); err != nil {
		return nil, err
	}
	args := []string{
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=" + env.ProfileDir,
		fmt.Sprintf("--window-size=%d,%d", m.opts.Width, m.opts.Height),
		"--disable-blink-features=AutomationControlled",
		"--disable-dev-shm-usage",
		"--disable-features=Translate,MediaRouter,OptimizationHints",
		"--password-store=basic",
		"--lang=en-US",
		"--mute-audio",
		// Every task has its own tab and most are "in the background";
		// without these, input events wait on throttled frames.
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-ipc-flooding-protection",
	}
	if m.opts.Headless {
		args = append(args, "--headless", "--hide-scrollbars")
	}
	if os.Geteuid() == 0 || env.UID > 0 {
		// Inside containers Chrome's own sandbox needs privileges we don't
		// have; isolation between users comes from separate UIDs instead.
		args = append(args, "--no-sandbox")
	}
	proc, err := startChrome(chromePath(m.opts.ChromePath), append(args, "about:blank"), env)
	if err != nil {
		return nil, fmt.Errorf("launch chrome: %w", err)
	}
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), proc.url, chromedp.NoModifyURL)
	rootCtx, rootCancel := chromedp.NewContext(allocCtx)
	if err := runWithTimeout(rootCtx, 45*time.Second); err != nil {
		rootCancel()
		allocCancel()
		proc.stop()
		return nil, proc.errLaunch(err)
	}
	closeOtherPages(rootCtx)
	b := &Browser{
		mgr:         m,
		env:         env,
		chrome:      proc,
		allocCancel: allocCancel,
		rootCtx:     rootCtx,
		rootCancel:  rootCancel,
		tabs:        map[string]*Tab{},
		last:        time.Now(),
		downloads:   downloads{dir: env.DownloadDir},
	}
	if m.opts.Downloaded != nil {
		b.downloads.done = func() { m.opts.Downloaded(env.UserID) }
	}
	// Normal Chrome user agent: many sites block "HeadlessChrome".
	_ = chromedp.Run(rootCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, product, _, ua, _, err := browser.GetVersion().Do(ctx)
		if err == nil {
			b.userAgent = strings.ReplaceAll(ua, "HeadlessChrome", "Chrome")
			b.product = product
		}
		return nil
	}))
	// On the browser's own session, so its download events come to the
	// browser listener below.
	if c := chromedp.FromContext(rootCtx); c != nil && c.Browser != nil {
		_ = browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).WithDownloadPath(env.DownloadDir).WithEventsEnabled(true).Do(cdp.WithExecutor(rootCtx, c.Browser))
	}
	// Follow popups (when a page in one of our tabs opens a new window, the
	// task's tab switches to it, and back when it closes) and note
	// downloads. Popups are adopted one at a time, in the order they were
	// opened, so one opened from a popup finds its opener already in the
	// tab.
	popups := make(chan *target.Info, 16)
	go b.followPopups(popups)
	chromedp.ListenBrowser(rootCtx, func(ev any) {
		switch e := ev.(type) {
		case *target.EventTargetCreated:
			if e.TargetInfo.Type == "page" && e.TargetInfo.OpenerID != "" {
				select {
				case popups <- e.TargetInfo:
				default: // a page opening windows faster than they're followed
					go b.closeTarget(e.TargetInfo.TargetID)
				}
			}
		case *target.EventTargetDestroyed:
			go b.targetGone(e.TargetID)
		case *browser.EventDownloadWillBegin, *browser.EventDownloadProgress:
			b.downloads.event(e)
		}
	})
	// chromedp only has the browser report new targets when it starts Chrome
	// itself; over the pipe that's left to us, and without it no popup is
	// ever followed.
	if c := chromedp.FromContext(rootCtx); c != nil && c.Browser != nil {
		if err := target.SetDiscoverTargets(true).Do(cdp.WithExecutor(rootCtx, c.Browser)); err != nil {
			slog.Warn("popups won't be followed", "user", env.UserID, "err", err)
		}
	}
	// chromedp ends the root context when Chrome goes away (it crashed, or
	// was killed for memory). Closing the browser then closes its tabs and
	// their live views, which reconnect, and the next Get launches a new
	// one; without this a live view would show the last frame for good.
	go func() {
		<-rootCtx.Done()
		b.mu.Lock()
		crashed := !b.closed
		keys := make([]string, 0, len(b.tabs))
		for k := range b.tabs {
			keys = append(keys, k)
		}
		b.mu.Unlock()
		if !crashed {
			return
		}
		slog.Warn("browser exited unexpectedly", "user", env.UserID)
		for _, k := range keys {
			b.emit(k, TabEvent{Type: "crashed"})
		}
		b.Close()
	}()
	slog.Info("browser launched", "user", env.UserID, "product", b.product)
	return b, nil
}

// chromeEnv is the whole environment Chrome starts with (chromeCmd). It
// inherits nothing else from the server: the model and mail API keys are in
// the server's environment, and a page, a person in take-over, or the
// user's shell (same UID) could read Chrome's from /proc/<pid>/environ.
func chromeEnv(env UserEnv) []string {
	// Under its own UID Chrome gets the user's home; otherwise the server's,
	// as before.
	home := os.Getenv("HOME")
	if env.UID > 0 && env.HomeDir != "" {
		home = env.HomeDir
	}
	vars := []string{"HOME=" + home}
	if p := os.Getenv("PATH"); p != "" {
		vars = append(vars, "PATH="+p)
	} else {
		vars = append(vars, "PATH=/usr/local/bin:/usr/bin:/bin")
	}
	if env.UID > 0 {
		vars = append(vars, "XDG_CONFIG_HOME="+home+"/.config", "XDG_CACHE_HOME="+home+"/.cache", "TMPDIR="+home+"/tmp")
	} else if t := os.Getenv("TMPDIR"); t != "" {
		vars = append(vars, "TMPDIR="+t)
	}
	// Who and where (language, time zone), a display for a visible window,
	// and the proxy the server itself goes through.
	for _, k := range []string{"USER", "LOGNAME", "LANG", "LC_ALL", "LANGUAGE", "TZ", "DISPLAY", "XAUTHORITY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR",
		"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		if v, ok := os.LookupEnv(k); ok {
			vars = append(vars, k+"="+v)
		}
	}
	return vars
}

// prepareDirs makes the profile, download and temp folders plain folders
// given to the user, and clears the lock a crashed Chrome leaves in the
// profile (it would keep a new one from starting). The server runs as root
// and the user's own processes own everything in their folder, so this
// goes through an os.Root on that folder and never follows a link: one put
// in place of a folder, or of one above it, would otherwise have root make,
// give away or clear things wherever it led. Such a profile or download
// folder is refused (userenv.Ensure, which runs first, moves it aside); a
// temp folder that isn't plain only means Chrome's temporary files go
// where the user's own UID can write anyway, so it isn't worth failing
// over.
func prepareDirs(env UserEnv) error {
	top := env.Root
	if top == "" {
		top = filepath.Dir(env.ProfileDir)
	}
	// Its parents are the server's own.
	if err := os.MkdirAll(top, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(top)
	if err != nil {
		return err
	}
	defer root.Close()
	dirs := []string{env.ProfileDir, env.DownloadDir}
	if env.HomeDir != "" {
		dirs = append(dirs, filepath.Join(env.HomeDir, "tmp"))
	}
	for i, d := range dirs {
		rel, err := filepath.Rel(top, d)
		if err == nil && !filepath.IsLocal(rel) {
			err = fmt.Errorf("the browser's %s folder isn't in the user's folder", filepath.Base(d))
		}
		if err == nil {
			err = plainDir(root, rel, env.UID, env.GID)
		}
		if err != nil {
			if i < 2 {
				return err
			}
			slog.Warn("browser temp folder isn't a plain folder", "user", env.UserID, "path", d, "err", err)
			continue
		}
		if i == 0 {
			for _, f := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
				_ = root.Remove(filepath.Join(rel, f))
			}
		}
	}
	return nil
}

// plainDir makes rel and each folder above it inside root a plain folder,
// given to uid if it's set, refusing one that's a link or a file.
func plainDir(root *os.Root, rel string, uid, gid int) error {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := range parts {
		d := filepath.Join(parts[:i+1]...)
		if err := root.Mkdir(d, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if fi, err := root.Lstat(d); err != nil {
			return err
		} else if !fi.IsDir() {
			return fmt.Errorf("the browser's %s folder isn't a plain folder (a link or a file is in its place), so it won't be used", filepath.ToSlash(d))
		}
		if uid > 0 {
			_ = root.Lchown(d, uid, gid)
		}
	}
	return nil
}

// closeOtherPages closes the blank window Chrome opens at start: the root
// context has a page of its own.
func closeOtherPages(rootCtx context.Context) {
	c := chromedp.FromContext(rootCtx)
	if c == nil || c.Browser == nil || c.Target == nil {
		return
	}
	ctx, cancel := context.WithTimeout(rootCtx, 5*time.Second)
	defer cancel()
	ex := cdp.WithExecutor(ctx, c.Browser)
	infos, err := target.GetTargets().Do(ex)
	if err != nil {
		return
	}
	for _, ti := range infos {
		if ti.Type == "page" && ti.TargetID != c.Target.TargetID {
			_ = target.CloseTarget(ti.TargetID).Do(ex)
		}
	}
}

type Browser struct {
	mgr         *Manager
	env         UserEnv
	chrome      *chrome
	allocCancel context.CancelFunc
	rootCtx     context.Context
	rootCancel  context.CancelFunc
	userAgent   string
	product     string

	mu     sync.Mutex
	tabs   map[string]*Tab // key -> tab
	last   time.Time
	closed bool

	// opening serialises tab creation, so the agent and a live view asking
	// for the same tab at once get one tab, not two.
	opening sync.Mutex

	downloads downloads
}

func (b *Browser) touch() {
	b.mu.Lock()
	b.last = time.Now()
	b.mu.Unlock()
}

// idleFor is how long no one has used the browser: none while an action
// is running or someone is watching a tab.
func (b *Browser) idleFor() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, t := range b.tabs {
		if t.busy() || t.watched() {
			return 0
		}
	}
	return time.Since(b.last)
}

func (b *Browser) alive() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.closed && b.rootCtx.Err() == nil
}

func (b *Browser) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	tabs := b.tabs
	b.tabs = map[string]*Tab{}
	b.mu.Unlock()
	for _, t := range tabs {
		t.close()
	}
	b.rootCancel()
	b.allocCancel()
	if b.chrome != nil {
		b.chrome.stop()
	}
}

// Tab returns (creating if needed) the tab with the given key.
func (b *Browser) Tab(taskID string) (*Tab, error) {
	b.touch()
	b.opening.Lock()
	defer b.opening.Unlock()
	b.mu.Lock()
	if t, ok := b.tabs[taskID]; ok {
		if t.ctx.Err() == nil {
			b.mu.Unlock()
			return t, nil
		}
		// A tab that died is replaced; closing it ends its live views, so
		// they reconnect to the new one instead of showing the old frame.
		delete(b.tabs, taskID)
		go t.close()
	}
	b.mu.Unlock()
	ctx, cancel := chromedp.NewContext(b.rootCtx)
	if err := runWithTimeout(ctx, 30*time.Second); err != nil {
		cancel()
		return nil, fmt.Errorf("open tab: %w", err)
	}
	t, err := newTab(b, taskID, ctx, cancel)
	if err != nil {
		cancel()
		return nil, err
	}
	b.mu.Lock()
	b.tabs[taskID] = t
	b.mu.Unlock()
	if f := b.mgr.opts.RestoreURL; f != nil {
		if u := f(b.env.UserID, taskID); u != "" {
			_ = t.Navigate(u)
		}
	}
	return t, nil
}

// PeekTab returns the task's tab if it exists, without creating one.
func (b *Browser) PeekTab(taskID string) *Tab {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t, ok := b.tabs[taskID]; ok && t.ctx.Err() == nil {
		return t
	}
	return nil
}

// maxPopups is how many popups a tab follows at once. A page that opens
// window after window can't pile up renderers that way.
const maxPopups = 5

// followPopups adopts the popups the browser reports, one at a time.
func (b *Browser) followPopups(popups <-chan *target.Info) {
	for {
		select {
		case <-b.rootCtx.Done():
			return
		case ti := <-popups:
			b.adoptPopup(ti.TargetID, ti.OpenerID)
		}
	}
}

// adoptPopup makes a popup the active page of the tab whose page opened it,
// a popup of its own included. One no tab can follow (its opener is gone,
// or the tab has as many as it follows) is closed: no one could see or
// reach it, and it would run for as long as the browser does.
func (b *Browser) adoptPopup(targetID, openerID target.ID) {
	b.mu.Lock()
	var owner *Tab
	for _, t := range b.tabs {
		if t.ownsTarget(openerID) {
			owner = t
		}
	}
	b.mu.Unlock()
	if owner == nil || owner.popups() >= maxPopups {
		slog.Info("closing a popup no tab follows", "user", b.env.UserID, "opener", owner != nil)
		b.closeTarget(targetID)
		return
	}
	ctx, cancel := chromedp.NewContext(b.rootCtx, chromedp.WithTargetID(targetID))
	if err := runWithTimeout(ctx, 15*time.Second); err != nil {
		slog.Warn("attach popup failed", "err", err)
		cancel()
		return
	}
	if err := owner.switchTo(ctx, cancel); err != nil {
		slog.Warn("adopt popup failed", "err", err)
		cancel()
	}
}

// closeTarget closes a page of the browser that no tab has.
func (b *Browser) closeTarget(id target.ID) {
	c := chromedp.FromContext(b.rootCtx)
	if c == nil || c.Browser == nil {
		return
	}
	ctx, cancel := context.WithTimeout(b.rootCtx, 3*time.Second)
	defer cancel()
	_ = target.CloseTarget(id).Do(cdp.WithExecutor(ctx, c.Browser))
}

// targetGone drops a popup that closed (the page called window.close, say)
// from the tab it belongs to, which goes back to the page that opened it.
func (b *Browser) targetGone(id target.ID) {
	b.mu.Lock()
	tabs := make([]*Tab, 0, len(b.tabs))
	for _, t := range b.tabs {
		tabs = append(tabs, t)
	}
	b.mu.Unlock()
	for _, t := range tabs {
		t.dropPopup(id)
	}
}

func (b *Browser) emit(taskID string, ev TabEvent) {
	if b.mgr.onEvent != nil {
		b.mgr.onEvent(b.env.UserID, taskID, ev)
	}
}

// runWithTimeout performs the first Run on a fresh chromedp context. That Run
// must use the context itself (a derived timeout context would close the
// target when cancelled), so the timeout is enforced from outside.
func runWithTimeout(ctx context.Context, d time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- chromedp.Run(ctx) }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		return fmt.Errorf("timed out after %s", d)
	}
}
