package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type ImageSnap struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type RolloutApp struct {
	ID          string
	OwnerID     string
	Destination string
	ServerID    string
	ClusterID   string
	RunStatus   string
	Services    []Service
}

func (s *Store) SetServiceImages(ctx context.Context, applicationID string, images map[string]string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT name, published FROM application_services WHERE application_id = $1`, applicationID)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	published := ""
	for rows.Next() {
		var name string
		var pub bool
		if err := rows.Scan(&name, &pub); err != nil {
			rows.Close()
			return err
		}
		known[name] = true
		if pub {
			published = name
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(known) == 0 || len(known) != len(images) {
		return ErrNotFound
	}
	for name := range images {
		if !known[name] {
			return ErrNotFound
		}
	}
	for name, image := range images {
		if _, err := tx.Exec(ctx, `UPDATE application_services SET image = $3 WHERE application_id = $1 AND name = $2`, applicationID, name, image); err != nil {
			return err
		}
	}
	if published != "" {
		if _, err := tx.Exec(ctx, `UPDATE applications SET source = $2 WHERE id = $1`, applicationID, images[published]); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) BeginRollout(ctx context.Context, applicationID, runStatus string, images []ImageSnap) error {
	raw, err := json.Marshal(images)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO image_rollbacks (application_id, images, run_status)
		VALUES ($1, $2, $3)
		ON CONFLICT (application_id) DO UPDATE SET images = EXCLUDED.images, run_status = EXCLUDED.run_status, started_at = now()`,
		applicationID, raw, runStatus); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE applications SET rollout = 'updating', rollout_detail = '' WHERE id = $1`, applicationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) FinishRollout(ctx context.Context, applicationID, rollout, detail, runStatus string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM image_rollbacks WHERE application_id = $1`, applicationID); err != nil {
		return err
	}
	if runStatus == "" {
		if _, err := tx.Exec(ctx, `UPDATE applications SET rollout = $2, rollout_detail = $3 WHERE id = $1`, applicationID, rollout, detail); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE applications SET rollout = $2, rollout_detail = $3, run_status = $4 WHERE id = $1`, applicationID, rollout, detail, runStatus); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) RolloutImages(ctx context.Context, applicationID string) (string, []ImageSnap, error) {
	var status string
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT run_status, images FROM image_rollbacks WHERE application_id = $1`, applicationID).Scan(&status, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	var images []ImageSnap
	if err := json.Unmarshal(raw, &images); err != nil {
		return "", nil, err
	}
	return status, images, nil
}

func (s *Store) PendingRollouts(ctx context.Context) ([]string, []time.Time, error) {
	rows, err := s.pool.Query(ctx, `SELECT application_id, started_at FROM image_rollbacks ORDER BY started_at`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids []string
	var started []time.Time
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		started = append(started, at)
	}
	return ids, started, rows.Err()
}

func (s *Store) RolloutApp(ctx context.Context, id string) (RolloutApp, error) {
	var app RolloutApp
	err := s.pool.QueryRow(ctx, `
		SELECT id, owner_id, destination_kind, COALESCE(server_id, ''), COALESCE(cluster_id, ''), run_status
		FROM applications WHERE id = $1`, id).Scan(&app.ID, &app.OwnerID, &app.Destination, &app.ServerID, &app.ClusterID, &app.RunStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return RolloutApp{}, ErrNotFound
	}
	if err != nil {
		return RolloutApp{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, image, published, disk, replicas, listen, disk_path, disk_gi, args, backup, spec
		FROM application_services WHERE application_id = $1 ORDER BY published DESC, name`, id)
	if err != nil {
		return RolloutApp{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var svc Service
		var args string
		var specRaw []byte
		if err := rows.Scan(&svc.ID, &svc.Name, &svc.Image, &svc.Published, &svc.Disk, &svc.Replicas, &svc.Listen, &svc.DiskPath, &svc.DiskGi, &args, &svc.Backup, &specRaw); err != nil {
			return RolloutApp{}, err
		}
		svc.Args = splitArgs(args)
		svc.Spec = readSpec(specRaw)
		app.Services = append(app.Services, svc)
	}
	return app, rows.Err()
}

// SetServiceReplicas changes the number of copies of the named services.
func (s *Store) SetServiceReplicas(ctx context.Context, applicationID string, copies map[string]int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for name, count := range copies {
		if count < 1 {
			return ErrNotFound
		}
		tag, err := tx.Exec(ctx, `UPDATE application_services SET replicas = $3 WHERE application_id = $1 AND name = $2`, applicationID, name, count)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE applications SET run_status = 'preparing', preparing_since = now() WHERE id = $1 AND NOT paused`, applicationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetServiceBackup moves a service between its node disk and the copy of its
// SQLite files in the cluster's object store.
func (s *Store) SetServiceBackup(ctx context.Context, applicationID, name, state string) error {
	if state != "" && state != "migrating" && state != "ready" {
		return ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, `UPDATE application_services SET backup = $3 WHERE application_id = $1 AND name = $2`, applicationID, name, state)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetServiceDatabase moves a service between its disk and a replicated
// PostgreSQL.
func (s *Store) SetServiceDatabase(ctx context.Context, applicationID, name, state string) error {
	if state != "" && state != "migrating" && state != "ready" {
		return ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, `UPDATE application_services SET database = $3 WHERE application_id = $1 AND name = $2`, applicationID, name, state)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetVariables replaces the variables of an application, sealed values included.
func (s *Store) SetVariables(ctx context.Context, applicationID string, vars map[string]string) error {
	raw, err := json.Marshal(vars)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE applications SET variables = $2 WHERE id = $1`, applicationID, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// KeepRollbackVariables keeps the variables to put back if a rollout fails.
func (s *Store) KeepRollbackVariables(ctx context.Context, applicationID string, vars map[string]string) error {
	raw, err := json.Marshal(vars)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE image_rollbacks SET variables = $2 WHERE application_id = $1`, applicationID, raw)
	return err
}

// RollbackVariables are the variables kept by KeepRollbackVariables, nil when
// the rollout did not change them.
func (s *Store) RollbackVariables(ctx context.Context, applicationID string) (map[string]string, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT variables FROM image_rollbacks WHERE application_id = $1`, applicationID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil || raw == nil {
		return nil, err
	}
	vars := map[string]string{}
	if err := json.Unmarshal(raw, &vars); err != nil {
		return nil, err
	}
	return vars, nil
}
