package store

import (
	"context"
	"time"
)

type OpenDeployment struct {
	ID        string
	Kind      string
	Name      string
	Status    string
	Detail    string
	Raw       []byte
	CreatedAt time.Time
}

func (s *Store) OpenDeployments(ctx context.Context, ownerID string) ([]OpenDeployment, error) {
	var out []OpenDeployment
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, status, deploy, created_at
		FROM servers
		WHERE owner_id = $1 AND status IN ('pending', 'deploying')
		ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item OpenDeployment
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Raw, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		item.Kind = "server"
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT id, name,
		       CASE WHEN rollout = 'updating' THEN 'updating' ELSE run_status END,
		       rollout_detail, created_at
		FROM applications
		WHERE owner_id = $1 AND paused = false AND (run_status = 'preparing' OR rollout = 'updating')
		ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item OpenDeployment
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Detail, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		item.Kind = "application"
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT id, body, created_at
		FROM template_runs
		WHERE owner_id = $1 AND status = 'running'
		ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item OpenDeployment
		if err := rows.Scan(&item.ID, &item.Raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Kind = "template"
		item.Status = "running"
		out = append(out, item)
	}
	return out, rows.Err()
}
