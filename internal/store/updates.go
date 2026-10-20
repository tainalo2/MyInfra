package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Update is something a person can update: an application, a server, MyInfra.
type Update struct {
	Kind      string
	TargetID  string
	OwnerID   string
	Current   string
	Available string
	Level     string
	Detail    json.RawMessage
	CheckedAt time.Time
}

// SaveUpdates replaces what a check found for one kind of target. Targets
// left out are up to date. With checked set, only those targets were looked
// at: the others keep what an earlier check found.
func (s *Store) SaveUpdates(ctx context.Context, kind string, found []Update, checked []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if checked == nil {
		if _, err := tx.Exec(ctx, `DELETE FROM updates WHERE kind = $1`, kind); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `DELETE FROM updates WHERE kind = $1 AND target_id = ANY($2)`, kind, checked); err != nil {
		return err
	}
	for _, item := range found {
		detail := item.Detail
		if len(detail) == 0 {
			detail = json.RawMessage("{}")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO updates (kind, target_id, owner_id, current, available, level, detail)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (kind, target_id) DO UPDATE SET owner_id = EXCLUDED.owner_id, current = EXCLUDED.current,
				available = EXCLUDED.available, level = EXCLUDED.level, detail = EXCLUDED.detail, checked_at = now()`,
			kind, item.TargetID, item.OwnerID, item.Current, item.Available, item.Level, detail); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ForgetUpdate removes one target, once it was updated.
func (s *Store) ForgetUpdate(ctx context.Context, kind, targetID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM updates WHERE kind = $1 AND target_id = $2`, kind, targetID)
	return err
}

// Updates lists what can be updated for an owner, MyInfra included.
func (s *Store) Updates(ctx context.Context, owner string) ([]Update, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, target_id, owner_id, current, available, level, detail, checked_at
		FROM updates WHERE owner_id = $1 OR kind = 'myinfra'
		ORDER BY kind, target_id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Update
	for rows.Next() {
		var item Update
		if err := rows.Scan(&item.Kind, &item.TargetID, &item.OwnerID, &item.Current, &item.Available, &item.Level, &item.Detail, &item.CheckedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpdateOf is what is known for one target.
func (s *Store) UpdateOf(ctx context.Context, kind, targetID string) (Update, error) {
	var item Update
	err := s.pool.QueryRow(ctx, `
		SELECT kind, target_id, owner_id, current, available, level, detail, checked_at
		FROM updates WHERE kind = $1 AND target_id = $2`, kind, targetID).
		Scan(&item.Kind, &item.TargetID, &item.OwnerID, &item.Current, &item.Available, &item.Level, &item.Detail, &item.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Update{}, ErrNotFound
	}
	return item, err
}

// ImageBaseline is the build a tag pointed at when MyInfra first saw it. The
// first call keeps digest and returns it.
func (s *Store) ImageBaseline(ctx context.Context, image, digest string) (string, error) {
	var kept string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO image_digests (image, digest) VALUES ($1, $2)
		ON CONFLICT (image) DO UPDATE SET image = EXCLUDED.image
		RETURNING digest`, image, digest).Scan(&kept)
	return kept, err
}
