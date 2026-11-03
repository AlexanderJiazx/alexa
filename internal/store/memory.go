package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ---------- memories ----------

// Memory is one entry of Alexa's long-term memory of a user.
type Memory struct {
	ID        int64  `json:"id"`
	Content   string `json:"content"`
	Source    string `json:"source"` // user | alexa | review: who wrote it
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// NewMemory is an entry to add with EditMemories.
type NewMemory struct {
	Content string
	Source  string
}

// keepDeleted is how long a deleted entry can still be restored. After
// that the next delete removes it for good.
const keepDeleted = 30 * 24 * time.Hour

const memoryCols = `id, content, source, created_at, updated_at`

func scanMemories(rows *sql.Rows) ([]*Memory, error) {
	defer rows.Close()
	var out []*Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Content, &m.Source, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// AddMemory adds an entry the user wrote.
func (s *Store) AddMemory(ctx context.Context, userID int64, content string) (*Memory, error) {
	m := &Memory{Content: content, Source: "user", CreatedAt: now()}
	m.UpdatedAt = m.CreatedAt
	res, err := s.db.ExecContext(ctx, `INSERT INTO memories(user_id, content, source, created_at, updated_at) VALUES(?,?,?,?,?)`,
		userID, content, m.Source, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	m.ID, _ = res.LastInsertId()
	return m, nil
}

// Memories returns the user's live entries (not deleted), oldest first.
func (s *Store) Memories(ctx context.Context, userID int64) ([]*Memory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+memoryCols+` FROM memories WHERE user_id=? AND deleted_at=0 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return scanMemories(rows)
}

// UpdateMemory rewrites an entry. The user edited it, so it's theirs now.
func (s *Store) UpdateMemory(ctx context.Context, userID, id int64, content string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE memories SET content=?, source='user', updated_at=? WHERE id=? AND user_id=? AND deleted_at=0`,
		content, now(), id, userID)
	return err
}

// DeleteMemory deletes an entry. It can be restored for a while
// (RestoreMemory); entries deleted longer ago go for good.
func (s *Store) DeleteMemory(ctx context.Context, userID, id int64) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	t := now()
	ok, err := trashMemory(ctx, tx, userID, id, t)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memories_deleted WHERE user_id=? AND deleted_at<?`, userID, t-keepDeleted.Milliseconds()); err != nil {
		return false, err
	}
	return ok, tx.Commit()
}

// trashMemory moves a live entry out of memories into memories_deleted,
// where no build reads it as memory, and says whether there was one.
func trashMemory(ctx context.Context, tx *sql.Tx, userID, id, at int64) (bool, error) {
	res, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO memories_deleted(id, user_id, content, source, created_at, updated_at, deleted_at)
		SELECT id, user_id, content, source, created_at, updated_at, ? FROM memories WHERE id=? AND user_id=? AND deleted_at=0`, at, id, userID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM memories WHERE id=? AND user_id=?`, id, userID)
	return err == nil, err
}

// RestoreMemory brings back a deleted entry, with its id.
func (s *Store) RestoreMemory(ctx context.Context, userID, id int64) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO memories(id, user_id, content, source, created_at, updated_at)
		SELECT id, user_id, content, source, created_at, updated_at FROM memories_deleted WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memories_deleted WHERE id=? AND user_id=?`, id, userID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// EditMemories changes the user's memory in one transaction. edit sees the
// live entries and returns the ids to delete and the entries to add, or an
// error, which leaves everything as it was. Since edit runs inside the
// transaction, it decides on what's really there, even when Alexa, the
// review and the user edit at once. It returns the entries added.
func (s *Store) EditMemories(ctx context.Context, userID int64, edit func(live []*Memory) (remove []int64, add []NewMemory, err error)) ([]*Memory, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+memoryCols+` FROM memories WHERE user_id=? AND deleted_at=0 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	live, err := scanMemories(rows)
	if err != nil {
		return nil, err
	}
	remove, add, err := edit(live)
	if err != nil {
		return nil, err
	}
	t := now()
	for _, id := range remove {
		ok, err := trashMemory(ctx, tx, userID, id, t)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("memory %d: %w", id, ErrNotFound)
		}
	}
	var added []*Memory
	for _, a := range add {
		m := &Memory{Content: a.Content, Source: a.Source, CreatedAt: t, UpdatedAt: t}
		res, err := tx.ExecContext(ctx, `INSERT INTO memories(user_id, content, source, created_at, updated_at) VALUES(?,?,?,?,?)`,
			userID, m.Content, m.Source, m.CreatedAt, m.UpdatedAt)
		if err != nil {
			return nil, err
		}
		m.ID, _ = res.LastInsertId()
		added = append(added, m)
	}
	return added, tx.Commit()
}
