package box

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// Box chiffre un secret avec une clé de données, puis enveloppe cette clé
// avec la clé maîtresse. La clé maîtresse ne va pas en base.
type Box struct {
	master []byte
}

type Sealed struct {
	Ciphertext []byte
	Nonce      []byte
	WrappedDEK []byte
}

func New(master []byte) (*Box, error) {
	if len(master) != 32 {
		return nil, errors.New("la clé maîtresse doit faire 32 octets")
	}
	return &Box{master: append([]byte(nil), master...)}, nil
}

func (b *Box) Seal(plaintext []byte) (Sealed, error) {
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return Sealed{}, err
	}
	wrapped, err := sealBlob(b.master, dek)
	if err != nil {
		return Sealed{}, err
	}
	nonce, ciphertext, err := sealParts(dek, plaintext)
	if err != nil {
		return Sealed{}, err
	}
	return Sealed{Ciphertext: ciphertext, Nonce: nonce, WrappedDEK: wrapped}, nil
}

func (b *Box) Open(s Sealed) ([]byte, error) {
	dek, err := openBlob(b.master, s.WrappedDEK)
	if err != nil {
		return nil, err
	}
	if len(dek) != 32 {
		return nil, errors.New("secret illisible")
	}
	return openParts(dek, s.Nonce, s.Ciphertext)
}

func sealBlob(key, plaintext []byte) ([]byte, error) {
	nonce, ciphertext, err := sealParts(key, plaintext)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(nonce)+len(ciphertext))
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

func openBlob(key, blob []byte) ([]byte, error) {
	if len(blob) < 12 {
		return nil, errors.New("secret illisible")
	}
	return openParts(key, blob[:12], blob[12:])
}

func sealParts(key, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, nil), nil
}

func openParts(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secret illisible")
	}
	return plain, nil
}
