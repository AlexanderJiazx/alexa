package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// What the server keeps for a user but nothing reads any more is deleted
// (BH-3): conversation items a summary covers (store.CompactSession, as it
// folds them), screenshots no remaining item shows, read
// notifications after a month, expired sign-ins, and the image tokens of
// deleted users or that nothing shows any more. The feed is the user's
// visible history and stays. It happens after a compaction, at a reset,
// and in a sweep the scheduler runs at start and every sweepEvery.

const (
	// sweepEvery is how often the scheduler sweeps.
	sweepEvery = 24 * time.Hour
	// shotGrace keeps a screenshot saved this recently: the item that
	// shows it may not be written yet (saveShot comes first).
	shotGrace = 10 * time.Minute
	// imageTokenGrace keeps an image token minted (or shown again) this
	// recently: the entry or report that shows it may not be written yet.
	imageTokenGrace = 7 * 24 * time.Hour
)

// Sweep deletes what nothing needs any more, for every user.
func (rt *Runtime) Sweep(ctx context.Context) {
	t0 := time.Now()
	p, err := rt.st.Prune(ctx)
	if err != nil {
		slog.Warn("sweep: prune the database", "err", err)
	}
	// Read before the list: a user made after it has a higher id.
	last, err := rt.st.LastUserID(ctx)
	if err != nil {
		slog.Warn("sweep: last user id", "err", err)
		return
	}
	users, err := rt.st.ListUsers(ctx)
	if err != nil {
		slog.Warn("sweep: list users", "err", err)
		return
	}
	shots := 0
	live := map[int64]bool{}
	for _, u := range users {
		live[u.ID] = true
		shots += rt.pruneShots(ctx, u.ID, shotGrace)
	}
	tokens, err := rt.pruneImageTokens(ctx, live, last)
	if err != nil {
		slog.Warn("sweep: image tokens", "err", err)
	}
	slog.Info("swept storage", "notifications", p.Notifications, "auth_sessions", p.AuthSessions,
		"shots", shots, "image_tokens", tokens, "took", time.Since(t0).Round(time.Millisecond))
}

// pruneShots deletes the user's screenshots that no item of the
// conversation shows any more (they went with the items folded into the
// summary, or with a reset), except those saved within grace. It returns
// how many it deleted.
func (rt *Runtime) pruneShots(ctx context.Context, userID int64, grace time.Duration) int {
	dir := rt.envs.For(userID).Shots
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	// Read after the listing: a screenshot listed is either old enough to
	// be left out of an item written since, or within grace.
	_, rows, err := rt.st.Conversation(ctx, userID)
	if err != nil {
		slog.Warn("prune screenshots", "user", userID, "err", err)
		return 0
	}
	shown := map[string]bool{}
	for _, r := range rows {
		var si storedItem
		if json.Unmarshal(r.JSON, &si) == nil && si.Shot != "" {
			shown[si.Shot] = true
		}
	}
	n := 0
	for _, e := range entries {
		if shown[e.Name()] || !e.Type().IsRegular() {
			continue
		}
		if fi, err := e.Info(); err != nil || time.Since(fi.ModTime()) < grace {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			n++
		}
	}
	return n
}

var imageRefRe = regexp.MustCompile(`/api/img/([0-9a-f]{32})`)

// pruneImageTokens deletes the image tokens (mintImage) of users that are
// gone (not live, with an id up to last, the highest given when live was
// read), and those nothing shows any more (no feed entry, report, item of
// the conversation or its summary) that weren't minted or shown again
// within imageTokenGrace. It returns how many it deleted.
func (rt *Runtime) pruneImageTokens(ctx context.Context, live map[int64]bool, last int64) (int, error) {
	dir := filepath.Dir(rt.imageTokenPath(strings.Repeat("0", 32)))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	shown := map[string]bool{}
	if err := rt.st.Mentions(ctx, "/api/img/", func(_ int64, text string) {
		for _, m := range imageRefRe.FindAllStringSubmatch(text, -1) {
			shown[m[1]] = true
		}
	}); err != nil {
		return 0, err // without the full picture, nothing is deleted
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		old := time.Since(fi.ModTime()) > imageTokenGrace
		token, ok := strings.CutSuffix(name, ".json")
		del := false
		switch {
		case !ok || !imageTokenRe.MatchString(token):
			del = old // a write that never finished, or a stranger
		default:
			var it imageToken
			b, err := os.ReadFile(filepath.Join(dir, name))
			switch {
			case err != nil:
				continue
			case json.Unmarshal(b, &it) != nil:
				del = old
			case !live[it.User] && it.User <= last: // a user made since isn't one that's gone
				del = true
			default:
				del = old && !shown[token]
			}
		}
		if del && os.Remove(filepath.Join(dir, name)) == nil {
			n++
		}
	}
	return n, nil
}
