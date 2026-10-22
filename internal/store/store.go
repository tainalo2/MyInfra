package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound     = errors.New("introuvable")
	ErrHasApps      = errors.New("applications")
	ErrInCluster    = errors.New("in cluster")
	ErrLinked       = errors.New("déjà reliée")
	ErrProviderBusy = errors.New("provider occupé")
	ErrNameTaken    = errors.New("nom pris")
	ErrStopped      = errors.New("arrêté")
	ErrSetupDone    = errors.New("déjà initialisé")
)

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() { s.pool.Close() }

// migrateLock is held while the schema changes: during an update, two copies
// of MyInfra start at the same time against the same database.
const migrateLock = 842015

func (s *Store) migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLock); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrateLock)
	if err := execScript(ctx, conn, schema); err != nil {
		return err
	}
	if err := execScript(ctx, conn, syncSchema); err != nil {
		return err
	}
	return s.ensureInstance(ctx)
}

func execScript(ctx context.Context, conn *pgxpool.Conn, script string) error {
	for _, stmt := range splitSQL(script) {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			line := stmt
			if len(line) > 120 {
				line = line[:120]
			}
			return fmt.Errorf("%s: %w", line, err)
		}
	}
	return nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, id, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO users (id, password_hash) VALUES ($1, $2)`, id, passwordHash)
	return err
}

func (s *Store) CreateFirstUser(ctx context.Context, id, passwordHash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(842014)`); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrSetupDone
	}
	if _, err := tx.Exec(ctx, `INSERT INTO users (id, password_hash, login, role) VALUES ($1, $2, 'admin', 'admin')`, id, passwordHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (s *Store) SSHHostKey(ctx context.Context, address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", nil
	}
	var key string
	err := s.pool.QueryRow(ctx, `
		SELECT ssh_host_key FROM servers
		WHERE address = $1 AND ssh_host_key <> ''
		ORDER BY created_at LIMIT 1`, address).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return key, err
}

func (s *Store) SaveSSHHostKey(ctx context.Context, address, key string) error {
	address = strings.TrimSpace(address)
	key = strings.TrimSpace(key)
	if address == "" || key == "" || strings.ContainsAny(key, "\r\n") {
		return errors.New("clé d'hôte illisible")
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE servers SET ssh_host_key = $2
		WHERE address = $1 AND ssh_host_key = ''`, address, key); err != nil {
		return err
	}
	var current string
	err := s.pool.QueryRow(ctx, `
		SELECT ssh_host_key FROM servers
		WHERE address = $1 AND ssh_host_key <> ''
		LIMIT 1`, address).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != key {
		return errors.New("clé d'hôte différente")
	}
	return nil
}

func (s *Store) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Onboarded(ctx context.Context, userID string) (bool, error) {
	var done bool
	err := s.pool.QueryRow(ctx, `SELECT onboarded FROM users WHERE id = $1`, userID).Scan(&done)
	return done, err
}

func (s *Store) FinishOnboarding(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET onboarded = true WHERE id = $1`, userID)
	return err
}

func (s *Store) User(ctx context.Context) (string, string, error) {
	var id, hash string
	err := s.pool.QueryRow(ctx, `SELECT id, password_hash FROM users LIMIT 1`).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return id, hash, err
}

