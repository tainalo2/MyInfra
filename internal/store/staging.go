package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"myinfra/internal/commit"
)

// StagingTTL is the sliding expiration window for a staging.
const StagingTTL = 7 * 24 * time.Hour

type Staging struct {
	ID             string
	OwnerID        string
	Kind           string // template | package
	TargetID       string
	Name           string
	Body           []byte
	Patches        []byte // JSON array of proposed (unapplied) patches
	Context        string
	CreatedAt      time.Time
	LastAccessedAt time.Time
	ExpiresAt      time.Time
}

func (s *Store) CreateStaging(ctx context.Context, ownerID, id, kind, targetID, name string, body, patches []byte, contextText string) error {
	now := time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO template_stagings (id, owner_id, kind, target_id, name, body, patches, context, created_at, last_accessed_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, $10)`,
		id, ownerID, kind, targetID, name, body, patches, contextText, now, now.Add(StagingTTL))
	return err
}

func (s *Store) Staging(ctx context.Context, ownerID, id string) (Staging, error) {
	var row Staging
	err := s.pool.QueryRow(ctx, `
		SELECT id, owner_id, kind, target_id, name, body, patches, context, created_at, last_accessed_at, expires_at
		FROM template_stagings WHERE id = $1 AND owner_id = $2`, id, ownerID).Scan(
		&row.ID, &row.OwnerID, &row.Kind, &row.TargetID, &row.Name, &row.Body, &row.Patches, &row.Context,
		&row.CreatedAt, &row.LastAccessedAt, &row.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Staging{}, ErrNotFound
	}
	return row, err
}

func (s *Store) TouchStaging(ctx context.Context, ownerID, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE template_stagings
		SET last_accessed_at = now(), expires_at = now() + $3::interval
		WHERE id = $1 AND owner_id = $2`, id, ownerID, StagingTTL.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateStaging(ctx context.Context, ownerID, id string, body, patches []byte, contextText string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE template_stagings
		SET body = $3, patches = $4, context = $5, last_accessed_at = now(), expires_at = now() + $6::interval
		WHERE id = $1 AND owner_id = $2`, id, ownerID, body, patches, contextText, StagingTTL.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteStaging(ctx context.Context, ownerID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM template_stagings WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListStagings(ctx context.Context, ownerID string) ([]Staging, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, kind, target_id, name, body, patches, context, created_at, last_accessed_at, expires_at
		FROM template_stagings WHERE owner_id = $1 ORDER BY last_accessed_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Staging
	for rows.Next() {
		var row Staging
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Kind, &row.TargetID, &row.Name, &row.Body, &row.Patches, &row.Context,
			&row.CreatedAt, &row.LastAccessedAt, &row.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetPatch returns the patch with the given id, searching all stagings.
func (s *Store) GetPatch(ctx context.Context, id string) (commit.Patch, error) {
	rows, err := s.pool.Query(ctx, `SELECT patches FROM template_stagings`)
	if err != nil {
		return commit.Patch{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return commit.Patch{}, err
		}
		var patches []commit.Patch
		if err := json.Unmarshal(raw, &patches); err != nil {
			continue
		}
		for _, p := range patches {
			if p.ID == id {
				return p, nil
			}
		}
	}
	if err := rows.Err(); err != nil {
		return commit.Patch{}, err
	}
	return commit.Patch{}, ErrNotFound
}

// MarkApplied removes the given patch ids from every staging's pending list.
func (s *Store) MarkApplied(ctx context.Context, ids []string) error {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	rows, err := s.pool.Query(ctx, `SELECT id, owner_id, patches FROM template_stagings`)
	if err != nil {
		return err
	}
	type row struct {
		id, owner string
		patches   []byte
	}
	var targets []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.owner, &r.patches); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range targets {
		var patches []commit.Patch
		if err := json.Unmarshal(r.patches, &patches); err != nil {
			continue
		}
		var kept []commit.Patch
		changed := false
		for _, p := range patches {
			if set[p.ID] {
				changed = true
				continue
			}
			kept = append(kept, p)
		}
		if !changed {
			continue
		}
		if kept == nil {
			kept = []commit.Patch{}
		}
		b, err := json.Marshal(kept)
		if err != nil {
			return err
		}
		if _, err := s.pool.Exec(ctx, `UPDATE template_stagings SET patches = $3, last_accessed_at = now(), expires_at = now() + $4::interval WHERE id = $1 AND owner_id = $2`, r.id, r.owner, b, StagingTTL.String()); err != nil {
			return err
		}
	}
	return nil
}

// PurgeExpiredStagings deletes stagings whose TTL has elapsed. No implicit commit.
func (s *Store) PurgeExpiredStagings(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM template_stagings WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}