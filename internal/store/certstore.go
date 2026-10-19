package store

import (
	"context"
	"errors"
)

// CertificateStore is one Traefik certificate file, sealed.
type CertificateStore struct {
	Resolver   string
	Ciphertext []byte
	Nonce      []byte
	WrappedDEK []byte
}

func (s *Store) CertificateStores(ctx context.Context, ownerID string) ([]CertificateStore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT resolver, ciphertext, nonce, wrapped_dek FROM certificate_stores
		WHERE owner_id = $1 ORDER BY resolver`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CertificateStore
	for rows.Next() {
		var row CertificateStore
		if err := rows.Scan(&row.Resolver, &row.Ciphertext, &row.Nonce, &row.WrappedDEK); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) SaveCertificateStore(ctx context.Context, ownerID string, row CertificateStore) error {
	if ownerID == "" || row.Resolver == "" {
		return errors.New("magasin de certificats illisible")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO certificate_stores (owner_id, resolver, ciphertext, nonce, wrapped_dek, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (owner_id, resolver) DO UPDATE SET
			ciphertext = EXCLUDED.ciphertext, nonce = EXCLUDED.nonce,
			wrapped_dek = EXCLUDED.wrapped_dek, updated_at = now()`,
		ownerID, row.Resolver, row.Ciphertext, row.Nonce, row.WrappedDEK)
	return err
}

// SetRunDetail records why an application is not online yet, or clears it.
func (s *Store) SetRunDetail(ctx context.Context, applicationID, detail string) error {
	_, err := s.pool.Exec(ctx, `UPDATE applications SET run_detail = $2 WHERE id = $1 AND run_detail <> $2`, applicationID, detail)
	return err
}
