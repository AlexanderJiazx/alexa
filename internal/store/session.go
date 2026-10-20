package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Each user has exactly one session with Alexa that never ends: the model
// conversation (session_items, older parts folded into a summary), messages
// waiting for the agent (inbox), what the user sees (feed) and the reports
// Alexa wrote.
const sessionSchema = `
CREATE TABLE IF NOT EXISTS session_state (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'idle',
  activity TEXT NOT NULL DEFAULT '',
  needs_json TEXT NOT NULL DEFAULT '',
  surface_json TEXT NOT NULL DEFAULT '',
  browser_url TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  compacted_through INTEGER NOT NULL DEFAULT 0,
  compactions INTEGER NOT NULL DEFAULT 0,
  context_tokens INTEGER NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS session_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS session_items_user ON session_items(user_id, id);
CREATE TABLE IF NOT EXISTS inbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL DEFAULT 'user',
  text TEXT NOT NULL,
  attachments TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS feed (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  done_text TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  data TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS feed_user ON feed(user_id, id);
CREATE TABLE IF NOT EXISTS reports (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  format TEXT NOT NULL DEFAULT 'markdown',
  content TEXT NOT NULL DEFAULT '',
  data TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS reports_user ON reports(user_id, updated_at DESC);
`

// ---------- session state ----------

type Session struct {
	UserID           int64
	Status           string // idle | working | needs
	Activity         string // what the agent is doing right now
	Needs            json.RawMessage
	Surface          json.RawMessage // what the user's screen shows
	BrowserURL       string
	Summary          string // the compacted earlier conversation
	CompactedThrough int64  // last session_items id covered by Summary
	Compactions      int64
	ContextTokens    int64 // prompt size of the latest model call
	InputTokens      int64
	OutputTokens     int64
	UpdatedAt        int64
}

func (s *Store) ensureSession(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO session_state(user_id, updated_at) VALUES(?,?) ON CONFLICT(user_id) DO NOTHING`, userID, now())
	return err
}

func (s *Store) setSession(ctx context.Context, userID int64, set string, args ...any) error {
	if err := s.ensureSession(ctx, userID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE session_state SET `+set+`, updated_at=? WHERE user_id=?`, append(args, now(), userID)...)
	return err
}

func (s *Store) GetSession(ctx context.Context, userID int64) (*Session, error) {
	if err := s.ensureSession(ctx, userID); err != nil {
		return nil, err
	}
	var ss Session
	var needs, surface string
	err := s.db.QueryRowContext(ctx, `SELECT user_id, status, activity, needs_json, surface_json, browser_url, summary, compacted_through,
		compactions, context_tokens, input_tokens, output_tokens, updated_at FROM session_state WHERE user_id=?`, userID).
		Scan(&ss.UserID, &ss.Status, &ss.Activity, &needs, &surface, &ss.BrowserURL, &ss.Summary, &ss.CompactedThrough,
			&ss.Compactions, &ss.ContextTokens, &ss.InputTokens, &ss.OutputTokens, &ss.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if needs != "" {
		ss.Needs = json.RawMessage(needs)
	}
	if surface != "" {
		ss.Surface = json.RawMessage(surface)
	}
	return &ss, nil
}

func (s *Store) SetSessionStatus(ctx context.Context, userID int64, status, activity string) error {
	return s.setSession(ctx, userID, `status=?, activity=?`, status, activity)
}

func (s *Store) SetSessionActivity(ctx context.Context, userID int64, activity string) error {
	return s.setSession(ctx, userID, `activity=?`, activity)
}

func (s *Store) SetSessionNeeds(ctx context.Context, userID int64, needs json.RawMessage) error {
	return s.setSession(ctx, userID, `needs_json=?`, string(needs))
}

func (s *Store) SetSessionSurface(ctx context.Context, userID int64, surface json.RawMessage) error {
	return s.setSession(ctx, userID, `surface_json=?`, string(surface))
}

func (s *Store) SetSessionBrowserURL(ctx context.Context, userID int64, url string) error {
	return s.setSession(ctx, userID, `browser_url=?`, url)
}

// AddSessionUsage records a model call: its prompt size becomes the current
// context size.
func (s *Store) AddSessionUsage(ctx context.Context, userID, in, out int64) error {
	return s.setSession(ctx, userID, `input_tokens=input_tokens+?, output_tokens=output_tokens+?, context_tokens=?`, in, out, in)
}

// CompactSession replaces the conversation up to item id through with summary.
func (s *Store) CompactSession(ctx context.Context, userID int64, summary string, through int64) error {
	return s.setSession(ctx, userID, `summary=?, compacted_through=?, compactions=compactions+1, context_tokens=0`, summary, through)
}

// SessionsWithStatus lists the users whose session is in the given state.
func (s *Store) SessionsWithStatus(ctx context.Context, status string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM session_state WHERE status=?`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ResetSession forgets the conversation (not memories, files, reports or
// schedules).
func (s *Store) ResetSession(ctx context.Context, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM session_items WHERE user_id=?`,
		`DELETE FROM inbox WHERE user_id=?`,
		`DELETE FROM feed WHERE user_id=?`,
		`DELETE FROM session_state WHERE user_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- conversation items (model replay) ----------

type ItemRow struct {
	ID   int64
	JSON json.RawMessage
}

func (s *Store) AppendItems(ctx context.Context, userID int64, items []json.RawMessage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, it := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_items(user_id, json) VALUES(?,?)`, userID, string(it)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Items returns the conversation items after the given id, oldest first.
func (s *Store) Items(ctx context.Context, userID, afterID int64) ([]ItemRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, json FROM session_items WHERE user_id=? AND id>? ORDER BY id`, userID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemRow
	for rows.Next() {
		var r ItemRow
		var j string
		if err := rows.Scan(&r.ID, &j); err != nil {
			return nil, err
		}
		r.JSON = json.RawMessage(j)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- inbox (messages that arrive while the agent works) ----------

type InboxMessage struct {
	Kind        string // user | schedule | system
	Text        string
	Attachments json.RawMessage
}

func (s *Store) AddInbox(ctx context.Context, userID int64, m InboxMessage) error {
	if len(m.Attachments) == 0 {
		m.Attachments = json.RawMessage("[]")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO inbox(user_id, kind, text, attachments, created_at) VALUES(?,?,?,?,?)`, userID, m.Kind, m.Text, string(m.Attachments), now())
	return err
}

func (s *Store) InboxCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbox WHERE user_id=?`, userID).Scan(&n)
	return n, err
}

// TakeInbox returns and removes the user's waiting messages.
func (s *Store) TakeInbox(ctx context.Context, userID int64) ([]InboxMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT kind, text, attachments FROM inbox WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	var out []InboxMessage
	for rows.Next() {
		var m InboxMessage
		var a string
		if err := rows.Scan(&m.Kind, &m.Text, &a); err != nil {
			rows.Close()
			return nil, err
		}
		m.Attachments = json.RawMessage(a)
		out = append(out, m)
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM inbox WHERE user_id=?`, userID); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// ---------- feed (what the user sees) ----------

