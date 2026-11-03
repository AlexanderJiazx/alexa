// Package store persists users, their sessions, memories, reports and schedules in SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  timezone TEXT NOT NULL DEFAULT 'UTC',
  about TEXT NOT NULL DEFAULT '',
  is_admin INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS auth_sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS memories (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS schedules (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  cron TEXT NOT NULL DEFAULT '',
  run_at INTEGER NOT NULL DEFAULT 0,
  prompt TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  last_run INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  read INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS push_subs (
  endpoint TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  json TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS kv (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() int64 { return time.Now().UnixMilli() }

// ---------- users ----------

type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Timezone    string `json:"timezone"`
	About       string `json:"about"`
	IsAdmin     bool   `json:"isAdmin"`
	CreatedAt   int64  `json:"createdAt"`
	hash        string
}

func (u *User) PasswordHash() string { return u.hash }

const userCols = `id, username, password_hash, display_name, email, timezone, about, is_admin, created_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var admin int
	if err := row.Scan(&u.ID, &u.Username, &u.hash, &u.DisplayName, &u.Email, &u.Timezone, &u.About, &admin, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.IsAdmin = admin == 1
	return &u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, hash, display string, admin bool) (*User, error) {
	a := 0
	if admin {
		a = 1
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO users(username, password_hash, display_name, is_admin, created_at) VALUES(?,?,?,?,?)`, username, hash, display, a, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetUser(ctx, id)
}

// ErrSignupClosed is returned by SignUp when there are users already and
// anyone else may not sign up.
var ErrSignupClosed = errors.New("sign-up is closed")

// SignUp creates an account from the sign-up page. The first one is the
// admin; after it, one is made only if open. The count and the insert are
// one transaction, holding the write lock throughout, so two people
// signing up at once to a new server can't both become admin, and neither
// gets in once the first has closed sign-up behind them.
func (s *Store) SignUp(ctx context.Context, username, hash, display string, open bool) (*User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 && !open {
		return nil, ErrSignupClosed
	}
	admin := 0
	if n == 0 {
		admin = 1
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO users(username, password_hash, display_name, is_admin, created_at) VALUES(?,?,?,?,?)`, username, hash, display, admin, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

// LastUserID is the highest id ever given a user, deleted or not: ids
// aren't reused (AUTOINCREMENT), so a user made after this is read has a
// higher one.
func (s *Store) LastUserID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name='users'`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func (s *Store) GetUserByName(ctx context.Context, name string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username=?`, name))
}

func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUserProfile(ctx context.Context, id int64, display, email, tz, about string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET display_name=?, email=?, timezone=?, about=? WHERE id=?`, display, email, tz, about, id)
	return err
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, hash, id)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	// Per-user kv entries are keyed "<what>:<user id>" (the agent's plan,
	// prune marks and resume flags) and have no foreign key to cascade.
	_, err = s.db.ExecContext(ctx, `DELETE FROM kv WHERE key LIKE '%:' || ?`, fmt.Sprint(id))
	return err
}

// ---------- sessions ----------

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_sessions(token_hash, user_id, created_at, expires_at) VALUES(?,?,?,?)`, tokenHash, userID, now(), time.Now().Add(ttl).UnixMilli())
	return err
}

func (s *Store) SessionUser(ctx context.Context, tokenHash string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT u.id, u.username, u.password_hash, u.display_name, u.email, u.timezone, u.about, u.is_admin, u.created_at
		FROM auth_sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash=? AND s.expires_at > ?`, tokenHash, now()))
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash=?`, tokenHash)
	return err
}

// DeleteOtherSessions signs a user out everywhere except the session whose
// token hash is keep, in one statement, so that one is never missing.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, keep string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id=? AND token_hash<>?`, userID, keep)
	return err
}

// ---------- schedules ----------

