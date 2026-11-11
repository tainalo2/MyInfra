package store

import "context"

// AssistantKeys returns the sealed BYOK provider keys, by provider, as
// KeyStore.Export gave them. Nothing in them opens without the BYOK master key.
func (s *Store) AssistantKeys(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT provider, sealed FROM assistant_keys`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var provider, sealed string
		if err := rows.Scan(&provider, &sealed); err != nil {
			return nil, err
		}
		out[provider] = sealed
	}
	return out, rows.Err()
}

// PutAssistantKey keeps the sealed key of one provider, unchanged.
func (s *Store) PutAssistantKey(ctx context.Context, provider, sealed string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO assistant_keys (provider, sealed) VALUES ($1, $2)
		ON CONFLICT (provider) DO UPDATE SET sealed = EXCLUDED.sealed, updated_at = now()`,
		provider, sealed)
	return err
}

// DeleteAssistantKey forgets the key of one provider. A missing one is no error.
func (s *Store) DeleteAssistantKey(ctx context.Context, provider string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM assistant_keys WHERE provider = $1`, provider)
	return err
}
