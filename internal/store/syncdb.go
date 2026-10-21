package store

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed sync_schema.sql
var syncSchema string

type querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type SyncPeer struct {
	URL       string
	LastOK    *time.Time
	LastError string
}

func (s *Store) ensureInstance(ctx context.Context) error {
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT instance_id FROM sync_meta WHERE singleton`).Scan(&id); err != nil {
		return err
	}
	if id != "" {
		return nil
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE sync_meta SET instance_id = $1 WHERE singleton AND instance_id = ''`, hex.EncodeToString(buf))
	return err
}

func (s *Store) InstanceID(ctx context.Context) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT instance_id FROM sync_meta WHERE singleton`).Scan(&id)
	return id, err
}

func NormalizePeerURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("adresse")
	}
	if !peerHostAllowed(parsed.Scheme, parsed.Hostname()) {
		return "", errors.New("adresse")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func peerHostAllowed(scheme, host string) bool {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" || strings.Contains(host, "%") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if !PeerIPAllowed(ip) {
			return false
		}
		if scheme == "http" && !ip.IsLoopback() && !ip.IsPrivate() {
			return false
		}
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	lower := strings.ToLower(host)
	if lower == "metadata.google.internal" || strings.HasSuffix(lower, ".metadata.google.internal") {
		return false
	}
	return scheme == "https"
}

func PeerIPAllowed(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsGlobalUnicast()
}

func (s *Store) AddPeer(ctx context.Context, raw string) error {
	peer, err := NormalizePeerURL(raw)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO sync_peers (url) VALUES ($1) ON CONFLICT (url) DO NOTHING`, peer)
	return err
}

func (s *Store) DeletePeer(ctx context.Context, raw string) error {
	peer, err := NormalizePeerURL(raw)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM sync_peers WHERE url = $1`, peer)
	return err
}

func (s *Store) ListPeers(ctx context.Context) ([]SyncPeer, error) {
	rows, err := s.pool.Query(ctx, `SELECT url, last_ok, last_error FROM sync_peers ORDER BY url`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncPeer
	for rows.Next() {
		var row SyncPeer
		if err := rows.Scan(&row.URL, &row.LastOK, &row.LastError); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if out == nil {
		out = []SyncPeer{}
	}
	return out, rows.Err()
}

func (s *Store) MarkPeer(ctx context.Context, raw, problem string) error {
	peer, err := NormalizePeerURL(raw)
	if err != nil {
		return err
	}
	if problem == "" {
		_, err = s.pool.Exec(ctx, `UPDATE sync_peers SET last_ok = now(), last_error = '' WHERE url = $1`, peer)
		return err
	}
	if len(problem) > 300 {
		problem = problem[:300]
	}
	_, err = s.pool.Exec(ctx, `UPDATE sync_peers SET last_error = $2 WHERE url = $1`, peer, problem)
	return err
}

func (s *Store) SyncExport(ctx context.Context) (SyncDoc, error) {
	if err := stampSync(ctx, s.pool); err != nil {
		return SyncDoc{}, err
	}
	return loadSync(ctx, s.pool)
}

func (s *Store) SyncMerge(ctx context.Context, remote SyncDoc) (SyncDoc, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SyncDoc{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '15s'`); err != nil {
		return SyncDoc{}, err
	}
	if _, err := tx.Exec(ctx, `
		LOCK TABLE users, secrets, clusters, servers, server_keys, applications, application_services,
			domains, templates, destructions, template_runs, updates, update_jobs, image_rollbacks, app_secrets, sync_tomb, sync_meta
		IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return SyncDoc{}, err
	}
	if err := stampSync(ctx, tx); err != nil {
		return SyncDoc{}, err
	}
	local, err := loadSync(ctx, tx)
	if err != nil {
		return SyncDoc{}, err
	}
	merged := mergeDocs(local, remote)
	if _, err := tx.Exec(ctx, `SELECT set_config('myinfra.apply', '1', true)`); err != nil {
		return SyncDoc{}, err
	}
	if err := replaceSync(ctx, tx, merged); err != nil {
		return SyncDoc{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE sync_meta SET seq = GREATEST(seq, $1) WHERE singleton`, maxSeq(merged)); err != nil {
		return SyncDoc{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SyncDoc{}, err
	}
	return merged, nil
}

