package store

import (
	"context"
	"time"
)

// What the server keeps but nothing reads any more is deleted, so the
// database doesn't grow forever (BH-3). The feed is the user's visible
// history and stays.

// NotificationKeep is how long a notification is kept once it's read.
const NotificationKeep = 30 * 24 * time.Hour

// Pruned counts what Prune deleted.
type Pruned struct {
	Notifications int64 // read ones older than NotificationKeep
	AuthSessions  int64 // expired sign-ins
}

// Prune deletes notifications read and older than NotificationKeep, and
// expired sign-ins. Conversation items a summary covers are deleted by the
// compaction that folds them (CompactSession), with any older ones still
// there: the recall index reads those first (agent/recall.go), so they
// aren't swept from under it here.
func (s *Store) Prune(ctx context.Context) (Pruned, error) {
	var p Pruned
	res, err := s.db.ExecContext(ctx, `DELETE FROM notifications WHERE read=1 AND created_at<?`, time.Now().Add(-NotificationKeep).UnixMilli())
	if err != nil {
		return p, err
	}
	p.Notifications, _ = res.RowsAffected()
	res, err = s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at<=?`, now())
	if err != nil {
		return p, err
	}
	p.AuthSessions, _ = res.RowsAffected()
	return p, nil
}

// Mentions calls fn with each stored text of a user's that contains sub:
// what the feed shows, their reports, the conversation still replayed and
// its summary. It's how files kept beside the database (the pictures
// blocks show) are known to be still in use. sub is matched exactly.
func (s *Store) Mentions(ctx context.Context, sub string, fn func(userID int64, text string)) error {
	for _, q := range []string{
		`SELECT user_id, text || ' ' || data FROM feed WHERE instr(text || ' ' || data, ?1) > 0`,
		`SELECT user_id, content || ' ' || data FROM reports WHERE instr(content || ' ' || data, ?1) > 0`,
		`SELECT user_id, json FROM session_items WHERE instr(json, ?1) > 0`,
		`SELECT user_id, summary FROM session_state WHERE instr(summary, ?1) > 0`,
	} {
		rows, err := s.db.QueryContext(ctx, q, sub)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var text string
			if err := rows.Scan(&id, &text); err != nil {
				rows.Close()
				return err
			}
			fn(id, text)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}
