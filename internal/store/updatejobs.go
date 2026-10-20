package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// UpdateJob updates one server, or the servers of a cluster one by one.
type UpdateJob struct {
	ID         string
	OwnerID    string
	ClusterID  string
	ServerID   string
	Status     string
	Steps      []byte
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// ErrUpdateRunning is returned while another update holds the same servers.
var ErrUpdateRunning = errors.New("update running")

// StartUpdateJob records a job, unless one is running on the same cluster or
// machine.
func (s *Store) StartUpdateJob(ctx context.Context, job UpdateJob) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(842016)`); err != nil {
		return err
	}
	var busy bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM update_jobs WHERE status = 'running' AND owner_id = $1
			AND (($2 <> '' AND cluster_id = $2) OR ($3 <> '' AND server_id = $3)))`,
		job.OwnerID, job.ClusterID, job.ServerID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrUpdateRunning
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO update_jobs (id, owner_id, cluster_id, server_id, status, steps)
		VALUES ($1, $2, $3, $4, 'running', $5)`,
		job.ID, job.OwnerID, job.ClusterID, job.ServerID, job.Steps); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SaveUpdateJob keeps the progress of a job, and its end once it is not running.
func (s *Store) SaveUpdateJob(ctx context.Context, id, status string, steps []byte) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE update_jobs SET status = $2, steps = $3,
			finished_at = CASE WHEN $2 = 'running' THEN NULL ELSE now() END
		WHERE id = $1`, id, status, steps)
	return err
}

const updateJobColumns = `id, owner_id, cluster_id, server_id, status, steps, created_at, finished_at`

func scanUpdateJob(row pgx.Row) (UpdateJob, error) {
	var job UpdateJob
	err := row.Scan(&job.ID, &job.OwnerID, &job.ClusterID, &job.ServerID, &job.Status, &job.Steps, &job.CreatedAt, &job.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateJob{}, ErrNotFound
	}
	return job, err
}

func (s *Store) UpdateJob(ctx context.Context, owner, id string) (UpdateJob, error) {
	return scanUpdateJob(s.pool.QueryRow(ctx, `SELECT `+updateJobColumns+` FROM update_jobs WHERE owner_id = $1 AND id = $2`, owner, id))
}

// RunningUpdateJobs are the jobs to resume.
func (s *Store) RunningUpdateJobs(ctx context.Context) ([]UpdateJob, error) {
	return s.updateJobs(ctx, `SELECT `+updateJobColumns+` FROM update_jobs WHERE status = 'running' ORDER BY created_at`)
}

// RecentUpdateJobs are the jobs of the last week, the newest first.
func (s *Store) RecentUpdateJobs(ctx context.Context, owner string) ([]UpdateJob, error) {
	return s.updateJobs(ctx, `SELECT `+updateJobColumns+` FROM update_jobs
		WHERE owner_id = $1 AND (status = 'running' OR created_at > now() - interval '7 days')
		ORDER BY created_at DESC LIMIT 20`, owner)
}

func (s *Store) updateJobs(ctx context.Context, query string, args ...any) ([]UpdateJob, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UpdateJob
	for rows.Next() {
		job, err := scanUpdateJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// SetServerStatus moves a server from one status to another, and only from it.
func (s *Store) SetServerStatus(ctx context.Context, id, from, to string) error {
	_, err := s.pool.Exec(ctx, `UPDATE servers SET status = $3 WHERE id = $1 AND status = $2`, id, from, to)
	return err
}
