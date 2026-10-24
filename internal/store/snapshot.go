package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

type Installation struct {
	Provider  string `json:"provider"`
	AccountID string `json:"accountId"`
	ServerID  string `json:"serverId"`
	PublicURL string `json:"publicUrl"`
	Method    string `json:"method,omitempty"`
	ClusterID string `json:"clusterId,omitempty"`
}

type Snapshot struct {
	Users        []snapUser        `json:"users"`
	Secrets      []snapSecret      `json:"secrets"`
	Clusters     []snapCluster     `json:"clusters"`
	Servers      []snapServer      `json:"servers"`
	ServerKeys   []snapServerKey   `json:"serverKeys"`
	Applications []snapApplication `json:"applications"`
	Services     []snapService     `json:"services,omitempty"`
	Domains      []snapDomain      `json:"domains"`
	AppSecrets   []snapAppSecret   `json:"appSecrets,omitempty"`
	Installation Installation      `json:"installation"`
	// Peers are copies the new copy keeps in sync with: the one that moved
	// it, when that one stays and can be reached.
	Peers []string `json:"peers,omitempty"`
}

type snapAppSecret struct {
	ID         string   `json:"id"`
	OwnerID    string   `json:"ownerId"`
	Name       string   `json:"name"`
	Ciphertext []byte   `json:"ciphertext"`
	Nonce      []byte   `json:"nonce"`
	WrappedDEK []byte   `json:"wrappedDek"`
	Apps       []string `json:"apps"`
}

type snapUser struct {
	ID           string `json:"id"`
	PasswordHash string `json:"passwordHash"`
	Login        string `json:"login,omitempty"`
	Role         string `json:"role,omitempty"`
	TOTPSecret   string `json:"totpSecret,omitempty"`
	Onboarded    bool   `json:"onboarded,omitempty"`
}

type snapSecret struct {
	ID          string `json:"id"`
	OwnerID     string `json:"ownerId"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
	Hint        string `json:"hint"`
	KeyID       string `json:"keyId,omitempty"`
	HasPrivate  bool   `json:"hasPrivate"`
	Ciphertext  []byte `json:"ciphertext"`
	Nonce       []byte `json:"nonce"`
	WrappedDEK  []byte `json:"wrappedDek"`
}

type snapCluster struct {
	ID             string `json:"id"`
	OwnerID        string `json:"ownerId"`
	Name           string `json:"name"`
	Origin         string `json:"origin,omitempty"`
	KubeCiphertext []byte `json:"kubeCiphertext,omitempty"`
	KubeNonce      []byte `json:"kubeNonce,omitempty"`
	KubeWrapped    []byte `json:"kubeWrapped,omitempty"`
}

type snapService struct {
	ID            string   `json:"id"`
	ApplicationID string   `json:"applicationId"`
	Name          string   `json:"name"`
	Image         string   `json:"image"`
	Published     bool     `json:"published"`
	Disk          bool     `json:"disk"`
	Replicas      int      `json:"replicas"`
	Listen        int      `json:"listen,omitempty"`
	DiskPath      string   `json:"diskPath,omitempty"`
	DiskGi        int      `json:"diskGi,omitempty"`
	Args          []string `json:"args,omitempty"`
	// Spec is left out when empty, as in snapshots made before it.
	Spec json.RawMessage `json:"spec,omitempty"`
}

type snapServer struct {
	ID               string          `json:"id"`
	OwnerID          string          `json:"ownerId"`
	ClusterID        *string         `json:"clusterId,omitempty"`
	Name             string          `json:"name"`
	Origin           string          `json:"origin"`
	Status           string          `json:"status"`
	Address          string          `json:"address"`
	JoinToken        string          `json:"joinToken"`
	ProviderID       string          `json:"providerId"`
	ProviderServerID string          `json:"providerServerId,omitempty"`
	Plan             json.RawMessage `json:"plan"`
	Deploy           json.RawMessage `json:"deploy,omitempty"`
}

type snapServerKey struct {
	ServerID string `json:"serverId"`
	SecretID string `json:"secretId"`
}

type snapApplication struct {
	ID              string          `json:"id"`
	OwnerID         string          `json:"ownerId"`
	Name            string          `json:"name"`
	SourceKind      string          `json:"sourceKind"`
	Source          string          `json:"source"`
	DestinationKind string          `json:"destinationKind"`
	ServerID        *string         `json:"serverId,omitempty"`
	ClusterID       *string         `json:"clusterId,omitempty"`
	Replicas        int             `json:"replicas"`
	Variables       json.RawMessage `json:"variables"`
	RunStatus       string          `json:"runStatus"`
	Paused          bool            `json:"paused,omitempty"`
}

type snapDomain struct {
	ID            string `json:"id"`
	ApplicationID string `json:"applicationId"`
	Hostname      string `json:"hostname"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Zone          string `json:"zone,omitempty"`
	SecretID      string `json:"secretId,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Service       string `json:"service,omitempty"`
}

func (s *Store) HostnameTaken(ctx context.Context, host string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT exists(SELECT 1 FROM domains WHERE hostname = $1)`, host).Scan(&exists)
	return exists, err
}

