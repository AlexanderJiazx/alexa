package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Logins are the sign-ins people save for Alexa to use. The password is
// stored encrypted (see internal/vault); the site and username are not, so
// the list can be shown and matched against the page being signed in to.
const loginSchema = `
CREATE TABLE IF NOT EXISTS logins (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  site TEXT NOT NULL,
  username TEXT NOT NULL,
  secret BLOB NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  used_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(user_id, site, username)
);
`

// ErrExists is returned when a login with the same site and username is
// already saved.
var ErrExists = errors.New("already exists")

type Login struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Site      string `json:"site"` // registrable domain, e.g. example.com
	Username  string `json:"username"`
	Secret    []byte `json:"-"` // the encrypted password
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	UsedAt    int64  `json:"usedAt,omitempty"`
}

const loginCols = `id, user_id, site, username, secret, created_at, updated_at, used_at`

func scanLogins(rows *sql.Rows) ([]*Login, error) {
	defer rows.Close()
	var out []*Login
	for rows.Next() {
		var l Login
		if err := rows.Scan(&l.ID, &l.UserID, &l.Site, &l.Username, &l.Secret, &l.CreatedAt, &l.UpdatedAt, &l.UsedAt); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// SaveLogin saves a login, replacing the password of one with the same
// site and username.
func (s *Store) SaveLogin(ctx context.Context, l *Login) error {
	t := now()
	err := s.db.QueryRowContext(ctx, `INSERT INTO logins(user_id, site, username, secret, created_at, updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(user_id, site, username) DO UPDATE SET secret=excluded.secret, updated_at=excluded.updated_at
		RETURNING id, created_at, updated_at, used_at`, l.UserID, l.Site, l.Username, l.Secret, t, t).
		Scan(&l.ID, &l.CreatedAt, &l.UpdatedAt, &l.UsedAt)
	return err
}

// UpdateLogin changes a login's username and password.
func (s *Store) UpdateLogin(ctx context.Context, l *Login) error {
	res, err := s.db.ExecContext(ctx, `UPDATE logins SET username=?, secret=?, updated_at=? WHERE id=? AND user_id=?`, l.Username, l.Secret, now(), l.ID, l.UserID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrExists
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Logins returns the user's saved logins by site.
func (s *Store) Logins(ctx context.Context, userID int64) ([]*Login, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+loginCols+` FROM logins WHERE user_id=? ORDER BY site, username`, userID)
	if err != nil {
		return nil, err
	}
	return scanLogins(rows)
}

// SiteLogins returns the user's logins for a site, the most recently used
// first.
func (s *Store) SiteLogins(ctx context.Context, userID int64, site string) ([]*Login, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+loginCols+` FROM logins WHERE user_id=? AND site=? ORDER BY MAX(used_at, updated_at) DESC, id DESC`, userID, site)
	if err != nil {
		return nil, err
	}
	return scanLogins(rows)
}

func (s *Store) GetLogin(ctx context.Context, userID, id int64) (*Login, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+loginCols+` FROM logins WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return nil, err
	}
	list, err := scanLogins(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// LoginUsed records that a login was just used to sign in.
func (s *Store) LoginUsed(ctx context.Context, userID, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE logins SET used_at=? WHERE id=? AND user_id=?`, now(), id, userID)
	return err
}

func (s *Store) DeleteLogin(ctx context.Context, userID, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM logins WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
