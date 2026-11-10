package assistant

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sort"
	"sync"
)

// KeyStore keeps provider API keys encrypted at rest (AES-256-GCM). Plain keys
// are never returned by List; Get is for adapters only.
//
// Each entry is base64(nonce||ciphertext), sealed with the provider name as
// associated data. Export and Import carry entries in this exact form: any
// persistence must keep it unchanged, and Rekey reads it to rotate the
// master key.
type KeyStore struct {
	mu   sync.RWMutex
	aead cipher.AEAD
	enc  map[string]string // provider -> base64(nonce||ciphertext)
}

// NewKeyStore needs a 32-byte master key.
func NewKeyStore(master []byte) (*KeyStore, error) {
	g, err := newAEAD(master)
	if err != nil {
		return nil, err
	}
	return &KeyStore{aead: g, enc: map[string]string{}}, nil
}

func newAEAD(master []byte) (cipher.AEAD, error) {
	if len(master) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	b, err := aes.NewCipher(master)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func (s *KeyStore) Set(provider, apiKey string) error {
	if provider == "" || apiKey == "" {
		return errors.New("provider and key required")
	}
	sealed, err := seal(s.aead, provider, apiKey)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.enc[provider] = sealed
	s.mu.Unlock()
	return nil
}

func seal(aead cipher.AEAD, provider, apiKey string) (string, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nonce, nonce, []byte(apiKey), []byte(provider))
	return base64.StdEncoding.EncodeToString(ct), nil
}

func open(aead cipher.AEAD, provider, sealed string) (string, bool) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < aead.NonceSize() {
		return "", false
	}
	n := aead.NonceSize()
	pt, err := aead.Open(nil, raw[:n], raw[n:], []byte(provider))
	if err != nil {
		return "", false
	}
	return string(pt), true
}

func (s *KeyStore) Get(provider string) (string, bool) {
	s.mu.RLock()
	v, ok := s.enc[provider]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	return open(s.aead, provider, v)
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
	sort.Strings(out)
	return out
}

// Export returns the sealed entries, as they are to be kept at rest. Nothing
// in them opens without the master key.
func (s *KeyStore) Export() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.enc))
	for k, v := range s.enc {
		out[k] = v
	}
	return out
}

// Import loads sealed entries kept at rest, as Export gave them. Entries are
// taken as they are: an entry sealed with another master key stays, and
// Get fails on it until Rekey seals it again.
func (s *KeyStore) Import(entries map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range entries {
		if k != "" && v != "" {
			s.enc[k] = v
		}
	}
}

// Rekey seals again, with the current master key, every entry that the
// current key does not open but the previous one does. It returns the
// providers sealed again, and those neither key opens: those are lost, and
// their users must enter their key again.
func (s *KeyStore) Rekey(previous []byte) (rekeyed, lost []string, err error) {
	old, err := newAEAD(previous)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for provider, sealed := range s.enc {
		if _, ok := open(s.aead, provider, sealed); ok {
			continue
		}
		plain, ok := open(old, provider, sealed)
		if !ok {
			lost = append(lost, provider)
			continue
		}
		again, err := seal(s.aead, provider, plain)
		if err != nil {
			return rekeyed, lost, err
		}
		s.enc[provider] = again
		rekeyed = append(rekeyed, provider)
	}
	sort.Strings(rekeyed)
	sort.Strings(lost)
	return rekeyed, lost, nil
}
