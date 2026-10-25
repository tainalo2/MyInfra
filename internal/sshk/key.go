package sshk

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

type Pair struct {
	Public      string
	Private     string
	Fingerprint string
}

func Generate() (Pair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Pair{}, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return Pair{}, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "myinfra")
	if err != nil {
		return Pair{}, err
	}
	return Pair{
		Public:      strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))),
		Private:     string(pem.EncodeToMemory(block)),
		Fingerprint: ssh.FingerprintSHA256(sshPub),
	}, nil
}

func ParsePublic(authorized string) (string, string, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(authorized)))
	if err != nil {
		return "", "", fmt.Errorf("clé publique illisible")
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), ssh.FingerprintSHA256(key), nil
}

// ErrPassphrase is a private key locked by a passphrase: MyInfra opens
// sessions alone and could not unlock it.
var ErrPassphrase = errors.New("clé privée protégée par une phrase de passe")

// ParsePrivate reads an imported private key and gives its public half.
func ParsePrivate(private string) (Pair, error) {
	private = strings.TrimSpace(private) + "\n"
	signer, err := ssh.ParsePrivateKey([]byte(private))
	if err != nil {
		var locked *ssh.PassphraseMissingError
		if errors.As(err, &locked) {
			return Pair{}, ErrPassphrase
		}
		return Pair{}, fmt.Errorf("clé privée illisible")
	}
	pub := signer.PublicKey()
	return Pair{
		Public:      strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))),
		Private:     private,
		Fingerprint: ssh.FingerprintSHA256(pub),
	}, nil
}

func Hint(secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) <= 4 {
		return secret
	}
	return secret[len(secret)-4:]
}