type Schedule struct {
	ID        string `json:"id"`
	UserID    int64  `json:"userId"`
	Name      string `json:"name"`
	Cron      string `json:"cron,omitempty"`
	RunAt     int64  `json:"runAt,omitempty"`
	Prompt    string `json:"prompt"`
	Enabled   bool   `json:"enabled"`
	LastRun   int64  `json:"lastRun,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	NextRun   int64  `json:"nextRun,omitempty"`
}

const schedCols = `id, user_id, name, cron, run_at, prompt, enabled, last_run, created_at`

func scanSched(row interface{ Scan(...any) error }) (*Schedule, error) {
	var sc Schedule
	var en int
	if err := row.Scan(&sc.ID, &sc.UserID, &sc.Name, &sc.Cron, &sc.RunAt, &sc.Prompt, &en, &sc.LastRun, &sc.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sc.Enabled = en == 1
	return &sc, nil
}

func (s *Store) CreateSchedule(ctx context.Context, sc *Schedule) error {
	sc.CreatedAt = now()
	en := 0
	if sc.Enabled {
		en = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO schedules(id, user_id, name, cron, run_at, prompt, enabled, created_at) VALUES(?,?,?,?,?,?,?,?)`,
		sc.ID, sc.UserID, sc.Name, sc.Cron, sc.RunAt, sc.Prompt, en, sc.CreatedAt)
	return err
}

func (s *Store) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	return scanSched(s.db.QueryRowContext(ctx, `SELECT `+schedCols+` FROM schedules WHERE id=?`, id))
}

func (s *Store) ListSchedules(ctx context.Context, userID int64) ([]*Schedule, error) {
	q := `SELECT ` + schedCols + ` FROM schedules`
	args := []any{}
	if userID > 0 {
		q += ` WHERE user_id=?`
		args = append(args, userID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		sc, err := scanSched(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Store) SetScheduleEnabled(ctx context.Context, userID int64, id string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE schedules SET enabled=? WHERE id=? AND user_id=?`, en, id, userID)
	return err
}

func (s *Store) MarkScheduleRun(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE schedules SET last_run=? WHERE id=?`, now(), id)
	return err
}

func (s *Store) DeleteSchedule(ctx context.Context, userID int64, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---------- notifications ----------

type Notification struct {
	ID        int64  `json:"id"`
	Ref       string `json:"ref,omitempty"` // what it is about, e.g. "report:<id>"
	Title     string `json:"title"`
	Body      string `json:"body"`
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *Store) AddNotification(ctx context.Context, userID int64, ref, title, body string) (*Notification, error) {
	n := &Notification{Ref: ref, Title: title, Body: body, CreatedAt: now()}
	res, err := s.db.ExecContext(ctx, `INSERT INTO notifications(user_id, task_id, title, body, created_at) VALUES(?,?,?,?,?)`, userID, ref, title, body, n.CreatedAt)
	if err != nil {
		return nil, err
	}
	n.ID, _ = res.LastInsertId()
	return n, nil
}

func (s *Store) Notifications(ctx context.Context, userID int64, limit int) ([]*Notification, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, title, body, read, created_at FROM notifications WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Notification
	for rows.Next() {
		var n Notification
		var r int
		if err := rows.Scan(&n.ID, &n.Ref, &n.Title, &n.Body, &r, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.Read = r == 1
		out = append(out, &n)
	}
	return out, rows.Err()
}

func (s *Store) MarkNotificationsRead(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET read=1 WHERE user_id=?`, userID)
	return err
}

// MarkNotificationRead marks read the unread notifications about ref, such
// as the question the user has since answered. It says whether there were
// any.
func (s *Store) MarkNotificationRead(ctx context.Context, userID int64, ref string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE notifications SET read=1 WHERE user_id=? AND task_id=? AND read=0`, userID, ref)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---------- push subscriptions ----------

func (s *Store) SavePushSub(ctx context.Context, userID int64, endpoint, js string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO push_subs(endpoint, user_id, json, created_at) VALUES(?,?,?,?)
		ON CONFLICT(endpoint) DO UPDATE SET user_id=excluded.user_id, json=excluded.json`, endpoint, userID, js, now())
	return err
}

func (s *Store) PushSubs(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT json FROM push_subs WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) DeletePushSub(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subs WHERE endpoint=?`, endpoint)
	return err
}

// ---------- kv ----------

func (s *Store) GetKV(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetKV(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO kv(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
