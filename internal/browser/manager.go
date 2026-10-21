// Package browser runs one Chrome instance per user (own profile directory,
// optionally own UID) with one tab per task. Tabs support agent actions
// (DOM-indexed and coordinate-based), live JPEG screencasting, and human
// takeover via forwarded input events.
package browser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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
}

// UserEnv describes the on-disk environment of a user.
type UserEnv struct {
	UserID      int64
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
	envFn    func(userID int64) UserEnv
	onEvent  func(userID int64, taskID string, ev TabEvent)
}

// TabEvent is emitted for navigation/cursor changes so the UI can follow along.
type TabEvent struct {
	Type  string  `json:"type"` // nav | cursor | closed
	URL   string  `json:"url,omitempty"`
	Title string  `json:"title,omitempty"`
	X     float64 `json:"x,omitempty"`
	Y     float64 `json:"y,omitempty"`
	Click bool    `json:"click,omitempty"`
}

func NewManager(opts Options, envFn func(int64) UserEnv, onEvent func(int64, string, TabEvent)) *Manager {
	m := &Manager{opts: opts, browsers: map[int64]*Browser{}, pending: map[int64]*pendingLaunch{}, envFn: envFn, onEvent: onEvent}
	go m.reaper()
	return m
}

func (m *Manager) reaper() {
	for range time.Tick(time.Minute) {
		m.mu.Lock()
		for id, b := range m.browsers {
			if b.idleFor() > m.opts.IdleAfter {
				slog.Info("closing idle browser", "user", id)
				delete(m.browsers, id)
				go b.Close()
			}
		}
		m.mu.Unlock()
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
	if b, ok := m.browsers[userID]; ok && b.alive() {
		b.touch()
		m.mu.Unlock()
		return b, nil
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
	if p.err == nil {
		m.browsers[userID] = p.b
	}
	m.mu.Unlock()
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
	dirs := []string{env.ProfileDir, env.DownloadDir}
	if env.HomeDir != "" {
		dirs = append(dirs, filepath.Join(env.HomeDir, "tmp"))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
		if env.UID > 0 {
			_ = os.Chown(d, env.UID, env.GID)
		}
	}
	// A crashed Chrome leaves a lock that prevents relaunch with the same profile.
	for _, f := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		_ = os.Remove(filepath.Join(env.ProfileDir, f))
	}
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.UserDataDir(env.ProfileDir),
		chromedp.WindowSize(m.opts.Width, m.opts.Height),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-background-networking", false),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-features", "Translate,MediaRouter,OptimizationHints"),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("lang", "en-US"),
		chromedp.Flag("hide-scrollbars", false),
		chromedp.Flag("mute-audio", true),
		// Every task has its own tab and most are "in the background";
		// without these, input events wait on throttled frames.
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-ipc-flooding-protection", true),
	}
	if m.opts.Headless {
		opts = append(opts, chromedp.Headless)
	}
	if m.opts.ChromePath != "" {
		opts = append(opts, chromedp.ExecPath(m.opts.ChromePath))
	}
	if os.Geteuid() == 0 || env.UID > 0 {
		// Inside containers Chrome's own sandbox needs privileges we don't
		// have; isolation between users comes from separate UIDs instead.
		opts = append(opts, chromedp.NoSandbox)
	}
	if env.UID > 0 {
		uid, gid, home := env.UID, env.GID, env.HomeDir
		opts = append(opts, chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {
			if cmd.SysProcAttr == nil {
				cmd.SysProcAttr = &syscall.SysProcAttr{}
			}
			cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
			cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home+"/.config", "XDG_CACHE_HOME="+home+"/.cache", "TMPDIR="+home+"/tmp")
		}))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	rootCtx, rootCancel := chromedp.NewContext(allocCtx)
	if err := runWithTimeout(rootCtx, 45*time.Second); err != nil {
		rootCancel()
		allocCancel()
		return nil, fmt.Errorf("launch chrome: %w", err)
	}
	b := &Browser{
		mgr:         m,
		env:         env,
		allocCancel: allocCancel,
		rootCtx:     rootCtx,
		rootCancel:  rootCancel,
		tabs:        map[string]*Tab{},
		last:        time.Now(),
		downloads:   downloads{dir: env.DownloadDir},
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
	// downloads.
	chromedp.ListenBrowser(rootCtx, func(ev any) {
		switch e := ev.(type) {
		case *target.EventTargetCreated:
			if e.TargetInfo.Type == "page" && e.TargetInfo.OpenerID != "" {
				go b.adoptPopup(e.TargetInfo.TargetID, e.TargetInfo.OpenerID)
			}
		case *target.EventTargetDestroyed:
			go b.targetGone(e.TargetID)
		case *browser.EventDownloadWillBegin, *browser.EventDownloadProgress:
			b.downloads.event(e)
		}
	})
	slog.Info("browser launched", "user", env.UserID, "product", b.product)
	return b, nil
}

type Browser struct {
	mgr         *Manager
	env         UserEnv
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

func (b *Browser) idleFor() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, t := range b.tabs {
		if t.busy() {
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
}

// Tab returns (creating if needed) the tab with the given key.
func (b *Browser) Tab(taskID string) (*Tab, error) {
	b.touch()
	b.opening.Lock()
	defer b.opening.Unlock()
	b.mu.Lock()
	if t, ok := b.tabs[taskID]; ok && t.ctx.Err() == nil {
		b.mu.Unlock()
		return t, nil
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

func (b *Browser) CloseTab(taskID string) {
	b.mu.Lock()
	t := b.tabs[taskID]
	delete(b.tabs, taskID)
	b.mu.Unlock()
	if t != nil {
		t.close()
	}
}

func (b *Browser) adoptPopup(targetID, openerID target.ID) {
	b.mu.Lock()
	var owner *Tab
	for _, t := range b.tabs {
		if t.targetID() == openerID {
			owner = t
		}
	}
	b.mu.Unlock()
	if owner == nil {
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