// Entry is one line of the session as the user sees it.
type Entry struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"-"`
	Kind      string          `json:"kind"` // user | say | ask | report | step | error | note
	Text      string          `json:"text"`
	DoneText  string          `json:"doneText,omitempty"`
	Status    string          `json:"status,omitempty"` // step: running|done|failed; ask: open|answered
	Data      json.RawMessage `json:"data,omitempty"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

func (s *Store) AddEntry(ctx context.Context, e *Entry) error {
	e.CreatedAt, e.UpdatedAt = now(), now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO feed(user_id, kind, text, done_text, status, data, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		e.UserID, e.Kind, e.Text, e.DoneText, e.Status, string(e.Data), e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateEntry(ctx context.Context, e *Entry) error {
	e.UpdatedAt = now()
	_, err := s.db.ExecContext(ctx, `UPDATE feed SET text=?, done_text=?, status=?, data=?, updated_at=? WHERE id=? AND user_id=?`,
		e.Text, e.DoneText, e.Status, string(e.Data), e.UpdatedAt, e.ID, e.UserID)
	return err
}

// Feed returns up to limit entries before the given id (0 = the latest),
// oldest first.
func (s *Store) Feed(ctx context.Context, userID, beforeID int64, limit int) ([]*Entry, error) {
	if beforeID <= 0 {
		beforeID = 1 << 62
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, kind, text, done_text, status, data, created_at, updated_at
		FROM feed WHERE user_id=? AND id<? ORDER BY id DESC LIMIT ?`, userID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		var e Entry
		var data string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Kind, &e.Text, &e.DoneText, &e.Status, &data, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if data != "" {
			e.Data = json.RawMessage(data)
		}
		out = append(out, &e)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

func (s *Store) SetEntryStatus(ctx context.Context, userID, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE feed SET status=?, updated_at=? WHERE id=? AND user_id=?`, status, now(), id, userID)
	return err
}

// SetEntriesStatus moves every entry of a kind from one status to another,
// e.g. running steps to failed after a crash.
func (s *Store) SetEntriesStatus(ctx context.Context, userID int64, kind, from, to string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE feed SET status=?, updated_at=? WHERE user_id=? AND kind=? AND status=?`, to, now(), userID, kind, from)
	return err
}

// ---------- reports ----------

// Report is a document Alexa wrote for the user (shown as an artifact).
type Report struct {
	ID        string          `json:"id"`
	UserID    int64           `json:"-"`
	Title     string          `json:"title"`
	Format    string          `json:"format"` // markdown | html
	Content   string          `json:"content,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"` // items, events, files, sources
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

// SaveReport creates a report or replaces one the same user owns.
func (s *Store) SaveReport(ctx context.Context, r *Report) error {
	r.UpdatedAt = now()
	if r.CreatedAt == 0 {
		r.CreatedAt = r.UpdatedAt
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO reports(id, user_id, title, format, content, data, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, format=excluded.format, content=excluded.content, data=excluded.data, updated_at=excluded.updated_at
		WHERE reports.user_id=excluded.user_id`,
		r.ID, r.UserID, r.Title, r.Format, r.Content, string(r.Data), r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetReport(ctx context.Context, userID int64, id string) (*Report, error) {
	var r Report
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, title, format, content, data, created_at, updated_at FROM reports WHERE id=? AND user_id=?`, id, userID).
		Scan(&r.ID, &r.UserID, &r.Title, &r.Format, &r.Content, &data, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if data != "" {
		r.Data = json.RawMessage(data)
	}
	return &r, nil
}

// ListReports returns the user's reports, newest first, without content.
func (s *Store) ListReports(ctx context.Context, userID int64, limit int) ([]*Report, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, title, format, created_at, updated_at FROM reports WHERE user_id=? ORDER BY updated_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Report
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.UserID, &r.Title, &r.Format, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteReport(ctx context.Context, userID int64, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM reports WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
