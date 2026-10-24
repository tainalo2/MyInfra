package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// APIToken is a personal access token: a program acting for a person, with a
// role no higher than the one it was made with. Only its hash is kept.
type APIToken struct {
	ID         string
	UserID     string
	UserLogin  string
	Name       string
	Role       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
}

func (s *Store) CreateAPIToken(ctx context.Context, token APIToken, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO api_tokens (id, user_id, name, token_hash, role, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		token.ID, token.UserID, token.Name, tokenHash, token.Role, token.ExpiresAt)
	return err
}

// ListAPITokens lists the tokens of one person, or of everyone when userID is
// empty. Expired tokens are left out: they no longer open anything.
func (s *Store) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.user_id, COALESCE(u.login, ''), t.name, t.role, t.created_at, t.last_used_at, t.expires_at
		FROM api_tokens t
		LEFT JOIN users u ON u.id = t.user_id
		WHERE ($1 = '' OR t.user_id = $1)
		  AND (t.expires_at IS NULL OR t.expires_at > now())
		ORDER BY t.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var row APIToken
		if err := rows.Scan(&row.ID, &row.UserID, &row.UserLogin, &row.Name, &row.Role, &row.CreatedAt, &row.LastUsedAt, &row.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// DeleteAPIToken revokes a token; userID empty lets an admin revoke anyone's.
func (s *Store) DeleteAPIToken(ctx context.Context, userID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_tokens WHERE id = $1 AND ($2 = '' OR user_id = $2)`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UserByAPIToken finds the live token with this hash and notes its use, at
// most once a minute so that a busy client does not write on every call.
func (s *Store) UserByAPIToken(ctx context.Context, tokenHash string) (APIToken, error) {
	var row APIToken
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, role, created_at, last_used_at, expires_at
		FROM api_tokens
		WHERE token_hash = $1 AND (expires_at IS NULL OR expires_at > now())`, tokenHash).
		Scan(&row.ID, &row.UserID, &row.Name, &row.Role, &row.CreatedAt, &row.LastUsedAt, &row.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIToken{}, ErrNotFound
	}
	if err != nil {
		return APIToken{}, err
	}
	if row.LastUsedAt == nil || time.Since(*row.LastUsedAt) > time.Minute {
		_, _ = s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, row.ID)
	}
	return row, nil
}