func (s *Store) CreateSession(ctx context.Context, tokenHash, userID string, expires time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`, tokenHash, userID, expires)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *Store) CreateLoginChallenge(ctx context.Context, tokenHash, userID string, expires time.Time) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM login_challenges WHERE expires_at <= now()`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO login_challenges (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`, tokenHash, userID, expires)
	return err
}

func (s *Store) TakeLoginChallenge(ctx context.Context, tokenHash string) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx, `
		DELETE FROM login_challenges
		WHERE token_hash = $1 AND expires_at > now()
		RETURNING user_id`, tokenHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return userID, err
}

func (s *Store) UserBySession(ctx context.Context, tokenHash string) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM sessions WHERE token_hash = $1 AND expires_at > now()`, tokenHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return userID, err
}

type Secret struct {
	ID          string
	OwnerID     string
	Kind        string
	Name        string
	Provider    string
	Fingerprint string
	PublicKey   string
	Hint        string
	KeyID       string
	HasPrivate  bool
	Ciphertext  []byte
	Nonce       []byte
	WrappedDEK  []byte
	CreatedAt   time.Time
}

func (s *Store) InsertSecret(ctx context.Context, sec Secret) error {
	var taken bool
	err := s.pool.QueryRow(ctx, `
		SELECT exists(
			SELECT 1 FROM secrets
			WHERE owner_id = $1 AND kind = $2 AND lower(btrim(name)) = lower(btrim($3))
		)`, sec.OwnerID, sec.Kind, sec.Name).Scan(&taken)
	if err != nil {
		return err
	}
	if taken {
		return ErrNameTaken
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO secrets (
			id, owner_id, kind, name, provider, fingerprint, public_key, hint, key_id, has_private,
			ciphertext, nonce, wrapped_dek
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		sec.ID, sec.OwnerID, sec.Kind, sec.Name, sec.Provider, sec.Fingerprint, sec.PublicKey, sec.Hint, sec.KeyID, sec.HasPrivate,
		sec.Ciphertext, sec.Nonce, sec.WrappedDEK,
	)
	return nameTaken(err)
}

func (s *Store) ListSecrets(ctx context.Context, ownerID string) ([]Secret, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, name, provider, fingerprint, public_key, hint, key_id, has_private, created_at
		FROM secrets WHERE owner_id = $1 ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Secret
	for rows.Next() {
		var sec Secret
		if err := rows.Scan(&sec.ID, &sec.Kind, &sec.Name, &sec.Provider, &sec.Fingerprint, &sec.PublicKey, &sec.Hint, &sec.KeyID, &sec.HasPrivate, &sec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

// SecretsOfKind are the secrets of one kind, sealed, whoever holds them: the
// logins of the registries are read by the deliveries.
func (s *Store) SecretsOfKind(ctx context.Context, kind string) ([]Secret, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, kind, name, provider, hint, key_id, ciphertext, nonce, wrapped_dek, created_at
		FROM secrets WHERE kind = $1 ORDER BY created_at`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Secret
	for rows.Next() {
		var sec Secret
		if err := rows.Scan(&sec.ID, &sec.OwnerID, &sec.Kind, &sec.Name, &sec.Provider, &sec.Hint, &sec.KeyID, &sec.Ciphertext, &sec.Nonce, &sec.WrappedDEK, &sec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

func (s *Store) Secret(ctx context.Context, ownerID, id string) (Secret, error) {
	var sec Secret
	err := s.pool.QueryRow(ctx, `
		SELECT id, kind, name, provider, fingerprint, public_key, hint, key_id, has_private, ciphertext, nonce, wrapped_dek, created_at
		FROM secrets WHERE owner_id = $1 AND id = $2`, ownerID, id).Scan(
		&sec.ID, &sec.Kind, &sec.Name, &sec.Provider, &sec.Fingerprint, &sec.PublicKey, &sec.Hint, &sec.KeyID, &sec.HasPrivate,
		&sec.Ciphertext, &sec.Nonce, &sec.WrappedDEK, &sec.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Secret{}, ErrNotFound
	}
	sec.OwnerID = ownerID
	return sec, err
}

func (s *Store) UpdateSecret(ctx context.Context, sec Secret) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE secrets
		SET ciphertext = $3, nonce = $4, wrapped_dek = $5, hint = $6, key_id = $7
		WHERE owner_id = $1 AND id = $2`,
		sec.OwnerID, sec.ID, sec.Ciphertext, sec.Nonce, sec.WrappedDEK, sec.Hint, sec.KeyID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSecret(ctx context.Context, ownerID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM secrets WHERE owner_id = $1 AND id = $2`, ownerID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SSHPublicKeys(ctx context.Context, ownerID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT public_key FROM secrets
		WHERE owner_id = $1 AND kind = 'ssh_key' AND id = ANY($2::text[])`, ownerID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(ids) {
		return nil, ErrNotFound
	}
	return out, nil
}

func (s *Store) ServerKeyIDs(ctx context.Context, serverID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT secret_id FROM server_keys WHERE server_id = $1`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) ClaimDeploy(ctx context.Context, ownerID, id string, steps []byte) error {
	if len(steps) == 0 {
		steps = []byte("[]")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE servers SET status = 'deploying', deploy = $3
		WHERE id = $1 AND owner_id = $2`,
		id, ownerID, string(steps))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type Server struct {
	ID               string
	Name             string
	Origin           string
	Status           string
	Address          string
	ClusterID        string
	JoinToken        string
	ProviderID       string
	Provider         string
	ProviderName     string
	Plan             []byte
	ProviderServerID string
	Deploy           []byte
	ClusterName      string
	ControlPlane     bool
	CreatedAt        time.Time
}

func (s *Store) CreateServer(ctx context.Context, ownerID, id, name, token, providerID, clusterID, status string, plan, deploy []byte, keyIDs []string, controlPlane bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if providerID != "" {
		var one int
		err := tx.QueryRow(ctx, `
			SELECT 1 FROM secrets
			WHERE id = $1 AND owner_id = $2 AND kind = 'provider_token'`, providerID, ownerID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	if len(plan) == 0 {
		plan = []byte("{}")
	}
	if len(deploy) == 0 {
		deploy = []byte("[]")
	}
	var cluster *string
	if clusterID != "" {
		var one int
		err = tx.QueryRow(ctx, `SELECT 1 FROM clusters WHERE id = $1 AND owner_id = $2`, clusterID, ownerID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		cluster = &clusterID
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO servers (id, owner_id, cluster_id, name, origin, status, join_token, provider_id, plan, deploy, control_plane)
		VALUES ($1, $2, $3, $4, 'linked', $5, $6, $7, $8, $9, $10)`,
		id, ownerID, cluster, name, status, token, providerID, string(plan), string(deploy), controlPlane)
	if err != nil {
		return nameTaken(err)
	}
	for _, keyID := range keyIDs {
		tag, err := tx.Exec(ctx, `
			INSERT INTO server_keys (server_id, secret_id)
			SELECT $1, id FROM secrets WHERE id = $2 AND owner_id = $3 AND kind = 'ssh_key'`, id, keyID, ownerID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) FinishPurchase(ctx context.Context, ownerID, id, address, providerServerID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE servers
		SET status = 'linked', origin = 'purchased', address = $3, provider_server_id = $4
		WHERE id = $1 AND owner_id = $2 AND status = 'pending'`, id, ownerID, address, providerServerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Server(ctx context.Context, ownerID, id string) (Server, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.name, s.origin, s.status, s.address, COALESCE(s.cluster_id, ''), s.join_token, s.created_at,
		       s.provider_id, COALESCE(sec.provider, ''), COALESCE(sec.name, ''), s.plan,
		       s.provider_server_id, s.deploy, COALESCE(clu.name, ''), s.control_plane
		FROM servers s
		LEFT JOIN secrets sec ON sec.id = s.provider_id AND sec.owner_id = s.owner_id
		LEFT JOIN clusters clu ON clu.id = s.cluster_id
		WHERE s.owner_id = $1 AND s.id = $2`, ownerID, id)
	if err != nil {
		return Server{}, err
	}
	defer rows.Close()
	list, err := collectServers(rows)
	if err != nil {
		return Server{}, err
	}
	if len(list) == 0 {
		return Server{}, ErrNotFound
	}
	return list[0], nil
}

func (s *Store) SetDeploy(ctx context.Context, ownerID, id, status, address, providerServerID string, steps []byte) error {
	if len(steps) == 0 {
		steps = []byte("[]")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE servers
		SET status = $3,
		    address = CASE WHEN $4 = '' THEN address ELSE $4 END,
		    provider_server_id = CASE WHEN $5 = '' THEN provider_server_id ELSE $5 END,
		    deploy = $6
		WHERE id = $1 AND owner_id = $2`,
		id, ownerID, status, address, providerServerID, string(steps),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetDeployActive(ctx context.Context, ownerID, id, status, address, providerServerID string, steps []byte) error {
	if len(steps) == 0 {
		steps = []byte("[]")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE servers
		SET status = $3,
		    address = CASE WHEN $4 = '' THEN address ELSE $4 END,
		    provider_server_id = CASE WHEN $5 = '' THEN provider_server_id ELSE $5 END,
		    deploy = $6
		WHERE id = $1 AND owner_id = $2 AND status IN ('pending', 'deploying')`,
		id, ownerID, status, address, providerServerID, string(steps),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var one int
		err = s.pool.QueryRow(ctx, `SELECT 1 FROM servers WHERE id = $1 AND owner_id = $2`, id, ownerID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return ErrStopped
	}
	return nil
}

func (s *Store) RememberMachine(ctx context.Context, ownerID, id, address, providerServerID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE servers
		SET address = CASE WHEN $3 = '' THEN address ELSE $3 END,
		    provider_server_id = CASE WHEN $4 = '' THEN provider_server_id ELSE $4 END
		WHERE id = $1 AND owner_id = $2`,
		id, ownerID, address, providerServerID)
	return err
}

func (s *Store) CountProviderServers(ctx context.Context, ownerID, providerID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM servers WHERE owner_id = $1 AND provider_id = $2`, ownerID, providerID).Scan(&n)
	return n, err
}

func (s *Store) DeleteServer(ctx context.Context, ownerID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM applications WHERE server_id = $1 AND owner_id = $2`, id, ownerID); err != nil {
		return err
	}
	// An update of the server ends with it: it would hold the next ones.
	if _, err := tx.Exec(ctx, `UPDATE update_jobs SET status = 'cancelled', finished_at = now() WHERE owner_id = $2 AND server_id = $1 AND status IN ('running', 'failed')`, id, ownerID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM servers WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (s *Store) DeletePendingServer(ctx context.Context, ownerID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM servers WHERE id = $1 AND owner_id = $2`, id, ownerID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "pending" {
		return ErrLinked
	}
	if _, err := tx.Exec(ctx, `DELETE FROM applications WHERE server_id = $1 AND owner_id = $2`, id, ownerID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM servers WHERE id = $1 AND owner_id = $2 AND status = 'pending'`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// DeployingServers lists the servers of every owner left in the middle of a
// deployment. At startup no deployment runs yet, so each of them was cut off.
func (s *Store) DeployingServers(ctx context.Context) (owners, ids []string, steps [][]byte, err error) {
	rows, err := s.pool.Query(ctx, `SELECT owner_id, id, deploy FROM servers WHERE status = 'deploying'`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var owner, id string
		var raw []byte
		if err := rows.Scan(&owner, &id, &raw); err != nil {
			return nil, nil, nil, err
		}
		owners, ids, steps = append(owners, owner), append(ids, id), append(steps, raw)
	}
	return owners, ids, steps, rows.Err()
}

func (s *Store) ListServers(ctx context.Context, ownerID string) ([]Server, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.name, s.origin, s.status, s.address, COALESCE(s.cluster_id, ''), s.join_token, s.created_at,
		       s.provider_id, COALESCE(sec.provider, ''), COALESCE(sec.name, ''), s.plan,
		       s.provider_server_id, s.deploy, COALESCE(clu.name, ''), s.control_plane
		FROM servers s
		LEFT JOIN secrets sec ON sec.id = s.provider_id AND sec.owner_id = s.owner_id
		LEFT JOIN clusters clu ON clu.id = s.cluster_id
		WHERE s.owner_id = $1
		ORDER BY s.created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectServers(rows)
}

func (s *Store) ServerByToken(ctx context.Context, token string) (Server, []string, error) {
	var srv Server
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, origin, status, address, COALESCE(cluster_id, ''), join_token, deploy
		FROM servers WHERE join_token = $1`, token).Scan(&srv.ID, &srv.Name, &srv.Origin, &srv.Status, &srv.Address, &srv.ClusterID, &srv.JoinToken, &srv.Deploy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{}, nil, ErrNotFound
	}
	if err != nil {
		return Server{}, nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT sec.public_key
		FROM server_keys sk
		JOIN secrets sec ON sec.id = sk.secret_id
		WHERE sk.server_id = $1`, srv.ID)
	if err != nil {
		return Server{}, nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return Server{}, nil, err
		}
		keys = append(keys, key)
	}
	return srv, keys, rows.Err()
}

type DesiredApp struct {
	ID        string
	Service   string
	Image     string
	Hosts     []string
	Zones     map[string]string
	Env       map[string]string
	Disk      bool
	DiskPath  string
	DiskGi    int
	Listen    int
	Args      []string
	Published bool
	Replicas  int
	Paused    bool
}

func (s *Store) DesiredApps(ctx context.Context, token string) ([]DesiredApp, error) {
	var serverID, clusterID string
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(cluster_id, '') FROM servers WHERE join_token = $1`, token).Scan(&serverID, &clusterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if clusterID != "" {
		return []DesiredApp{}, nil
	}
	return s.desired(ctx, `
		SELECT a.id, s.name, s.image, s.published, s.disk, s.replicas, s.listen, s.disk_path, s.disk_gi, s.args, a.variables, a.paused
		FROM applications a
		JOIN application_services s ON s.application_id = a.id
		WHERE a.server_id = $1 AND a.destination_kind = 'server' AND a.source_kind = 'image'
		ORDER BY a.created_at, s.published DESC, s.name`, serverID)
}

func (s *Store) desired(ctx context.Context, query, id string) ([]DesiredApp, error) {
	rows, err := s.pool.Query(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DesiredApp
	hostsByApp := map[string][]string{}
	zonesByApp := map[string]map[string]string{}
	for rows.Next() {
		var app DesiredApp
		var raw []byte
		var args string
		if err := rows.Scan(&app.ID, &app.Service, &app.Image, &app.Published, &app.Disk, &app.Replicas, &app.Listen, &app.DiskPath, &app.DiskGi, &args, &raw, &app.Paused); err != nil {
			return nil, err
		}
		app.Args = splitArgs(args)
		app.Env = map[string]string{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &app.Env)
		}
		if app.Replicas < 1 {
			app.Replicas = 1
		}
		if app.Disk {
			app.Replicas = 1
		}
		out = append(out, app)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if !out[i].Published {
			continue
		}
		hosts, ok := hostsByApp[out[i].ID]
		if !ok {
			found, zones, err := readyHosts(ctx, s, out[i].ID)
			if err != nil {
				return nil, err
			}
			hosts = found
			hostsByApp[out[i].ID] = hosts
			zonesByApp[out[i].ID] = zones
		}
		out[i].Hosts = hosts
		out[i].Zones = zonesByApp[out[i].ID]
	}
	return out, nil
}

func readyHosts(ctx context.Context, s *Store, applicationID string) ([]string, map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT hostname, COALESCE(zone, '') FROM domains
		WHERE application_id = $1 AND status = 'ready'
		ORDER BY hostname`, applicationID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var hosts []string
	zones := map[string]string{}
	for rows.Next() {
		var host, zone string
		if err := rows.Scan(&host, &zone); err != nil {
			return nil, nil, err
		}
		hosts = append(hosts, host)
		zones[host] = zone
	}
	return hosts, zones, rows.Err()
}

// AppNames are the names a person gave an application, with the DNS account
// that publishes each one.
func (s *Store) AppNames(ctx context.Context, applicationID string) ([]Name, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, hostname, COALESCE(zone, ''), COALESCE(secret_id, '') FROM domains
		WHERE application_id = $1 AND kind = 'custom'
		ORDER BY created_at, hostname`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Name
	for rows.Next() {
		var name Name
		if err := rows.Scan(&name.ID, &name.Hostname, &name.Zone, &name.SecretID); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// AddNames gives an application new names, waiting for their DNS record. A
// name another application holds refuses them all.
func (s *Store) AddNames(ctx context.Context, applicationID string, names []Name) error {
	if len(names) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, name := range names {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT exists(SELECT 1 FROM domains WHERE hostname = $1)`, name.Hostname).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrNameTaken
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO domains (id, application_id, hostname, kind, status, zone, secret_id)
			VALUES ($1, $2, $3, 'custom', 'pending_dns', $4, $5)`,
			name.ID, applicationID, name.Hostname, name.Zone, name.SecretID); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrNameTaken
			}
			return err
		}
	}
	return tx.Commit(ctx)
}

// RemoveName takes a name from an application.
func (s *Store) RemoveName(ctx context.Context, applicationID, hostname string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM domains WHERE application_id = $1 AND kind = 'custom' AND hostname = $2`, applicationID, hostname)
	return err
}

// CustomNames are the names a person gave an application, with whether
// each one is published yet.
func (s *Store) CustomNames(ctx context.Context, applicationID string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT hostname, status FROM domains WHERE application_id = $1 AND kind = 'custom'`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var host, status string
		if err := rows.Scan(&host, &status); err != nil {
			return nil, err
		}
		out[host] = status == "ready"
	}
	return out, rows.Err()
}

func (s *Store) ReportRun(ctx context.Context, token, appID, status string) error {
	if status != "running" && status != "failed" && status != "paused" {
		return ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE applications AS a
		SET run_status = $3
		FROM servers AS s
		WHERE s.join_token = $1
		  AND s.cluster_id IS NULL
		  AND a.server_id = s.id
		  AND a.id = $2
		  AND a.source_kind = 'image'`, token, appID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type AloneServer struct {
	ID        string
	OwnerID   string
	Address   string
	JoinToken string
}

// AloneServers are the linked machines that run alone, with Docker.
func (s *Store) AloneServers(ctx context.Context) ([]AloneServer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, address, join_token FROM servers
		WHERE cluster_id IS NULL AND status = 'linked' AND address <> ''
		ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AloneServer
	for rows.Next() {
		var row AloneServer
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.Address, &row.JoinToken); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ServerAppStatuses is the state of each application of a machine alone.
func (s *Store) ServerAppStatuses(ctx context.Context, serverID string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, run_status FROM applications WHERE server_id = $1 AND destination_kind = 'server'`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		out[id] = status
	}
	return out, rows.Err()
}

// EnsureWireGuardIndex gives a cluster node its number in 10.66.0.0/24.
// The number never changes, so Kubernetes keeps the same node address.
func (s *Store) EnsureWireGuardIndex(ctx context.Context, id string) (int, error) {
	for attempt := 0; attempt < 5; attempt++ {
		var index int
		var cluster *string
		if err := s.pool.QueryRow(ctx, `SELECT wg_index, cluster_id FROM servers WHERE id = $1`, id).Scan(&index, &cluster); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, ErrNotFound
			}
			return 0, err
		}
		if index > 0 {
			return index, nil
		}
		if cluster == nil {
			return 0, fmt.Errorf("Cette machine n'appartient à aucun cluster.")
		}
		err := s.pool.QueryRow(ctx, `
			UPDATE servers SET wg_index = (
				SELECT min(n) FROM generate_series(1, 254) AS n
				WHERE n NOT IN (SELECT wg_index FROM servers WHERE cluster_id = $2 AND wg_index > 0)
			)
			WHERE id = $1 AND wg_index = 0
			RETURNING wg_index`, id, *cluster).Scan(&index)
		if err == nil && index > 0 {
			return index, nil
		}
		var pgErr *pgconn.PgError
		if err != nil && !errors.Is(err, pgx.ErrNoRows) && !(errors.As(err, &pgErr) && pgErr.Code == "23505") {
			return 0, err
		}
	}
	return 0, fmt.Errorf("Le réseau du cluster est complet.")
}

// WireGuardIndexes returns the number of each node of a cluster that has one.
func (s *Store) WireGuardIndexes(ctx context.Context, clusterID string) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, wg_index FROM servers WHERE cluster_id = $1 AND wg_index > 0`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var index int
		if err := rows.Scan(&id, &index); err != nil {
			return nil, err
		}
		out[id] = index
	}
	return out, rows.Err()
}

// RememberAddress records the address a pasted command called from, when none
// is known yet.
func (s *Store) RememberAddress(ctx context.Context, token, address string) error {
	_, err := s.pool.Exec(ctx, `UPDATE servers SET address = $2 WHERE join_token = $1 AND address = ''`, token, address)
	return err
}

// SetJoinAddress replaces the address announced by the machine itself.
func (s *Store) SetJoinAddress(ctx context.Context, token, address string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE servers SET address = $2 WHERE join_token = $1`, token, address)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ServerOwner(ctx context.Context, id string) (string, error) {
	var owner string
	err := s.pool.QueryRow(ctx, `SELECT owner_id FROM servers WHERE id = $1`, id).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return owner, err
}

// MarkLinked links a machine alone that ran its pasted command.
func (s *Store) MarkLinked(ctx context.Context, token string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE servers SET status = 'linked' WHERE join_token = $1 AND cluster_id IS NULL AND status = 'pending'`, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AttachCluster(ctx context.Context, ownerID, serverID, clusterID string) error {
	if clusterID != "" {
		var n int
		if err := s.pool.QueryRow(ctx, `
			SELECT count(*) FROM applications
			WHERE owner_id = $1 AND server_id = $2 AND destination_kind = 'server'`, ownerID, serverID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrHasApps
		}
		var one int
		err := s.pool.QueryRow(ctx, `SELECT 1 FROM clusters WHERE id = $1 AND owner_id = $2`, clusterID, ownerID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE servers SET cluster_id = NULLIF($1, '')
		WHERE id = $2 AND owner_id = $3`, clusterID, serverID, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type Cluster struct {
	ID     string
	Name   string
	Origin string
}

func (s *Store) Cluster(ctx context.Context, ownerID, id string) (Cluster, error) {
	var cluster Cluster
	err := s.pool.QueryRow(ctx, `SELECT id, name, origin FROM clusters WHERE id = $1 AND owner_id = $2`, id, ownerID).Scan(&cluster.ID, &cluster.Name, &cluster.Origin)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cluster{}, ErrNotFound
	}
	return cluster, err
}

func (s *Store) ClusterNames(ctx context.Context, ownerID, clusterID string) ([]Name, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.hostname, d.zone, d.secret_id
		FROM domains d
		JOIN applications a ON a.id = d.application_id
		WHERE a.owner_id = $1
		  AND d.kind = 'custom' AND d.zone <> '' AND d.secret_id <> ''
		  AND (
		    a.cluster_id = $2
		    OR a.server_id IN (SELECT id FROM servers WHERE owner_id = $1 AND cluster_id = $2)
		  )`, ownerID, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Name
	for rows.Next() {
		var name Name
		if err := rows.Scan(&name.Hostname, &name.Zone, &name.SecretID); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *Store) DomainName(ctx context.Context, ownerID, host string) (Name, string, error) {
	var name Name
	var provider string
	err := s.pool.QueryRow(ctx, `
		SELECT d.hostname, COALESCE(d.zone, ''), COALESCE(d.secret_id, ''), COALESCE(sec.provider, '')
		FROM domains d
		JOIN applications a ON a.id = d.application_id
		LEFT JOIN secrets sec ON sec.id = d.secret_id
		WHERE a.owner_id = $1 AND d.hostname = $2`, ownerID, host).Scan(&name.Hostname, &name.Zone, &name.SecretID, &provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return Name{}, "", ErrNotFound
	}
	return name, provider, err
}

func (s *Store) DeleteCluster(ctx context.Context, ownerID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE update_jobs SET status = 'cancelled', finished_at = now() WHERE owner_id = $1 AND cluster_id = $2 AND status IN ('running', 'failed')`, ownerID, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM applications
		WHERE owner_id = $1 AND (
			cluster_id = $2
			OR server_id IN (SELECT id FROM servers WHERE owner_id = $1 AND cluster_id = $2)
		)`, ownerID, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM servers WHERE owner_id = $1 AND cluster_id = $2`, ownerID, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM clusters WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateCluster(ctx context.Context, ownerID, id, name string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO clusters (id, owner_id, name) VALUES ($1, $2, $3)`, id, ownerID, name)
	return nameTaken(err)
}

func (s *Store) ListClusters(ctx context.Context, ownerID string) ([]Cluster, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, origin FROM clusters WHERE owner_id = $1 ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cluster
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.Name, &c.Origin); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ImportCluster(ctx context.Context, ownerID, id, name string, ciphertext, nonce, wrapped []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO clusters (id, owner_id, name, origin, kube_ciphertext, kube_nonce, kube_wrapped)
		VALUES ($1, $2, $3, 'imported', $4, $5, $6)`, id, ownerID, name, ciphertext, nonce, wrapped)
	return nameTaken(err)
}

func (s *Store) ClusterKube(ctx context.Context, ownerID, id string) (string, []byte, []byte, []byte, error) {
	var origin string
	var ciphertext, nonce, wrapped []byte
	err := s.pool.QueryRow(ctx, `
		SELECT origin, kube_ciphertext, kube_nonce, kube_wrapped
		FROM clusters WHERE id = $1 AND owner_id = $2`, id, ownerID).Scan(&origin, &ciphertext, &nonce, &wrapped)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil, nil, ErrNotFound
	}
	return origin, ciphertext, nonce, wrapped, err
}

type ClusterLeader struct {
	ClusterID string
	ServerID  string
	Address   string
	OwnerID   string
}

func (s *Store) ClusterLeaders(ctx context.Context) ([]ClusterLeader, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (s.cluster_id) s.cluster_id, s.id, s.address, s.owner_id
		FROM servers s
		JOIN clusters c ON c.id = s.cluster_id
		WHERE c.origin = 'created' AND s.status = 'linked' AND s.address <> '' AND s.control_plane
		ORDER BY s.cluster_id, s.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClusterLeader
	for rows.Next() {
		var row ClusterLeader
		if err := rows.Scan(&row.ClusterID, &row.ServerID, &row.Address, &row.OwnerID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

type ClusterApp struct {
	ID             string
	Paused         bool
	RunStatus      string
	PreparingSince *time.Time
	Services       []Service
	Hosts          []string
	Zones          map[string]string
	Env            map[string]string
}

func (s *Store) ClusterApps(ctx context.Context, clusterID string) ([]ClusterApp, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.paused, s.id, s.name, s.image, s.published, s.disk, s.replicas, s.listen, s.disk_path, s.disk_gi, s.args, a.variables, a.run_status, a.preparing_since, s.backup, s.database
		FROM applications a
		JOIN application_services s ON s.application_id = a.id
		WHERE a.cluster_id = $1 AND a.destination_kind = 'cluster' AND a.source_kind = 'image'
		ORDER BY a.created_at, s.published DESC, s.name`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]*ClusterApp{}
	var order []string
	for rows.Next() {
		var appID string
		var paused bool
		var svc Service
		var raw []byte
		var args, runStatus string
		var preparingSince *time.Time
		if err := rows.Scan(&appID, &paused, &svc.ID, &svc.Name, &svc.Image, &svc.Published, &svc.Disk, &svc.Replicas, &svc.Listen, &svc.DiskPath, &svc.DiskGi, &args, &raw, &runStatus, &preparingSince, &svc.Backup, &svc.Database); err != nil {
			return nil, err
		}
		svc.Args = splitArgs(args)
		app := byID[appID]
		if app == nil {
			env := map[string]string{}
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &env)
			}
			app = &ClusterApp{ID: appID, Paused: paused, Env: env, RunStatus: runStatus, PreparingSince: preparingSince}
			byID[appID] = app
			order = append(order, appID)
		}
		// A disk holds one copy, unless the data moved to PostgreSQL.
		if (svc.Disk && svc.Database != "ready") || svc.Replicas < 1 {
			svc.Replicas = 1
		}
		app.Services = append(app.Services, svc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ClusterApp, 0, len(order))
	for _, id := range order {
		app := byID[id]
		for _, svc := range app.Services {
			if svc.Published {
				hosts, zones, err := readyHosts(ctx, s, id)
				if err != nil {
					return nil, err
				}
				app.Hosts = hosts
				app.Zones = zones
				break
			}
		}
		out = append(out, *app)
	}
	return out, nil
}

func (s *Store) SetPaused(ctx context.Context, ownerID, id string, paused bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE applications SET paused = $3 WHERE owner_id = $1 AND id = $2`, ownerID, id, paused)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetRunStatus(ctx context.Context, applicationID, status string) error {
	if status != "running" && status != "failed" && status != "preparing" && status != "paused" {
		return ErrNotFound
	}
	var preparing any
	switch status {
	case "preparing":
		preparing = time.Now().UTC()
	default:
		preparing = nil
	}
	tag, err := s.pool.Exec(ctx, `UPDATE applications SET run_status = $2, preparing_since = $3 WHERE id = $1`, applicationID, status, preparing)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type Service struct {
	ID        string
	Name      string
	Image     string
	Published bool
	Disk      bool
	DiskPath  string
	DiskGi    int
	Listen    int
	Args      []string
	Replicas  int
	// Backup is '', 'migrating' or 'ready': see application_services.backup.
	Backup string
	// Database is '', 'migrating' or 'ready': see application_services.database.
	Database string
}

func servicePlace(svc Service) (listen int, path string, gi int) {
	listen = svc.Listen
	if listen < 1 {
		listen = 80
	}
	path = svc.DiskPath
	if path == "" {
		path = "/data"
	}
	gi = svc.DiskGi
	if gi < 1 {
		gi = 1
	}
	return listen, path, gi
}

func splitArgs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Fields(raw)
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

type Domain struct {
	ID       string
	Hostname string
	Kind     string
	Status   string
	Zone     string
	Detail   string
}

type Name struct {
	ID       string
	Hostname string
	Zone     string
	SecretID string
}

type Application struct {
	ID              string
	OwnerID         string
	Name            string
	SourceKind      string
	Source          string
	DestinationKind string
	ServerID        string
	ServerName      string
	ClusterID       string
	ClusterName     string
	Replicas        int
	Variables       map[string]string
	Hostname        string
	RunStatus       string
	Paused          bool
	Rollout         string
	RolloutDetail   string
	RunDetail       string
	// Failover is true when a service restarts on another node from its
	// replicated data: one copy is then not a single point of failure.
	Failover  bool
	Domains   []Domain
	Services  []Service
	CreatedAt time.Time
}

type NewApplication struct {
	ID              string
	OwnerID         string
	Name            string
	SourceKind      string
	Source          string
	DestinationKind string
	ServerID        string
	ClusterID       string
	Replicas        int
	Variables       map[string]string
	DefaultID       string
	DefaultHost     string
	Names           []Name
	Services        []Service
}

func (s *Store) CreateApplication(ctx context.Context, in NewApplication) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if in.DestinationKind == "server" {
		var clusterID *string
		err := tx.QueryRow(ctx, `SELECT cluster_id FROM servers WHERE id = $1 AND owner_id = $2`, in.ServerID, in.OwnerID).Scan(&clusterID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if clusterID != nil && *clusterID != "" {
			return ErrInCluster
		}
	} else {
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM clusters WHERE id = $1 AND owner_id = $2`, in.ClusterID, in.OwnerID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}

	raw, err := json.Marshal(in.Variables)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO applications (
			id, owner_id, name, source_kind, source, destination_kind, server_id, cluster_id, replicas, variables
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			NULLIF($7, ''), NULLIF($8, ''), $9, $10
		)`,
		in.ID, in.OwnerID, in.Name, in.SourceKind, in.Source, in.DestinationKind,
		in.ServerID, in.ClusterID, in.Replicas, raw,
	)
	if err != nil {
		return nameTaken(err)
	}
	host, err := freeHostname(ctx, tx, in.DefaultHost, in.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO domains (id, application_id, hostname, kind, status)
		VALUES ($1, $2, $3, 'default', 'unpublished')`, in.DefaultID, in.ID, host)
	if err != nil {
		return err
	}
	for _, name := range in.Names {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT exists(SELECT 1 FROM domains WHERE hostname = $1)`, name.Hostname).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrNameTaken
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO domains (id, application_id, hostname, kind, status, zone, secret_id)
			VALUES ($1, $2, $3, 'custom', 'pending_dns', $4, $5)`,
			name.ID, in.ID, name.Hostname, name.Zone, name.SecretID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrNameTaken
			}
			return err
		}
	}
	for _, svc := range in.Services {
		replicas := svc.Replicas
		if replicas < 1 {
			replicas = 1
		}
		listen, path, gi := servicePlace(svc)
		if _, err := tx.Exec(ctx, `
			INSERT INTO application_services (id, application_id, name, image, published, disk, replicas, listen, disk_path, disk_gi, args, backup, database)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			svc.ID, in.ID, svc.Name, svc.Image, svc.Published, svc.Disk, replicas, listen, path, gi, joinArgs(svc.Args), svc.Backup, svc.Database); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListApplications(ctx context.Context, ownerID string) ([]Application, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name, a.source_kind, a.source, a.destination_kind,
		       COALESCE(a.server_id, ''), COALESCE(srv.name, ''),
		       COALESCE(a.cluster_id, ''), COALESCE(clu.name, ''),
		       COALESCE(
		         (SELECT hostname FROM domains d WHERE d.application_id = a.id AND d.kind = 'custom' LIMIT 1),
		         (SELECT hostname FROM domains d WHERE d.application_id = a.id AND d.kind = 'default' LIMIT 1),
		         ''
		       ),
		       a.run_status, a.paused,
		       EXISTS (SELECT 1 FROM application_services s WHERE s.application_id = a.id AND s.backup = 'ready'
		               AND NOT EXISTS (SELECT 1 FROM application_services o WHERE o.application_id = a.id AND o.disk AND o.backup = '' AND o.database = ''))
		FROM applications a
		LEFT JOIN servers srv ON srv.id = a.server_id
		LEFT JOIN clusters clu ON clu.id = a.cluster_id
		WHERE a.owner_id = $1
		ORDER BY a.created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Application
	for rows.Next() {
		var app Application
		if err := rows.Scan(&app.ID, &app.Name, &app.SourceKind, &app.Source, &app.DestinationKind, &app.ServerID, &app.ServerName, &app.ClusterID, &app.ClusterName, &app.Hostname, &app.RunStatus, &app.Paused, &app.Failover); err != nil {
			return nil, err
		}
		out = append(out, app)
	}
	return out, rows.Err()
}

func (s *Store) Application(ctx context.Context, ownerID, id string) (Application, error) {
	var app Application
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT a.id, a.owner_id, a.name, a.source_kind, a.source, a.destination_kind,
		       COALESCE(a.server_id, ''), COALESCE(srv.name, ''),
		       COALESCE(a.cluster_id, ''), COALESCE(clu.name, ''),
		       a.replicas, a.variables, a.created_at, a.run_status, a.paused, a.rollout, a.rollout_detail, a.run_detail
		FROM applications a
		LEFT JOIN servers srv ON srv.id = a.server_id
		LEFT JOIN clusters clu ON clu.id = a.cluster_id
		WHERE a.owner_id = $1 AND a.id = $2`, ownerID, id).Scan(
		&app.ID, &app.OwnerID, &app.Name, &app.SourceKind, &app.Source, &app.DestinationKind,
		&app.ServerID, &app.ServerName, &app.ClusterID, &app.ClusterName,
		&app.Replicas, &raw, &app.CreatedAt, &app.RunStatus, &app.Paused, &app.Rollout, &app.RolloutDetail, &app.RunDetail,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Application{}, ErrNotFound
	}
	if err != nil {
		return Application{}, err
	}
	app.Variables = map[string]string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &app.Variables)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, hostname, kind, status, zone, detail FROM domains WHERE application_id = $1 ORDER BY kind DESC, hostname`, id)
	if err != nil {
		return Application{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.Hostname, &d.Kind, &d.Status, &d.Zone, &d.Detail); err != nil {
			return Application{}, err
		}
		app.Domains = append(app.Domains, d)
		if d.Kind == "custom" || app.Hostname == "" {
			app.Hostname = d.Hostname
		}
	}
	if err := rows.Err(); err != nil {
		return Application{}, err
	}
	app.Services, err = s.services(ctx, id)
	return app, err
}

func (s *Store) ApplicationByID(ctx context.Context, id string) (Application, error) {
	var owner string
	err := s.pool.QueryRow(ctx, `SELECT owner_id FROM applications WHERE id = $1`, id).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return Application{}, ErrNotFound
	}
	if err != nil {
		return Application{}, err
	}
	return s.Application(ctx, owner, id)
}

func (s *Store) services(ctx context.Context, applicationID string) ([]Service, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, image, published, disk, replicas, listen, disk_path, disk_gi, args, backup, database
		FROM application_services WHERE application_id = $1 ORDER BY published DESC, name`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		var svc Service
		var args string
		if err := rows.Scan(&svc.ID, &svc.Name, &svc.Image, &svc.Published, &svc.Disk, &svc.Replicas, &svc.Listen, &svc.DiskPath, &svc.DiskGi, &args, &svc.Backup, &svc.Database); err != nil {
			return nil, err
		}
		svc.Args = splitArgs(args)
		out = append(out, svc)
	}
	return out, rows.Err()
}

func collectServers(rows pgx.Rows) ([]Server, error) {
	var out []Server
	for rows.Next() {
		var srv Server
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.Origin, &srv.Status, &srv.Address, &srv.ClusterID, &srv.JoinToken, &srv.CreatedAt, &srv.ProviderID, &srv.Provider, &srv.ProviderName, &srv.Plan, &srv.ProviderServerID, &srv.Deploy, &srv.ClusterName, &srv.ControlPlane); err != nil {
			return nil, err
		}
		out = append(out, srv)
	}
	return out, rows.Err()
}

type PendingName struct {
	ID              string
	Hostname        string
	Zone            string
	SecretID        string
	OwnerID         string
	DestinationKind string
	ServerID        string
	ClusterID       string
}

func (s *Store) PendingNames(ctx context.Context) ([]PendingName, error) {
	return s.namesWithStatus(ctx, "pending_dns")
}

// ReadyNames are the published names, checked again when the machines change.
func (s *Store) ReadyNames(ctx context.Context) ([]PendingName, error) {
	return s.namesWithStatus(ctx, "ready")
}

func (s *Store) namesWithStatus(ctx context.Context, status string) ([]PendingName, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.hostname, d.zone, d.secret_id, a.owner_id, a.destination_kind,
		       COALESCE(a.server_id, ''), COALESCE(a.cluster_id, '')
		FROM domains d
		JOIN applications a ON a.id = d.application_id
		WHERE d.kind = 'custom' AND d.status = $1 AND d.zone <> '' AND d.secret_id <> ''`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingName
	for rows.Next() {
		var row PendingName
		if err := rows.Scan(&row.ID, &row.Hostname, &row.Zone, &row.SecretID, &row.OwnerID, &row.DestinationKind, &row.ServerID, &row.ClusterID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) MarkName(ctx context.Context, id, status, detail string) error {
	if len(detail) > 200 {
		detail = detail[:200]
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE domains SET status = $2, detail = $3
		WHERE id = $1 AND status = 'pending_dns'`, id, status, detail)
	return err
}

// DestinationAddresses are the addresses a name points to: the machine, or
// every linked node of the cluster, since each node answers on 80 and 443.
func (s *Store) DestinationAddresses(ctx context.Context, kind, serverID, clusterID string) ([]string, error) {
	if kind == "server" {
		address, err := s.DestinationAddress(ctx, kind, serverID, clusterID)
		if err != nil || address == "" {
			return nil, err
		}
		return []string{address}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT address FROM servers
		WHERE cluster_id = $1 AND status = 'linked' AND address <> ''
		ORDER BY address`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			return nil, err
		}
		out = append(out, address)
	}
	return out, rows.Err()
}

func (s *Store) DestinationAddress(ctx context.Context, kind, serverID, clusterID string) (string, error) {
	var address string
	var err error
	if kind == "server" {
		err = s.pool.QueryRow(ctx, `SELECT address FROM servers WHERE id = $1`, serverID).Scan(&address)
	} else {
		err = s.pool.QueryRow(ctx, `
			SELECT address FROM servers
			WHERE cluster_id = $1 AND status = 'linked' AND address <> ''
			ORDER BY created_at
			LIMIT 1`, clusterID).Scan(&address)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return address, err
}

func freeHostname(ctx context.Context, tx pgx.Tx, host, id string) (string, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT exists(SELECT 1 FROM domains WHERE hostname = $1)`, host).Scan(&exists); err != nil {
		return "", err
	}
	if !exists {
		return host, nil
	}
	suffix := id
	if len(suffix) > 4 {
		suffix = suffix[:4]
	}
	return host + "-" + suffix, nil
}

// OwnedName is an address of an application MyInfra writes in a DNS zone.
type OwnedName struct {
	PendingName
	Status  string
	AppID   string
	AppName string
}

// OwnedNames lists the addresses MyInfra writes for an owner's applications.
func (s *Store) OwnedNames(ctx context.Context, owner string) ([]OwnedName, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.hostname, d.zone, d.secret_id, a.owner_id, a.destination_kind,
		       COALESCE(a.server_id, ''), COALESCE(a.cluster_id, ''), d.status, a.id, a.name
		FROM domains d
		JOIN applications a ON a.id = d.application_id
		WHERE d.kind = 'custom' AND d.zone <> '' AND a.owner_id = $1
		ORDER BY d.hostname`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OwnedName
	for rows.Next() {
		var row OwnedName
		if err := rows.Scan(&row.ID, &row.Hostname, &row.Zone, &row.SecretID, &row.OwnerID, &row.DestinationKind, &row.ServerID, &row.ClusterID, &row.Status, &row.AppID, &row.AppName); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// HoldNames stops publishing the names of what is being removed: an
// application, the applications of a server or of a cluster. Without it, the
// names a destruction removes are written again until the applications go.
func (s *Store) HoldNames(ctx context.Context, owner, kind, target string) error {
	var where string
	switch kind {
	case "application":
		where = `application_id = $2 AND application_id IN (SELECT id FROM applications WHERE owner_id = $1)`
	case "server":
		where = `application_id IN (SELECT id FROM applications WHERE owner_id = $1 AND server_id = $2)`
	case "cluster":
		where = `application_id IN (SELECT id FROM applications WHERE owner_id = $1 AND cluster_id = $2)`
	default:
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE domains SET status = 'unpublished' WHERE kind = 'custom' AND `+where, owner, target)
	return err
}
