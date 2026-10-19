package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Destruction struct {
	ID        string
	OwnerID   string
	Kind      string
	TargetID  string
	Name      string
	Status    string
	Body      []byte
	CreatedAt time.Time
}

func (s *Store) CreateDestruction(ctx context.Context, ownerID, id, kind, targetID, name string, body []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO destructions (id, owner_id, kind, target_id, name, status, body)
		VALUES ($1, $2, $3, $4, $5, 'running', $6)`, id, ownerID, kind, targetID, name, body)
	return err
}

func (s *Store) UpdateDestruction(ctx context.Context, ownerID, id, status string, body []byte) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE destructions SET status = $3, body = $4 WHERE id = $1 AND owner_id = $2`, id, ownerID, status, body)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Destruction(ctx context.Context, ownerID, id string) (Destruction, error) {
	var row Destruction
	err := s.pool.QueryRow(ctx, `
		SELECT id, owner_id, kind, target_id, name, status, body, created_at
		FROM destructions WHERE id = $1 AND owner_id = $2`, id, ownerID).Scan(
		&row.ID, &row.OwnerID, &row.Kind, &row.TargetID, &row.Name, &row.Status, &row.Body, &row.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Destruction{}, ErrNotFound
	}
	return row, err
}

func (s *Store) RunningDestruction(ctx context.Context, ownerID, kind, targetID string) (Destruction, error) {
	var row Destruction
	err := s.pool.QueryRow(ctx, `
		SELECT id, owner_id, kind, target_id, name, status, body, created_at
		FROM destructions
		WHERE owner_id = $1 AND kind = $2 AND target_id = $3 AND status = 'running'
		ORDER BY created_at DESC
		LIMIT 1`, ownerID, kind, targetID).Scan(
		&row.ID, &row.OwnerID, &row.Kind, &row.TargetID, &row.Name, &row.Status, &row.Body, &row.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Destruction{}, ErrNotFound
	}
	return row, err
}

func (s *Store) ListRunningDestructions(ctx context.Context, ownerID string) ([]Destruction, error) {
	return s.listDestructions(ctx, `
		SELECT id, owner_id, kind, target_id, name, status, body, created_at
		FROM destructions WHERE owner_id = $1 AND status = 'running' ORDER BY created_at`, ownerID)
}

func (s *Store) RunningDestructions(ctx context.Context) ([]Destruction, error) {
	return s.listDestructions(ctx, `
		SELECT id, owner_id, kind, target_id, name, status, body, created_at
		FROM destructions WHERE status = 'running' ORDER BY created_at`)
}

func (s *Store) listDestructions(ctx context.Context, query string, args ...any) ([]Destruction, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Destruction
	for rows.Next() {
		var row Destruction
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Kind, &row.TargetID, &row.Name, &row.Status, &row.Body, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) DeleteApplication(ctx context.Context, ownerID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM applications WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return s.ForgetAppSecretGrant(ctx, id)
}

func (s *Store) ApplicationNames(ctx context.Context, ownerID, applicationID string) ([]Name, error) {
	return s.customNames(ctx, `
		SELECT d.hostname, d.zone, d.secret_id
		FROM domains d
		JOIN applications a ON a.id = d.application_id
		WHERE a.owner_id = $1 AND a.id = $2
		  AND d.kind = 'custom' AND d.zone <> '' AND d.secret_id <> ''`, ownerID, applicationID)
}

func (s *Store) ServerNames(ctx context.Context, ownerID, serverID string) ([]Name, error) {
	return s.customNames(ctx, `
		SELECT d.hostname, d.zone, d.secret_id
		FROM domains d
		JOIN applications a ON a.id = d.application_id
		WHERE a.owner_id = $1 AND a.server_id = $2
		  AND d.kind = 'custom' AND d.zone <> '' AND d.secret_id <> ''`, ownerID, serverID)
}

func (s *Store) customNames(ctx context.Context, query, ownerID, id string) ([]Name, error) {
	rows, err := s.pool.Query(ctx, query, ownerID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Name
	for rows.Next() {
		var name Name
		if err := rows.Scan(&name.Hostname, &name.Zone, &name.SecretID); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
