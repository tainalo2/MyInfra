package assistant

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
)

// KeyStore keeps provider API keys encrypted at rest (AES-256-GCM). Plain keys
// are never returned by List; Get is for adapters only.
type KeyStore struct {
	mu   sync.RWMutex
	aead cipher.AEAD
	enc  map[string]string // provider -> base64(nonce||ciphertext)
}

// NewKeyStore needs a 32-byte master key.
func NewKeyStore(master []byte) (*KeyStore, error) {
	if len(master) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	b, err := aes.NewCipher(master)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &KeyStore{aead: g, enc: map[string]string{}}, nil
}

func (s *KeyStore) Set(provider, apiKey string) error {
	if provider == "" || apiKey == "" {
		return errors.New("provider and key required")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ct := s.aead.Seal(nonce, nonce, []byte(apiKey), []byte(provider))
	s.mu.Lock()
	s.enc[provider] = base64.StdEncoding.EncodeToString(ct)
	s.mu.Unlock()
	return nil
}

func (s *KeyStore) Get(provider string) (string, bool) {
	s.mu.RLock()
	v, ok := s.enc[provider]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return "", false
	}
	n := s.aead.NonceSize()
	pt, err := s.aead.Open(nil, raw[:n], raw[n:], []byte(provider))
	if err != nil {
		return "", false
	}
	return string(pt), true
}

func (s *KeyStore) Delete(provider string) { s.mu.Lock(); delete(s.enc, provider); s.mu.Unlock() }

// List returns configured provider names only.
func (s *KeyStore) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{}
	for k := range s.enc {
		out = append(out, k)
	}
	return out
}
