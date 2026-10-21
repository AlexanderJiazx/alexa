// Command alexagent runs Alexa, an always-on personal agent web service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"alexagent/internal/agent"
	"alexagent/internal/api"
	"alexagent/internal/browser"
	"alexagent/internal/config"
	"alexagent/internal/hub"
	"alexagent/internal/llm"
	"alexagent/internal/notify"
	"alexagent/internal/store"
	"alexagent/internal/userenv"
	"alexagent/internal/vault"
	"alexagent/internal/voice"
	"alexagent/web"
)

func main() {
	debug := flag.Bool("debug", os.Getenv("DEBUG") != "", "verbose logging")
	createUser := flag.String("create-user", "", "create a user (username:password[:admin]) and exit")
	flag.Parse()
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	cfg := config.Load()
	// Everything the server writes is private to it; per-user processes
	// (Chrome, the shell tool) run under their own UIDs and must not be able
	// to read the database or other users' data.
	syscall.Umask(0o077)
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		fatal("data dir", err)
	}
	if err := adoptOldFiles(cfg.DataDir); err != nil {
		fatal("rename old data files", err)
	}
	if cfg.SandboxUIDs {
		lockDown(cfg.DataDir)
	}
	// Keep a copy of the log next to the data so it survives the terminal.
	if *createUser == "" {
		if f, err := openLog(filepath.Join(cfg.DataDir, "alexa.log")); err == nil {
			defer f.Close()
			slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, f), &slog.HandlerOptions{Level: level})))
		} else {
			slog.Warn("can't open log file", "err", err)
		}
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "alexa.db"))
	if err != nil {
		fatal("open database", err)
	}
	defer st.Close()
	envs := userenv.New(cfg.DataDir, cfg.SandboxUIDs, cfg.UIDBase)

	if *createUser != "" {
		if err := cliCreateUser(st, envs, *createUser); err != nil {
			fatal("create user", err)
		}
		return
	}
	if cfg.APIKey == "" {
		fatal("config", errors.New("META_MODEL_API_KEY is not set"))
	}
	logins, err := vault.Open(cfg.VaultKeyFile, st)
	if err != nil {
		fatal("open the login vault", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	h := hub.New()
	notifier, err := notify.New(ctx, st, h, cfg.ResendAPIKey, cfg.MailFrom, cfg.PublicURL)
	if err != nil {
		fatal("notifier", err)
	}
	client := llm.New(cfg.BaseURL, cfg.APIKey)
	go client.KeepWarm(ctx, 4*time.Minute)

	var rt *agent.Runtime
	br := browser.NewManager(browser.Options{
		ChromePath: cfg.ChromePath,
		Headless:   cfg.BrowserHeadless,
		Width:      cfg.ViewportWidth,
		Height:     cfg.ViewportHeight,
		IdleAfter:  time.Duration(cfg.BrowserIdleMins) * time.Minute,
		RestoreURL: func(userID int64, _ string) string {
			if rt == nil {
				return ""
			}
			return rt.LastURL(userID)
		},
	}, func(userID int64) browser.UserEnv {
		e := envs.For(userID)
		return browser.UserEnv{UserID: userID, ProfileDir: e.Profile, DownloadDir: e.Downloads, HomeDir: e.Home, UID: e.UID, GID: e.GID}
	}, func(userID int64, taskID string, ev browser.TabEvent) {
		if rt != nil {
			rt.OnBrowserEvent(userID, taskID, ev)
		}
	})
	defer br.CloseAll()

	rt = agent.New(ctx, cfg, st, client, br, envs, h, notifier, logins)
	rt.Scheduler().Start(ctx)
	rt.ResumeInterrupted(ctx)

	static, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		fatal("static", err)
	}
	vs := &voice.Service{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.ASRModel}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(cfg, st, rt, h, envs, notifier, vs, br, logins, static).Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}
	go func() {
		slog.Info("Alexa is running", "addr", cfg.Addr, "model", cfg.Model, "data", cfg.DataDir, "sandboxUIDs", cfg.SandboxUIDs)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("http", err)
		}
	}()
	<-ctx.Done()
	slog.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	rt.Shutdown()
}

func cliCreateUser(st *store.Store, envs *userenv.Envs, spec string) error {
	var user, pass, role string
	parts := splitN(spec, ':', 3)
	if len(parts) < 2 {
		return errors.New("use username:password[:admin]")
	}
	user, pass = parts[0], parts[1]
	if len(parts) == 3 {
		role = parts[2]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	ctx := context.Background()
	n, _ := st.CountUsers(ctx)
	u, err := st.CreateUser(ctx, user, string(hash), user, role == "admin" || n == 0)
	if err != nil {
		return err
	}
	if _, err := envs.Ensure(u.ID); err != nil {
		return err
	}
	fmt.Printf("created user %s (id %d, admin=%v)\n", u.Username, u.ID, u.IsAdmin)
	return nil
}

func splitN(s string, sep byte, n int) []string {
	var out []string
	for len(out) < n-1 {
		i := -1
		for j := 0; j < len(s); j++ {
			if s[j] == sep {
				i = j
				break
			}
		}
		if i < 0 {
			break
		}
		out = append(out, s[:i])
		s = s[i+1:]
	}
	return append(out, s)
}

func fatal(what string, err error) {
	slog.Error("fatal: "+what, "err", err)
	os.Exit(1)
}

// lockDown makes the data dir traversable (so sandboxed users can reach
// their own directories) and every file directly in it private to root.
func lockDown(dir string) {
	_ = os.Chmod(dir, 0o711)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if e.Name() != "users" {
				_ = os.Chmod(p, 0o700)
			}
			continue
		}
		_ = os.Chmod(p, 0o600)
	}
}

// adoptOldFiles renames the database and log left by the app's former name,
// Paper, so an existing data directory keeps working.
func adoptOldFiles(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "alexa.db")); err == nil {
		return nil
	}
	// The WAL moves before the database: a database without its WAL would
	// lose the latest writes.
	for _, suffix := range []string{".db-wal", ".db-shm", ".db", ".log", ".log.1"} {
		from, to := filepath.Join(dir, "paper"+suffix), filepath.Join(dir, "alexa"+suffix)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		slog.Info("renamed data file", "from", from, "to", to)
	}
	return nil
}

// openLog opens the log file for appending, first moving it to .1 if it has
// grown past 10 MB so it can't fill a small disk.
func openLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > 10<<20 {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