func (s *Store) Installation(ctx context.Context) (Installation, error) {
	var inst Installation
	err := s.pool.QueryRow(ctx, `SELECT provider, account_id, server_id, public_url, method, cluster_id FROM installation WHERE singleton`).
		Scan(&inst.Provider, &inst.AccountID, &inst.ServerID, &inst.PublicURL, &inst.Method, &inst.ClusterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Installation{}, nil
	}
	return inst, err
}

func (s *Store) ExportSnapshot(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	users, err := s.pool.Query(ctx, `SELECT id, password_hash, login, role, totp_secret, onboarded FROM users ORDER BY created_at`)
	if err != nil {
		return Snapshot{}, err
	}
	defer users.Close()
	for users.Next() {
		var row snapUser
		if err := users.Scan(&row.ID, &row.PasswordHash, &row.Login, &row.Role, &row.TOTPSecret, &row.Onboarded); err != nil {
			return Snapshot{}, err
		}
		snap.Users = append(snap.Users, row)
	}
	if err := users.Err(); err != nil {
		return Snapshot{}, err
	}

	secrets, err := s.pool.Query(ctx, `
		SELECT id, owner_id, kind, name, provider, fingerprint, public_key, hint, key_id, has_private, ciphertext, nonce, wrapped_dek
		FROM secrets ORDER BY created_at`)
	if err != nil {
		return Snapshot{}, err
	}
	defer secrets.Close()
	for secrets.Next() {
		var row snapSecret
		if err := secrets.Scan(&row.ID, &row.OwnerID, &row.Kind, &row.Name, &row.Provider, &row.Fingerprint, &row.PublicKey, &row.Hint, &row.KeyID, &row.HasPrivate, &row.Ciphertext, &row.Nonce, &row.WrappedDEK); err != nil {
			return Snapshot{}, err
		}
		snap.Secrets = append(snap.Secrets, row)
	}
	if err := secrets.Err(); err != nil {
		return Snapshot{}, err
	}

	clusters, err := s.pool.Query(ctx, `SELECT id, owner_id, name, origin, kube_ciphertext, kube_nonce, kube_wrapped FROM clusters ORDER BY created_at`)
	if err != nil {
		return Snapshot{}, err
	}
	defer clusters.Close()
	for clusters.Next() {
		var row snapCluster
		if err := clusters.Scan(&row.ID, &row.OwnerID, &row.Name, &row.Origin, &row.KubeCiphertext, &row.KubeNonce, &row.KubeWrapped); err != nil {
			return Snapshot{}, err
		}
		snap.Clusters = append(snap.Clusters, row)
	}
	if err := clusters.Err(); err != nil {
		return Snapshot{}, err
	}

	servers, err := s.pool.Query(ctx, `
		SELECT id, owner_id, cluster_id, name, origin, status, address, join_token, provider_id, provider_server_id, plan, deploy
		FROM servers ORDER BY created_at`)
	if err != nil {
		return Snapshot{}, err
	}
	defer servers.Close()
	for servers.Next() {
		var row snapServer
		if err := servers.Scan(&row.ID, &row.OwnerID, &row.ClusterID, &row.Name, &row.Origin, &row.Status, &row.Address, &row.JoinToken, &row.ProviderID, &row.ProviderServerID, &row.Plan, &row.Deploy); err != nil {
			return Snapshot{}, err
		}
		if len(row.Plan) == 0 {
			row.Plan = json.RawMessage(`{}`)
		}
		if len(row.Deploy) == 0 {
			row.Deploy = json.RawMessage(`[]`)
		}
		snap.Servers = append(snap.Servers, row)
	}
	if err := servers.Err(); err != nil {
		return Snapshot{}, err
	}

	keys, err := s.pool.Query(ctx, `SELECT server_id, secret_id FROM server_keys`)
	if err != nil {
		return Snapshot{}, err
	}
	defer keys.Close()
	for keys.Next() {
		var row snapServerKey
		if err := keys.Scan(&row.ServerID, &row.SecretID); err != nil {
			return Snapshot{}, err
		}
		snap.ServerKeys = append(snap.ServerKeys, row)
	}
	if err := keys.Err(); err != nil {
		return Snapshot{}, err
	}

	apps, err := s.pool.Query(ctx, `
		SELECT id, owner_id, name, source_kind, source, destination_kind, server_id, cluster_id, replicas, variables, run_status, paused
		FROM applications ORDER BY created_at`)
	if err != nil {
		return Snapshot{}, err
	}
	defer apps.Close()
	for apps.Next() {
		var row snapApplication
		if err := apps.Scan(&row.ID, &row.OwnerID, &row.Name, &row.SourceKind, &row.Source, &row.DestinationKind, &row.ServerID, &row.ClusterID, &row.Replicas, &row.Variables, &row.RunStatus, &row.Paused); err != nil {
			return Snapshot{}, err
		}
		if len(row.Variables) == 0 {
			row.Variables = json.RawMessage(`{}`)
		}
		snap.Applications = append(snap.Applications, row)
	}
	if err := apps.Err(); err != nil {
		return Snapshot{}, err
	}

	services, err := s.pool.Query(ctx, `SELECT id, application_id, name, image, published, disk, replicas, listen, disk_path, disk_gi, args, spec FROM application_services ORDER BY name`)
	if err != nil {
		return Snapshot{}, err
	}
	defer services.Close()
	for services.Next() {
		var row snapService
		var args string
		var specRaw []byte
		if err := services.Scan(&row.ID, &row.ApplicationID, &row.Name, &row.Image, &row.Published, &row.Disk, &row.Replicas, &row.Listen, &row.DiskPath, &row.DiskGi, &args, &specRaw); err != nil {
			return Snapshot{}, err
		}
		row.Args = splitArgs(args)
		row.Spec = canonSpec(specRaw)
		snap.Services = append(snap.Services, row)
	}
	if err := services.Err(); err != nil {
		return Snapshot{}, err
	}

	domains, err := s.pool.Query(ctx, `SELECT id, application_id, hostname, kind, status, zone, secret_id, detail, service FROM domains ORDER BY created_at`)
	if err != nil {
		return Snapshot{}, err
	}
	defer domains.Close()
	for domains.Next() {
		var row snapDomain
		if err := domains.Scan(&row.ID, &row.ApplicationID, &row.Hostname, &row.Kind, &row.Status, &row.Zone, &row.SecretID, &row.Detail, &row.Service); err != nil {
			return Snapshot{}, err
		}
		snap.Domains = append(snap.Domains, row)
	}
	if err := domains.Err(); err != nil {
		return Snapshot{}, err
	}
	secretsApp, err := s.pool.Query(ctx, `SELECT id, owner_id, name, ciphertext, nonce, wrapped_dek, apps FROM app_secrets ORDER BY name`)
	if err != nil {
		return Snapshot{}, err
	}
	defer secretsApp.Close()
	for secretsApp.Next() {
		var row snapAppSecret
		var apps []byte
		if err := secretsApp.Scan(&row.ID, &row.OwnerID, &row.Name, &row.Ciphertext, &row.Nonce, &row.WrappedDEK, &apps); err != nil {
			return Snapshot{}, err
		}
		if json.Unmarshal(apps, &row.Apps) != nil || row.Apps == nil {
			row.Apps = []string{}
		}
		snap.AppSecrets = append(snap.AppSecrets, row)
	}
	return snap, secretsApp.Err()
}

