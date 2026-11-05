package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// The database records its schema version in PRAGMA user_version.
//
// Version 1 is the baseline: the CREATE ... IF NOT EXISTS statements in
// schema, sessionSchema and loginSchema, as they stood when versioning began.
// They run on every start, as before, so a fresh database and one created
// before versioning (still at version 0) both reach the baseline the same way.
//
// To change the schema, append a migration to migrations; never edit, reorder
// or remove one that has shipped. Each runs once, in order, in its own
// transaction together with the version bump, so a failed migration leaves the
// database as it was. Migrations run on fresh databases too, so don't also
// change the baseline statements for the same change. A brand-new table can
// still be added to a baseline constant with IF NOT EXISTS, but anything that
// alters an existing table (a column, an index, a backfill) is a migration.
const schemaBaseline = 1

type migration struct {
	name string // what it changes, for errors and the log
	sql  string
}

// migrations take the database from version schemaBaseline+i to
// schemaBaseline+i+1.
var migrations = []migration{
	{
		// Bounded memory: who wrote each entry, when it last changed, and a
		// soft delete. Existing entries count as the user's, so the
		// background review never touches them; their content stays as it
		// is.
		name: "memory source, updated_at and soft delete",
		sql: `ALTER TABLE memories ADD COLUMN source TEXT NOT NULL DEFAULT 'user';
ALTER TABLE memories ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memories ADD COLUMN deleted_at INTEGER NOT NULL DEFAULT 0;
UPDATE memories SET updated_at = created_at;
CREATE INDEX IF NOT EXISTS memories_live ON memories(user_id, deleted_at, id);`,
	},
	{
		// Recall: when each conversation item was added, and the search
		// index of the folded conversation (recall.go).
		name: "session item times and recall",
		sql:  `ALTER TABLE session_items ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;` + recallSchema,
	},
	{
		// Each user's rows are found without reading everyone's (BH-7):
		// their notifications, waiting messages, sign-ins and schedules,
		// and the steps still running when a run ends. Expired sign-ins
		// are found for the sweep (Prune). Memories have memories_live.
		name: "per-user indexes",
		sql: `CREATE INDEX IF NOT EXISTS notifications_user ON notifications(user_id, id);
CREATE INDEX IF NOT EXISTS inbox_user ON inbox(user_id, id);
CREATE INDEX IF NOT EXISTS auth_sessions_user ON auth_sessions(user_id);
CREATE INDEX IF NOT EXISTS auth_sessions_expiry ON auth_sessions(expires_at);
CREATE INDEX IF NOT EXISTS schedules_user ON schedules(user_id, created_at);
CREATE INDEX IF NOT EXISTS feed_status ON feed(user_id, status);`,
	},
	{
		// Deleted memories wait out the time they can be restored in their
		// own table, not behind deleted_at in memories: a build from before
		// soft delete reads every row of memories, so run on this database
		// after a rollback, it would put what the user deleted back into
		// Alexa's instructions. memories.deleted_at is no longer set, and
		// reads still skip a row that has it.
		name: "deleted memories apart",
		sql: `CREATE TABLE IF NOT EXISTS memories_deleted (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'user',
  created_at INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS memories_deleted_user ON memories_deleted(user_id, deleted_at);
INSERT OR REPLACE INTO memories_deleted(id, user_id, content, source, created_at, updated_at, deleted_at)
  SELECT id, user_id, content, source, created_at, updated_at, deleted_at FROM memories WHERE deleted_at>0;
DELETE FROM memories WHERE deleted_at>0;`,
	},
}

// schemaVersion is the version a database ends at with this build.
func schemaVersion() int { return schemaBaseline + len(migrations) }

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema+sessionSchema+loginSchema); err != nil {
		return err
	}
	v, err := userVersion(ctx, db)
	if err != nil {
		return err
	}
	if v < schemaBaseline {
		if err := setUserVersion(ctx, db, schemaBaseline); err != nil {
			return err
		}
		v = schemaBaseline
	}
	if v > schemaVersion() {
		// A newer build migrated this database and this one was rolled back
		// to. Its queries name their columns, so it can usually keep working;
		// refusing to start would turn a rollback into an outage.
		slog.Warn("database schema is newer than this build", "version", v, "known", schemaVersion())
		return nil
	}
	for i := v - schemaBaseline; i < len(migrations); i++ {
		to := schemaBaseline + i + 1
		if err := runMigration(ctx, db, to, migrations[i]); err != nil {
			return fmt.Errorf("migration %d (%s): %w", to, migrations[i].name, err)
		}
		slog.Info("migrated database", "version", to, "change", migrations[i].name)
	}
	return nil
}

func runMigration(ctx context.Context, db *sql.DB, to int, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	// user_version is part of the database header, so the bump commits or
	// rolls back with the migration.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", to)); err != nil {
		return err
	}
	return tx.Commit()
}

func userVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v int
	err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

func setUserVersion(ctx context.Context, db *sql.DB, v int) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v))
	return err
}
