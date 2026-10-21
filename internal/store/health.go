package store

import (
	"context"
	"time"
)

// WatchedServer is a server whose reachability this copy follows.
type WatchedServer struct {
	ID               string
	OwnerID          string
	Name             string
	Status           string
	Address          string
	ClusterID        string
	ProviderID       string
	Provider         string
	ProviderServerID string
}

func (s *Store) WatchedServers(ctx context.Context) ([]WatchedServer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.owner_id, s.name, s.status, s.address, COALESCE(s.cluster_id, ''),
		       s.provider_id, COALESCE(sec.provider, ''), s.provider_server_id
		FROM servers s
		LEFT JOIN secrets sec ON sec.id = s.provider_id AND sec.owner_id = s.owner_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchedServer
	for rows.Next() {
		var row WatchedServer
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Name, &row.Status, &row.Address, &row.ClusterID,
			&row.ProviderID, &row.Provider, &row.ProviderServerID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ServerHealth is what this copy saw of a server lately.
type ServerHealth struct {
	SeenAt            *time.Time
	LostSince         *time.Time
	Fails             int
	ProviderState     string
	ProviderCheckedAt *time.Time
	ProviderError     string
}

func (s *Store) ServerHealths(ctx context.Context) (map[string]ServerHealth, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT server_id, seen_at, lost_since, fails, provider_state, provider_checked_at, provider_error
		FROM server_health`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ServerHealth{}
	for rows.Next() {
		var id string
		var row ServerHealth
		if err := rows.Scan(&id, &row.SeenAt, &row.LostSince, &row.Fails, &row.ProviderState, &row.ProviderCheckedAt, &row.ProviderError); err != nil {
			return nil, err
		}
		out[id] = row
	}
	return out, rows.Err()
}

// MarkReach records one probe of a server: answered, or not.
func (s *Store) MarkReach(ctx context.Context, id string, answered bool) error {
	if answered {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO server_health (server_id, seen_at, lost_since, fails) VALUES ($1, now(), NULL, 0)
			ON CONFLICT (server_id) DO UPDATE SET seen_at = now(), lost_since = NULL, fails = 0`, id)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO server_health (server_id, lost_since, fails) VALUES ($1, now(), 1)
		ON CONFLICT (server_id) DO UPDATE SET lost_since = COALESCE(server_health.lost_since, now()),
			fails = server_health.fails + 1`, id)
	return err
}

// MarkProvider records what the provider said of a server: present, gone,
// or, with a problem, nothing new.
func (s *Store) MarkProvider(ctx context.Context, id, state, problem string) error {
	if problem != "" {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO server_health (server_id, provider_checked_at, provider_error) VALUES ($1, now(), $2)
			ON CONFLICT (server_id) DO UPDATE SET provider_checked_at = now(), provider_error = $2`, id, problem)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO server_health (server_id, provider_state, provider_checked_at) VALUES ($1, $2, now())
		ON CONFLICT (server_id) DO UPDATE SET provider_state = $2, provider_checked_at = now(), provider_error = ''`, id, state)
	return err
}

// PruneHealth drops what is known of servers that are gone from the list.
func (s *Store) PruneHealth(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM server_health WHERE server_id NOT IN (SELECT id FROM servers)`)
	return err
}
