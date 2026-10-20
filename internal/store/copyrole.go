package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// CopyRole says whether this copy was kept as a replica by a transfer, and
// where the copy that drives the machines runs.
type CopyRole struct {
	Replica       bool
	MainMethod    string
	MainClusterID string
	MainURL       string
}

func (s *Store) CopyRole(ctx context.Context) (CopyRole, error) {
	var role CopyRole
	err := s.pool.QueryRow(ctx, `SELECT replica, main_method, main_cluster_id, main_url FROM copy_role WHERE singleton`).
		Scan(&role.Replica, &role.MainMethod, &role.MainClusterID, &role.MainURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return CopyRole{}, nil
	}
	return role, err
}

func (s *Store) SetCopyRole(ctx context.Context, role CopyRole) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO copy_role (singleton, replica, main_method, main_cluster_id, main_url) VALUES (true, $1, $2, $3, $4)
		ON CONFLICT (singleton) DO UPDATE SET replica = EXCLUDED.replica, main_method = EXCLUDED.main_method,
			main_cluster_id = EXCLUDED.main_cluster_id, main_url = EXCLUDED.main_url`,
		role.Replica, role.MainMethod, role.MainClusterID, role.MainURL)
	return err
}