func stampSync(ctx context.Context, q querier) error {
	for _, stmt := range []string{
		`UPDATE users SET id = id WHERE sync_seq = 0`,
		`UPDATE secrets SET id = id WHERE sync_seq = 0`,
		`UPDATE clusters SET id = id WHERE sync_seq = 0`,
		`UPDATE servers SET id = id WHERE sync_seq = 0`,
		`UPDATE server_keys SET server_id = server_id WHERE sync_seq = 0`,
		`UPDATE applications SET id = id WHERE sync_seq = 0`,
		`UPDATE application_services SET id = id WHERE sync_seq = 0`,
		`UPDATE domains SET id = id WHERE sync_seq = 0`,
		`UPDATE templates SET id = id WHERE sync_seq = 0`,
		`UPDATE destructions SET id = id WHERE sync_seq = 0`,
		`UPDATE template_runs SET id = id WHERE sync_seq = 0`,
		`UPDATE updates SET kind = kind WHERE sync_seq = 0`,
		`UPDATE update_jobs SET id = id WHERE sync_seq = 0`,
		`UPDATE image_rollbacks SET application_id = application_id WHERE sync_seq = 0`,
		`UPDATE app_secrets SET id = id WHERE sync_seq = 0`,
	} {
		if _, err := q.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func loadSync(ctx context.Context, q querier) (SyncDoc, error) {
	var doc SyncDoc
	if err := loadUsers(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadSecrets(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadClusters(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadServers(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadKeys(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadApps(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadServices(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadDomains(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadTemplates(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadDestructions(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadRuns(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadUpdates(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadUpdateJobs(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadRollbacks(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	if err := loadAppSecrets(ctx, q, &doc); err != nil {
		return SyncDoc{}, err
	}
	rows, err := q.Query(ctx, `SELECT tbl, row_id, seq, origin, payload FROM sync_tomb`)
	if err != nil {
		return SyncDoc{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var tb syncTomb
		var payload []byte
		if err := rows.Scan(&tb.Table, &tb.ID, &tb.Seq, &tb.Origin, &payload); err != nil {
			return SyncDoc{}, err
		}
		tb.Payload = payload
		doc.Tombs = append(doc.Tombs, tb)
	}
	if err := rows.Err(); err != nil {
		return SyncDoc{}, err
	}
	return doc.canon(), nil
}

func loadUsers(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `SELECT id, password_hash, created_at, sync_seq, sync_origin FROM users`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncUser
		if err := rows.Scan(&row.ID, &row.PasswordHash, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		doc.Users = append(doc.Users, row)
	}
	return rows.Err()
}

func loadSecrets(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `
		SELECT id, owner_id, kind, name, provider, fingerprint, public_key, hint, key_id, has_private,
			ciphertext, nonce, wrapped_dek, created_at, sync_seq, sync_origin
		FROM secrets`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncSecret
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Kind, &row.Name, &row.Provider, &row.Fingerprint, &row.PublicKey, &row.Hint, &row.KeyID, &row.HasPrivate, &row.Ciphertext, &row.Nonce, &row.WrappedDEK, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		doc.Secrets = append(doc.Secrets, row)
	}
	return rows.Err()
}

func loadClusters(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `
		SELECT id, owner_id, name, origin, kube_ciphertext, kube_nonce, kube_wrapped, created_at, sync_seq, sync_origin
		FROM clusters`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncCluster
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Name, &row.OriginCluster, &row.KubeCiphertext, &row.KubeNonce, &row.KubeWrapped, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		doc.Clusters = append(doc.Clusters, row)
	}
	return rows.Err()
}

func loadServers(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `
		SELECT id, owner_id, cluster_id, name, origin, status, address, join_token, provider_id, provider_server_id,
			plan, deploy, control_plane, wg_index, created_at, sync_seq, sync_origin
		FROM servers`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncServer
		var plan, deploy []byte
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.ClusterID, &row.Name, &row.OriginServer, &row.Status, &row.Address, &row.JoinToken, &row.ProviderID, &row.ProviderServerID, &plan, &deploy, &row.ControlPlane, &row.WGIndex, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		row.Plan, row.Deploy = plan, deploy
		doc.Servers = append(doc.Servers, row)
	}
	return rows.Err()
}

func loadKeys(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `SELECT server_id, secret_id, sync_seq, sync_origin FROM server_keys`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncKey
		if err := rows.Scan(&row.ServerID, &row.SecretID, &row.Seq, &row.Origin); err != nil {
			return err
		}
		doc.Keys = append(doc.Keys, row)
	}
	return rows.Err()
}

func loadApps(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `
		SELECT id, owner_id, name, source_kind, source, destination_kind, server_id, cluster_id, replicas, variables,
			run_status, paused, rollout, rollout_detail, preparing_since, created_at, sync_seq, sync_origin
		FROM applications`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncApp
		var variables []byte
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Name, &row.SourceKind, &row.Source, &row.DestinationKind, &row.ServerID, &row.ClusterID, &row.Replicas, &variables, &row.RunStatus, &row.Paused, &row.Rollout, &row.RolloutDetail, &row.PreparingSince, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		row.Variables = variables
		doc.Applications = append(doc.Applications, row)
	}
	return rows.Err()
}

func loadServices(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `
		SELECT id, application_id, name, image, published, disk, replicas, listen, disk_path, disk_gi, args, sync_seq, sync_origin
		FROM application_services`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncService
		if err := rows.Scan(&row.ID, &row.ApplicationID, &row.Name, &row.Image, &row.Published, &row.Disk, &row.Replicas, &row.Listen, &row.DiskPath, &row.DiskGi, &row.Args, &row.Seq, &row.Origin); err != nil {
			return err
		}
		doc.Services = append(doc.Services, row)
	}
	return rows.Err()
}

func loadDomains(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `
		SELECT id, application_id, hostname, kind, status, zone, secret_id, detail, created_at, sync_seq, sync_origin
		FROM domains`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncDomain
		if err := rows.Scan(&row.ID, &row.ApplicationID, &row.Hostname, &row.Kind, &row.Status, &row.Zone, &row.SecretID, &row.Detail, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		doc.Domains = append(doc.Domains, row)
	}
	return rows.Err()
}

func loadTemplates(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `SELECT id, owner_id, name, body, created_at, sync_seq, sync_origin FROM templates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncTemplate
		var body []byte
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Name, &body, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		row.Body = body
		doc.Templates = append(doc.Templates, row)
	}
	return rows.Err()
}

func loadDestructions(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `SELECT id, owner_id, kind, target_id, name, status, body, created_at, sync_seq, sync_origin FROM destructions`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncDestruction
		var body []byte
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Kind, &row.TargetID, &row.Name, &row.Status, &body, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		row.Body = body
		doc.Destructions = append(doc.Destructions, row)
	}
	return rows.Err()
}

func loadRuns(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `SELECT id, owner_id, template_id, status, cluster_id, body, created_at, sync_seq, sync_origin FROM template_runs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncRun
		var body []byte
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.TemplateID, &row.Status, &row.ClusterID, &body, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		row.Body = body
		doc.Runs = append(doc.Runs, row)
	}
	return rows.Err()
}

func loadUpdates(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `
		SELECT kind, target_id, owner_id, current, available, level, detail, checked_at, sync_seq, sync_origin
		FROM updates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncUpdate
		var detail []byte
		if err := rows.Scan(&row.Kind, &row.TargetID, &row.OwnerID, &row.Current, &row.Available, &row.Level, &detail, &row.CheckedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		row.Detail = detail
		doc.Updates = append(doc.Updates, row)
	}
	return rows.Err()
}

func loadUpdateJobs(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `
		SELECT id, owner_id, cluster_id, server_id, status, steps, created_at, finished_at, sync_seq, sync_origin
		FROM update_jobs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncUpdateJob
		var steps []byte
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.ClusterID, &row.ServerID, &row.Status, &steps, &row.CreatedAt, &row.FinishedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		row.Steps = steps
		doc.UpdateJobs = append(doc.UpdateJobs, row)
	}
	return rows.Err()
}

func loadRollbacks(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `SELECT application_id, images, run_status, started_at, sync_seq, sync_origin FROM image_rollbacks`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncRollback
		var images []byte
		if err := rows.Scan(&row.ApplicationID, &images, &row.RunStatus, &row.StartedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		row.Images = images
		doc.Rollbacks = append(doc.Rollbacks, row)
	}
	return rows.Err()
}

func loadAppSecrets(ctx context.Context, q querier, doc *SyncDoc) error {
	rows, err := q.Query(ctx, `
		SELECT id, owner_id, name, ciphertext, nonce, wrapped_dek, apps, created_at, sync_seq, sync_origin
		FROM app_secrets`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row syncAppSecret
		var apps []byte
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Name, &row.Ciphertext, &row.Nonce, &row.WrappedDEK, &apps, &row.CreatedAt, &row.Seq, &row.Origin); err != nil {
			return err
		}
		row.Apps = apps
		doc.AppSecrets = append(doc.AppSecrets, row)
	}
	return rows.Err()
}

func replaceSync(ctx context.Context, q querier, doc SyncDoc) error {
	deletes := []struct {
		sql string
		ids []string
	}{
		{`DELETE FROM image_rollbacks WHERE NOT (application_id = ANY($1::text[]))`, idsOf(doc.Rollbacks, func(r syncRollback) string { return r.ApplicationID })},
		{`DELETE FROM domains WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Domains, func(r syncDomain) string { return r.ID })},
		{`DELETE FROM application_services WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Services, func(r syncService) string { return r.ID })},
		{`DELETE FROM template_runs WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Runs, func(r syncRun) string { return r.ID })},
		{`DELETE FROM update_jobs WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.UpdateJobs, func(r syncUpdateJob) string { return r.ID })},
		{`DELETE FROM updates WHERE NOT ((kind || '/' || target_id) = ANY($1::text[]))`, idsOf(doc.Updates, func(r syncUpdate) string { return r.ref() })},
		{`DELETE FROM destructions WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Destructions, func(r syncDestruction) string { return r.ID })},
		{`DELETE FROM applications WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Applications, func(r syncApp) string { return r.ID })},
		{`DELETE FROM server_keys WHERE NOT ((server_id || '/' || secret_id) = ANY($1::text[]))`, idsOf(doc.Keys, func(r syncKey) string { return r.ref() })},
		{`DELETE FROM templates WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Templates, func(r syncTemplate) string { return r.ID })},
		{`DELETE FROM servers WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Servers, func(r syncServer) string { return r.ID })},
		{`DELETE FROM clusters WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Clusters, func(r syncCluster) string { return r.ID })},
		{`DELETE FROM secrets WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Secrets, func(r syncSecret) string { return r.ID })},
		{`DELETE FROM app_secrets WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.AppSecrets, func(r syncAppSecret) string { return r.ID })},
		{`DELETE FROM users WHERE NOT (id = ANY($1::text[]))`, idsOf(doc.Users, func(r syncUser) string { return r.ID })},
	}
	for _, step := range deletes {
		if _, err := q.Exec(ctx, step.sql, step.ids); err != nil {
			return err
		}
	}
	for _, row := range doc.Users {
		if _, err := q.Exec(ctx, `
			INSERT INTO users (id, password_hash, created_at, sync_seq, sync_origin) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash, created_at = EXCLUDED.created_at, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.PasswordHash, row.CreatedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Secrets {
		if _, err := q.Exec(ctx, `
			INSERT INTO secrets (
				id, owner_id, kind, name, provider, fingerprint, public_key, hint, key_id, has_private,
				ciphertext, nonce, wrapped_dek, created_at, sync_seq, sync_origin
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT (id) DO UPDATE SET
				owner_id = EXCLUDED.owner_id, kind = EXCLUDED.kind, name = EXCLUDED.name, provider = EXCLUDED.provider,
				fingerprint = EXCLUDED.fingerprint, public_key = EXCLUDED.public_key, hint = EXCLUDED.hint, key_id = EXCLUDED.key_id,
				has_private = EXCLUDED.has_private, ciphertext = EXCLUDED.ciphertext, nonce = EXCLUDED.nonce, wrapped_dek = EXCLUDED.wrapped_dek,
				created_at = EXCLUDED.created_at, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.OwnerID, row.Kind, row.Name, row.Provider, row.Fingerprint, row.PublicKey, row.Hint, row.KeyID, row.HasPrivate,
			filled(row.Ciphertext), filled(row.Nonce), filled(row.WrappedDEK), row.CreatedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Clusters {
		origin := row.OriginCluster
		if origin == "" {
			origin = "created"
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO clusters (id, owner_id, name, origin, kube_ciphertext, kube_nonce, kube_wrapped, created_at, sync_seq, sync_origin)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (id) DO UPDATE SET
				owner_id = EXCLUDED.owner_id, name = EXCLUDED.name, origin = EXCLUDED.origin,
				kube_ciphertext = EXCLUDED.kube_ciphertext, kube_nonce = EXCLUDED.kube_nonce, kube_wrapped = EXCLUDED.kube_wrapped,
				created_at = EXCLUDED.created_at, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.OwnerID, row.Name, origin, row.KubeCiphertext, row.KubeNonce, row.KubeWrapped, row.CreatedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Servers {
		if _, err := q.Exec(ctx, `
			INSERT INTO servers (
				id, owner_id, cluster_id, name, origin, status, address, join_token, provider_id, provider_server_id,
				plan, deploy, control_plane, created_at, sync_seq, sync_origin, wg_index
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			ON CONFLICT (id) DO UPDATE SET
				owner_id = EXCLUDED.owner_id, cluster_id = EXCLUDED.cluster_id, name = EXCLUDED.name, origin = EXCLUDED.origin,
				status = EXCLUDED.status, address = EXCLUDED.address, join_token = EXCLUDED.join_token, provider_id = EXCLUDED.provider_id,
				provider_server_id = EXCLUDED.provider_server_id, plan = EXCLUDED.plan, deploy = EXCLUDED.deploy,
				control_plane = EXCLUDED.control_plane, created_at = EXCLUDED.created_at, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin,
				wg_index = EXCLUDED.wg_index`,
			row.ID, row.OwnerID, row.ClusterID, row.Name, row.OriginServer, row.Status, row.Address, row.JoinToken, row.ProviderID, row.ProviderServerID,
			string(row.Plan), string(row.Deploy), row.ControlPlane, row.CreatedAt, row.Seq, row.Origin, row.WGIndex); err != nil {
			return err
		}
	}
	for _, row := range doc.Keys {
		if _, err := q.Exec(ctx, `
			INSERT INTO server_keys (server_id, secret_id, sync_seq, sync_origin) VALUES ($1,$2,$3,$4)
			ON CONFLICT (server_id, secret_id) DO UPDATE SET sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ServerID, row.SecretID, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Templates {
		if _, err := q.Exec(ctx, `
			INSERT INTO templates (id, owner_id, name, body, created_at, sync_seq, sync_origin) VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.owner_id, name = EXCLUDED.name, body = EXCLUDED.body, created_at = EXCLUDED.created_at, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.OwnerID, row.Name, string(row.Body), row.CreatedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Applications {
		if _, err := q.Exec(ctx, `
			INSERT INTO applications (
				id, owner_id, name, source_kind, source, destination_kind, server_id, cluster_id, replicas, variables,
				run_status, paused, rollout, rollout_detail, preparing_since, created_at, sync_seq, sync_origin
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
			ON CONFLICT (id) DO UPDATE SET
				owner_id = EXCLUDED.owner_id, name = EXCLUDED.name, source_kind = EXCLUDED.source_kind, source = EXCLUDED.source,
				destination_kind = EXCLUDED.destination_kind, server_id = EXCLUDED.server_id, cluster_id = EXCLUDED.cluster_id,
				replicas = EXCLUDED.replicas, variables = EXCLUDED.variables, run_status = EXCLUDED.run_status, paused = EXCLUDED.paused,
				rollout = EXCLUDED.rollout, rollout_detail = EXCLUDED.rollout_detail, preparing_since = EXCLUDED.preparing_since,
				created_at = EXCLUDED.created_at, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.OwnerID, row.Name, row.SourceKind, row.Source, row.DestinationKind, row.ServerID, row.ClusterID, row.Replicas, string(row.Variables),
			row.RunStatus, row.Paused, row.Rollout, row.RolloutDetail, row.PreparingSince, row.CreatedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Services {
		if _, err := q.Exec(ctx, `
			INSERT INTO application_services (
				id, application_id, name, image, published, disk, replicas, listen, disk_path, disk_gi, args, sync_seq, sync_origin
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (id) DO UPDATE SET
				application_id = EXCLUDED.application_id, name = EXCLUDED.name, image = EXCLUDED.image, published = EXCLUDED.published,
				disk = EXCLUDED.disk, replicas = EXCLUDED.replicas, listen = EXCLUDED.listen, disk_path = EXCLUDED.disk_path,
				disk_gi = EXCLUDED.disk_gi, args = EXCLUDED.args, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.ApplicationID, row.Name, row.Image, row.Published, row.Disk, row.Replicas, row.Listen, row.DiskPath, row.DiskGi, row.Args, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Domains {
		if _, err := q.Exec(ctx, `
			INSERT INTO domains (id, application_id, hostname, kind, status, zone, secret_id, detail, created_at, sync_seq, sync_origin)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (id) DO UPDATE SET
				application_id = EXCLUDED.application_id, hostname = EXCLUDED.hostname, kind = EXCLUDED.kind, status = EXCLUDED.status,
				zone = EXCLUDED.zone, secret_id = EXCLUDED.secret_id, detail = EXCLUDED.detail, created_at = EXCLUDED.created_at,
				sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.ApplicationID, row.Hostname, row.Kind, row.Status, row.Zone, row.SecretID, row.Detail, row.CreatedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Destructions {
		if _, err := q.Exec(ctx, `
			INSERT INTO destructions (id, owner_id, kind, target_id, name, status, body, created_at, sync_seq, sync_origin)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (id) DO UPDATE SET
				owner_id = EXCLUDED.owner_id, kind = EXCLUDED.kind, target_id = EXCLUDED.target_id, name = EXCLUDED.name,
				status = EXCLUDED.status, body = EXCLUDED.body, created_at = EXCLUDED.created_at, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.OwnerID, row.Kind, row.TargetID, row.Name, row.Status, string(row.Body), row.CreatedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Runs {
		if _, err := q.Exec(ctx, `
			INSERT INTO template_runs (id, owner_id, template_id, status, cluster_id, body, created_at, sync_seq, sync_origin)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (id) DO UPDATE SET
				owner_id = EXCLUDED.owner_id, template_id = EXCLUDED.template_id, status = EXCLUDED.status, cluster_id = EXCLUDED.cluster_id,
				body = EXCLUDED.body, created_at = EXCLUDED.created_at, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.OwnerID, row.TemplateID, row.Status, row.ClusterID, string(row.Body), row.CreatedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Updates {
		detail := row.Detail
		if len(detail) == 0 {
			detail = json.RawMessage(`{}`)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO updates (kind, target_id, owner_id, current, available, level, detail, checked_at, sync_seq, sync_origin)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (kind, target_id) DO UPDATE SET
				owner_id = EXCLUDED.owner_id, current = EXCLUDED.current, available = EXCLUDED.available, level = EXCLUDED.level,
				detail = EXCLUDED.detail, checked_at = EXCLUDED.checked_at, sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.Kind, row.TargetID, row.OwnerID, row.Current, row.Available, row.Level, string(detail), row.CheckedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.UpdateJobs {
		steps := row.Steps
		if len(steps) == 0 {
			steps = []byte("[]")
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO update_jobs (id, owner_id, cluster_id, server_id, status, steps, created_at, finished_at, sync_seq, sync_origin)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (id) DO UPDATE SET
				owner_id = EXCLUDED.owner_id, cluster_id = EXCLUDED.cluster_id, server_id = EXCLUDED.server_id, status = EXCLUDED.status,
				steps = EXCLUDED.steps, created_at = EXCLUDED.created_at, finished_at = EXCLUDED.finished_at,
				sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.OwnerID, row.ClusterID, row.ServerID, row.Status, steps, row.CreatedAt, row.FinishedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.Rollbacks {
		if _, err := q.Exec(ctx, `
			INSERT INTO image_rollbacks (application_id, images, run_status, started_at, sync_seq, sync_origin)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (application_id) DO UPDATE SET
				images = EXCLUDED.images, run_status = EXCLUDED.run_status, started_at = EXCLUDED.started_at,
				sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ApplicationID, string(row.Images), row.RunStatus, row.StartedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	for _, row := range doc.AppSecrets {
		apps := row.Apps
		if len(apps) == 0 {
			apps = json.RawMessage(`[]`)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO app_secrets (id, owner_id, name, ciphertext, nonce, wrapped_dek, apps, created_at, sync_seq, sync_origin)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (id) DO UPDATE SET
				owner_id = EXCLUDED.owner_id, name = EXCLUDED.name, ciphertext = EXCLUDED.ciphertext, nonce = EXCLUDED.nonce,
				wrapped_dek = EXCLUDED.wrapped_dek, apps = EXCLUDED.apps, created_at = EXCLUDED.created_at,
				sync_seq = EXCLUDED.sync_seq, sync_origin = EXCLUDED.sync_origin`,
			row.ID, row.OwnerID, row.Name, row.Ciphertext, row.Nonce, row.WrappedDEK, []byte(apps), row.CreatedAt, row.Seq, row.Origin); err != nil {
			return err
		}
	}
	if _, err := q.Exec(ctx, `DELETE FROM sync_tomb`); err != nil {
		return err
	}
	for _, tb := range doc.Tombs {
		payload := tb.Payload
		if len(payload) == 0 {
			payload = json.RawMessage(`null`)
		}
		if _, err := q.Exec(ctx, `INSERT INTO sync_tomb (tbl, row_id, seq, origin, payload) VALUES ($1,$2,$3,$4,$5)`, tb.Table, tb.ID, tb.Seq, tb.Origin, []byte(payload)); err != nil {
			return err
		}
	}
	return nil
}
