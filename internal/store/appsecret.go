package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

type AppSecret struct {
	ID         string
	OwnerID    string
	Name       string
	Ciphertext []byte
	Nonce      []byte
	WrappedDEK []byte
	Apps       []string
}

type AppRef struct {
	ID   string
	Name string
}

func (s *Store) ListAppSecrets(ctx context.Context, ownerID string) ([]AppSecret, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, name, ciphertext, nonce, wrapped_dek, apps
		FROM app_secrets WHERE owner_id = $1 ORDER BY name`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppSecret
	for rows.Next() {
		row, err := scanAppSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) AppSecret(ctx context.Context, ownerID, id string) (AppSecret, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, name, ciphertext, nonce, wrapped_dek, apps
		FROM app_secrets WHERE owner_id = $1 AND id = $2`, ownerID, id)
	if err != nil {
		return AppSecret{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return AppSecret{}, err
		}
		return AppSecret{}, ErrNotFound
	}
	return scanAppSecret(rows)
}

func (s *Store) GrantedAppSecrets(ctx context.Context, ownerID, appID string) ([]AppSecret, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, name, ciphertext, nonce, wrapped_dek, apps
		FROM app_secrets
		WHERE owner_id = $1 AND apps @> jsonb_build_array($2::text)
		ORDER BY name`, ownerID, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppSecret
	for rows.Next() {
		row, err := scanAppSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) AppSecretEntries(ctx context.Context, appID string) ([]AppSecret, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.owner_id, s.name, s.ciphertext, s.nonce, s.wrapped_dek, s.apps
		FROM app_secrets s
		JOIN applications a ON a.owner_id = s.owner_id
		WHERE a.id = $1 AND s.apps @> jsonb_build_array($1::text)
		ORDER BY s.name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppSecret
	for rows.Next() {
		row, err := scanAppSecret(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) CreateAppSecret(ctx context.Context, row AppSecret) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_secrets (id, owner_id, name, ciphertext, nonce, wrapped_dek, apps)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		row.ID, row.OwnerID, row.Name, row.Ciphertext, row.Nonce, row.WrappedDEK, encodeApps(row.Apps))
	return nameTaken(err)
}

func (s *Store) UpdateAppSecret(ctx context.Context, row AppSecret) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE app_secrets
		SET name = $3, ciphertext = $4, nonce = $5, wrapped_dek = $6, apps = $7
		WHERE id = $1 AND owner_id = $2`,
		row.ID, row.OwnerID, row.Name, row.Ciphertext, row.Nonce, row.WrappedDEK, encodeApps(row.Apps))
	if err != nil {
		return nameTaken(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAppSecret(ctx context.Context, ownerID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM app_secrets WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ApplicationRefs(ctx context.Context, ownerID string) ([]AppRef, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM applications WHERE owner_id = $1 ORDER BY name`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppRef
	for rows.Next() {
		var ref AppRef
		if err := rows.Scan(&ref.ID, &ref.Name); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (s *Store) ForgetAppSecretGrant(ctx context.Context, appID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE app_secrets
		SET apps = (
			SELECT COALESCE(jsonb_agg(elem), '[]'::jsonb)
			FROM jsonb_array_elements(apps) AS elem
			WHERE elem <> to_jsonb($1::text)
		)
		WHERE apps @> jsonb_build_array($1::text)`, appID)
	return err
}

type appSecretScanner interface {
	Scan(dest ...any) error
}

func scanAppSecret(row appSecretScanner) (AppSecret, error) {
	var out AppSecret
	var apps []byte
	if err := row.Scan(&out.ID, &out.OwnerID, &out.Name, &out.Ciphertext, &out.Nonce, &out.WrappedDEK, &apps); err != nil {
		return AppSecret{}, err
	}
	out.Apps = decodeApps(apps)
	return out, nil
}

func encodeApps(ids []string) []byte {
	if ids == nil {
		ids = []string{}
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return []byte("[]")
	}
	return raw
}

func decodeApps(raw []byte) []string {
	var ids []string
	if json.Unmarshal(raw, &ids) != nil || ids == nil {
		return []string{}
	}
	return ids
}

func nameTaken(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrNameTaken
	}
	return err
}
