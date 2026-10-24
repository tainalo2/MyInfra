package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Confirmation is what a confirmation code stands for.
type Confirmation struct {
	TokenID     string
	Tool        string
	ArgsHash    string
	Fingerprint string
	ExpiresAt   time.Time
}

func (s *Store) CreateConfirmation(ctx context.Context, codeHash string, c Confirmation) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM mcp_confirmations WHERE expires_at < now() - interval '1 day'`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mcp_confirmations (code_hash, token_id, tool, args_hash, fingerprint, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		codeHash, c.TokenID, c.Tool, c.ArgsHash, c.Fingerprint, c.ExpiresAt)
	return err
}

// TakeConfirmation uses a code up: a code that was not live, or was already
// used, is ErrNotFound. Once taken, a code never works again, even when the
// caller then finds it does not fit.
func (s *Store) TakeConfirmation(ctx context.Context, codeHash string) (Confirmation, error) {
	var c Confirmation
	err := s.pool.QueryRow(ctx, `
		UPDATE mcp_confirmations SET used_at = now()
		WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING token_id, tool, args_hash, fingerprint, expires_at`, codeHash).
		Scan(&c.TokenID, &c.Tool, &c.ArgsHash, &c.Fingerprint, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Confirmation{}, ErrNotFound
	}
	return c, err
}

// ConfirmationLive says whether a code can still be taken, without taking it.
func (s *Store) ConfirmationLive(ctx context.Context, codeHash string) (bool, error) {
	var live bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM mcp_confirmations WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now())`, codeHash).
		Scan(&live)
	return live, err
}