func (s *Store) ImportSnapshot(ctx context.Context, snap Snapshot) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('myinfra.apply', '1', true)`); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DELETE FROM sync_tomb`,
		`DELETE FROM domains`,
		`DELETE FROM application_services`,
		`DELETE FROM applications`,
		`DELETE FROM server_keys`,
		`DELETE FROM servers`,
		`DELETE FROM clusters`,
		`DELETE FROM sessions`,
		`DELETE FROM login_challenges`,
		`DELETE FROM secrets`,
		`DELETE FROM app_secrets`,
		`DELETE FROM users`,
		`DELETE FROM installation`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	for _, row := range snap.Users {
		role := row.Role
		if role == "" {
			role = "admin"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (id, password_hash, login, role, totp_secret, onboarded)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			row.ID, row.PasswordHash, row.Login, role, row.TOTPSecret, row.Onboarded); err != nil {
			return err
		}
	}
	for _, row := range snap.Secrets {
		if _, err := tx.Exec(ctx, `
			INSERT INTO secrets (
				id, owner_id, kind, name, provider, fingerprint, public_key, hint, key_id, has_private, ciphertext, nonce, wrapped_dek
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			row.ID, row.OwnerID, row.Kind, row.Name, row.Provider, row.Fingerprint, row.PublicKey, row.Hint, row.KeyID, row.HasPrivate,
			row.Ciphertext, row.Nonce, row.WrappedDEK,
		); err != nil {
			return err
		}
	}
	for _, row := range snap.AppSecrets {
		apps := row.Apps
		if apps == nil {
			apps = []string{}
		}
		raw, err := json.Marshal(apps)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO app_secrets (id, owner_id, name, ciphertext, nonce, wrapped_dek, apps)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			row.ID, row.OwnerID, row.Name, row.Ciphertext, row.Nonce, row.WrappedDEK, raw,
		); err != nil {
			return err
		}
	}
	for _, row := range snap.Clusters {
		origin := row.Origin
		if origin == "" {
			origin = "created"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO clusters (id, owner_id, name, origin, kube_ciphertext, kube_nonce, kube_wrapped)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, row.ID, row.OwnerID, row.Name, origin, row.KubeCiphertext, row.KubeNonce, row.KubeWrapped); err != nil {
			return err
		}
	}
	for _, row := range snap.Servers {
		plan := row.Plan
		if len(plan) == 0 {
			plan = json.RawMessage(`{}`)
		}
		deploy := row.Deploy
		if len(deploy) == 0 {
			deploy = json.RawMessage(`[]`)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO servers (id, owner_id, cluster_id, name, origin, status, address, join_token, provider_id, provider_server_id, plan, deploy)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			row.ID, row.OwnerID, row.ClusterID, row.Name, row.Origin, row.Status, row.Address, row.JoinToken, row.ProviderID, row.ProviderServerID, string(plan), string(deploy),
		); err != nil {
			return err
		}
	}
	for _, row := range snap.ServerKeys {
		if _, err := tx.Exec(ctx, `INSERT INTO server_keys (server_id, secret_id) VALUES ($1, $2)`, row.ServerID, row.SecretID); err != nil {
			return err
		}
	}
	for _, row := range snap.Applications {
		variables := row.Variables
		if len(variables) == 0 {
			variables = json.RawMessage(`{}`)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO applications (
				id, owner_id, name, source_kind, source, destination_kind, server_id, cluster_id, replicas, variables, run_status, paused
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			row.ID, row.OwnerID, row.Name, row.SourceKind, row.Source, row.DestinationKind, row.ServerID, row.ClusterID, row.Replicas, string(variables), row.RunStatus, row.Paused,
		); err != nil {
			return err
		}
	}
	for _, row := range snap.Services {
		replicas := row.Replicas
		if replicas < 1 {
			replicas = 1
		}
		listen, path, gi := servicePlace(Service{Listen: row.Listen, DiskPath: row.DiskPath, DiskGi: row.DiskGi})
		if _, err := tx.Exec(ctx, `
			INSERT INTO application_services (id, application_id, name, image, published, disk, replicas, listen, disk_path, disk_gi, args, spec)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			row.ID, row.ApplicationID, row.Name, row.Image, row.Published, row.Disk, replicas, listen, path, gi, joinArgs(row.Args), specJSON(readSpec(row.Spec))); err != nil {
			return err
		}
	}
	for _, row := range snap.Domains {
		if _, err := tx.Exec(ctx, `
			INSERT INTO domains (id, application_id, hostname, kind, status, zone, secret_id, detail, service) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			row.ID, row.ApplicationID, row.Hostname, row.Kind, row.Status, row.Zone, row.SecretID, row.Detail, row.Service,
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO installation (singleton, provider, account_id, server_id, public_url, method, cluster_id) VALUES (true, $1, $2, $3, $4, $5, $6)`,
		snap.Installation.Provider, snap.Installation.AccountID, snap.Installation.ServerID, snap.Installation.PublicURL, snap.Installation.Method, snap.Installation.ClusterID,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
