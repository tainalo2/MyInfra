package store

import (
	"context"
	"time"
)

type Event struct {
	ID          string
	OwnerID     string
	At          time.Time
	Kind        string
	Level       string
	Message     string
	ServerID    string
	ClusterID   string
	ServerName  string
	ClusterName string
}

func (s *Store) AddEvent(ctx context.Context, event Event) error {
	if event.At.IsZero() {
		event.At = time.Now()
	}
	if event.Level == "" {
		event.Level = "info"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO events (id, owner_id, at, kind, level, message, server_id, cluster_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING`,
		event.ID, event.OwnerID, event.At, event.Kind, event.Level, event.Message, event.ServerID, event.ClusterID)
	return err
}

func (s *Store) BackfillEvents(ctx context.Context, ownerID string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO events (id, owner_id, at, kind, level, message, server_id, cluster_id)
		SELECT 'opened-' || id, owner_id, created_at, 'server.opened', 'info', 'Serveur ouvert.', id, COALESCE(cluster_id, '')
		FROM servers WHERE owner_id = $1
		ON CONFLICT (id) DO NOTHING`, ownerID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO events (id, owner_id, at, kind, level, message, server_id, cluster_id)
		SELECT 'opened-cluster-' || id, owner_id, created_at, 'cluster.opened', 'info',
		       CASE WHEN origin = 'imported' THEN 'Cluster importé.' ELSE 'Cluster créé.' END,
		       '', id
		FROM clusters WHERE owner_id = $1
		ON CONFLICT (id) DO NOTHING`, ownerID)
	return err
}

func (s *Store) ListEvents(ctx context.Context, ownerID, serverID, clusterID string) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.at, e.kind, e.level, e.message, e.server_id, COALESCE(s.name, ''), e.cluster_id, COALESCE(c.name, '')
		FROM events e
		LEFT JOIN servers s ON s.id = e.server_id AND s.owner_id = e.owner_id
		LEFT JOIN clusters c ON c.id = e.cluster_id AND c.owner_id = e.owner_id
		WHERE e.owner_id = $1
		  AND ($2 = '' OR e.server_id = $2)
		  AND ($3 = '' OR e.cluster_id = $3 OR e.server_id IN (SELECT id FROM servers WHERE owner_id = $1 AND cluster_id = $3))
		ORDER BY e.at DESC
		LIMIT 200`, ownerID, serverID, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.At, &event.Kind, &event.Level, &event.Message, &event.ServerID, &event.ServerName, &event.ClusterID, &event.ClusterName); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
