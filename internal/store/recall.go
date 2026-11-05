package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// ---------- recall (search of the folded conversation) ----------

// Compaction folds the older conversation into a summary and deletes its
// items. Before it does, what was said in them (the user's messages,
// Alexa's says and asks) is copied here, with the reports she wrote, so
// recall can still find it. Nothing here depends on session_items: a
// document keeps its own text and date. The full-text index (recall, FTS5)
// follows recall_docs through triggers, and a user's documents go with
// them (ON DELETE CASCADE fires the delete trigger).
const recallSchema = `
CREATE TABLE IF NOT EXISTS recall_docs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  ref TEXT NOT NULL,
  kind TEXT NOT NULL,
  item_id INTEGER NOT NULL DEFAULT 0,
  at INTEGER NOT NULL DEFAULT 0,
  text TEXT NOT NULL,
  UNIQUE(user_id, ref)
);
CREATE INDEX IF NOT EXISTS recall_docs_item ON recall_docs(user_id, item_id);
CREATE VIRTUAL TABLE IF NOT EXISTS recall USING fts5(text, content='recall_docs', content_rowid='id', tokenize='unicode61 remove_diacritics 2');
CREATE TRIGGER IF NOT EXISTS recall_docs_ai AFTER INSERT ON recall_docs BEGIN
  INSERT INTO recall(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS recall_docs_ad AFTER DELETE ON recall_docs BEGIN
  INSERT INTO recall(recall, rowid, text) VALUES ('delete', old.id, old.text);
END;
CREATE TRIGGER IF NOT EXISTS recall_docs_au AFTER UPDATE ON recall_docs BEGIN
  INSERT INTO recall(recall, rowid, text) VALUES ('delete', old.id, old.text);
  INSERT INTO recall(rowid, text) VALUES (new.id, new.text);
END;
`

// RecallDoc is one searchable moment: something said in the conversation,
// or a report.
type RecallDoc struct {
	Ref    string // "item:<id>" or "report:<id>"
	Kind   string // user | schedule | said | asked | report
	ItemID int64  // the conversation item it came from; 0 for a report
	At     int64  // unix ms; 0 if unknown
	Text   string // for a search hit, the snippet around the match, matches in [brackets]
}

const recallCols = `d.ref, d.kind, d.item_id, d.at`

func scanRecall(rows *sql.Rows) ([]RecallDoc, error) {
	defer rows.Close()
	var out []RecallDoc
	for rows.Next() {
		var d RecallDoc
		if err := rows.Scan(&d.Ref, &d.Kind, &d.ItemID, &d.At, &d.Text); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// IndexRecall adds documents, replacing those with the same ref.
func (s *Store) IndexRecall(ctx context.Context, userID int64, docs []RecallDoc) error {
	if len(docs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := indexRecall(ctx, tx, userID, docs); err != nil {
		return err
	}
	return tx.Commit()
}

// indexRecall adds documents in tx, replacing those with the same ref.
func indexRecall(ctx context.Context, tx *sql.Tx, userID int64, docs []RecallDoc) error {
	for _, d := range docs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recall_docs(user_id, ref, kind, item_id, at, text) VALUES(?,?,?,?,?,?)
			ON CONFLICT(user_id, ref) DO UPDATE SET kind=excluded.kind, item_id=excluded.item_id, at=excluded.at, text=excluded.text`,
			userID, d.Ref, d.Kind, d.ItemID, d.At, d.Text); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRecall removes documents by ref.
func (s *Store) DeleteRecall(ctx context.Context, userID int64, refs ...string) error {
	for _, ref := range refs {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM recall_docs WHERE user_id=? AND ref=?`, userID, ref); err != nil {
			return err
		}
	}
	return nil
}

// SearchRecall finds the user's documents matching an FTS5 query, best
// first, optionally only those from after (inclusive) and before
// (exclusive), in unix ms; 0 leaves a bound open. The query must already
// be safe FTS5 syntax (the agent's ftsQuery makes it so).
func (s *Store) SearchRecall(ctx context.Context, userID int64, match string, after, before int64, limit int) ([]RecallDoc, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recallCols+`, snippet(recall, 0, '[', ']', '…', 24)
		FROM recall JOIN recall_docs d ON d.id = recall.rowid
		WHERE recall MATCH ? AND d.user_id = ? AND (? = 0 OR d.at >= ?) AND (? = 0 OR d.at < ?)
		ORDER BY recall.rank LIMIT ?`, match, userID, after, after, before, before, limit)
	if err != nil {
		return nil, err
	}
	return scanRecall(rows)
}

// GetRecall returns one document, with its whole text.
func (s *Store) GetRecall(ctx context.Context, userID int64, ref string) (*RecallDoc, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recallCols+`, d.text FROM recall_docs d WHERE d.user_id=? AND d.ref=?`, userID, ref)
	if err != nil {
		return nil, err
	}
	docs, err := scanRecall(rows)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrNotFound
	}
	return &docs[0], nil
}

// RecallAround returns the conversation documents just before and after
// the item, up to n on each side, oldest first, with the item's own.
func (s *Store) RecallAround(ctx context.Context, userID, itemID int64, n int) ([]RecallDoc, error) {
	before, err := s.db.QueryContext(ctx, `SELECT `+recallCols+`, d.text FROM recall_docs d
		WHERE d.user_id=? AND d.item_id>0 AND d.item_id<? ORDER BY d.item_id DESC LIMIT ?`, userID, itemID, n)
	if err != nil {
		return nil, err
	}
	earlier, err := scanRecall(before)
	if err != nil {
		return nil, err
	}
	after, err := s.db.QueryContext(ctx, `SELECT `+recallCols+`, d.text FROM recall_docs d
		WHERE d.user_id=? AND d.item_id>=? ORDER BY d.item_id LIMIT ?`, userID, itemID, n+1)
	if err != nil {
		return nil, err
	}
	later, err := scanRecall(after)
	if err != nil {
		return nil, err
	}
	out := make([]RecallDoc, 0, len(earlier)+len(later))
	for i := len(earlier) - 1; i >= 0; i-- {
		out = append(out, earlier[i])
	}
	return append(out, later...), nil
}

// ItemTimes returns when each conversation item after id after, up to id
// through, was added (unix ms), for the items that are still there and
// have a time: those from before times were kept have none.
func (s *Store) ItemTimes(ctx context.Context, userID, after, through int64) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, created_at FROM session_items WHERE user_id=? AND id>? AND id<=? AND created_at>0`, userID, after, through)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}

// ItemsThrough returns the conversation items after id after, up to id
// through, oldest first: what's left of a stretch compacted before recall
// kept it.
func (s *Store) ItemsThrough(ctx context.Context, userID, after, through int64) ([]ItemRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, json FROM session_items WHERE user_id=? AND id>? AND id<=? ORDER BY id`, userID, after, through)
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
