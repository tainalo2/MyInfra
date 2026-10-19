package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Template struct {
	ID        string
	Name      string
	Body      []byte
	CreatedAt time.Time
}

type TemplateRun struct {
	ID         string
	OwnerID    string
	TemplateID string
	Status     string
	ClusterID  string
	Body       []byte
	CreatedAt  time.Time
}

func (s *Store) CreateTemplate(ctx context.Context, ownerID, id, name string, body []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO templates (id, owner_id, name, body) VALUES ($1, $2, $3, $4)`, id, ownerID, name, body)
	return nameTaken(err)
}

func (s *Store) UpdateTemplate(ctx context.Context, ownerID, id, name string, body []byte) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE templates SET name = $3, body = $4 WHERE id = $1 AND owner_id = $2`, id, ownerID, name, body)
	if err != nil {
		return nameTaken(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteTemplate(ctx context.Context, ownerID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM templates WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Template(ctx context.Context, ownerID, id string) (Template, error) {
	var row Template
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, body, created_at FROM templates WHERE id = $1 AND owner_id = $2`, id, ownerID).Scan(&row.ID, &row.Name, &row.Body, &row.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Template{}, ErrNotFound
	}
	return row, err
}

func (s *Store) ListTemplates(ctx context.Context, ownerID string) ([]Template, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, body, created_at FROM templates WHERE owner_id = $1 ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		var row Template
		if err := rows.Scan(&row.ID, &row.Name, &row.Body, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) CreateTemplateRun(ctx context.Context, ownerID, id, templateID, clusterID string, body []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO template_runs (id, owner_id, template_id, status, cluster_id, body)
		VALUES ($1, $2, $3, 'running', $4, $5)`, id, ownerID, templateID, clusterID, body)
	return err
}

func (s *Store) PersistRunningTemplateRun(ctx context.Context, ownerID, id, status, clusterID string, body []byte) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE template_runs
		SET status = $3, cluster_id = $4, body = $5
		WHERE id = $1 AND owner_id = $2 AND status = 'running'`,
		id, ownerID, status, clusterID, body)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStopped
	}
	return nil
}

func (s *Store) CancelTemplateRun(ctx context.Context, ownerID, id string, body []byte) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE template_runs SET status = 'cancelled', body = $3
		WHERE id = $1 AND owner_id = $2 AND status = 'running'`,
		id, ownerID, body)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStopped
	}
	return nil
}

func (s *Store) UpdateTemplateRun(ctx context.Context, ownerID, id, status, clusterID string, body []byte) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE template_runs SET status = $3, cluster_id = $4, body = $5
		WHERE id = $1 AND owner_id = $2`, id, ownerID, status, clusterID, body)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) TemplateRun(ctx context.Context, ownerID, id string) (TemplateRun, error) {
	var row TemplateRun
	err := s.pool.QueryRow(ctx, `
		SELECT id, owner_id, template_id, status, cluster_id, body, created_at
		FROM template_runs WHERE id = $1 AND owner_id = $2`, id, ownerID).Scan(
		&row.ID, &row.OwnerID, &row.TemplateID, &row.Status, &row.ClusterID, &row.Body, &row.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TemplateRun{}, ErrNotFound
	}
	return row, err
}

func (s *Store) RunningTemplateRuns(ctx context.Context) ([]TemplateRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, template_id, status, cluster_id, body, created_at
		FROM template_runs WHERE status = 'running' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TemplateRun
	for rows.Next() {
		var row TemplateRun
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.TemplateID, &row.Status, &row.ClusterID, &row.Body, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
