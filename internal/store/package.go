package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Package is an application package a person keeps: Body is the JSON of a
// pack.Package.
type Package struct {
	ID        string
	Name      string
	Body      []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s *Store) CreatePackage(ctx context.Context, ownerID, id, name string, body []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO packages (id, owner_id, name, body) VALUES ($1, $2, $3, $4)`, id, ownerID, name, body)
	return nameTaken(err)
}

func (s *Store) UpdatePackage(ctx context.Context, ownerID, id, name string, body []byte) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE packages SET name = $3, body = $4, updated_at = now() WHERE id = $1 AND owner_id = $2`, id, ownerID, name, body)
	if err != nil {
		return nameTaken(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePackage(ctx context.Context, ownerID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM packages WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Package(ctx context.Context, ownerID, id string) (Package, error) {
	var row Package
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, body, created_at, updated_at FROM packages WHERE id = $1 AND owner_id = $2`, id, ownerID).
		Scan(&row.ID, &row.Name, &row.Body, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Package{}, ErrNotFound
	}
	return row, err
}

func (s *Store) ListPackages(ctx context.Context, ownerID string) ([]Package, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, body, created_at, updated_at FROM packages WHERE owner_id = $1 ORDER BY lower(name)`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Package
	for rows.Next() {
		var row Package
		if err := rows.Scan(&row.ID, &row.Name, &row.Body, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
