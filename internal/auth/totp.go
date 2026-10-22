package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var base32Decoder = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret creates a base32 secret for TOTP enrollment.
func GenerateSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(buf), "="), nil
}

// ProvisioningURI builds the otpauth URL scanned by authenticator apps.
func ProvisioningURI(issuer, account, secret string) string {
	name := strings.TrimSpace(account)
	if name == "" {
		name = issuer
	}
	label := url.QueryEscape(issuer + ":" + name)
	label = strings.ReplaceAll(label, "+", "%20")
	secret = strings.TrimSpace(strings.ReplaceAll(secret, " ", ""))
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// NormalizeTOTPCode keeps only digits (spaces or copy-paste artefacts are dropped).
func NormalizeTOTPCode(code string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(code) {
		if r >= '0' && r <= '9' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Validate checks a 6-digit code for the secret at the current time.
func Validate(secret, code string) bool {
	secret = strings.TrimSpace(secret)
	code = NormalizeTOTPCode(code)
	if secret == "" || len(code) != 6 {
		return false
	}
	now := time.Now().Unix() / 30
	for skew := -2; skew <= 2; skew++ {
		if code == hotp(secret, now+int64(skew)) {
			return true
		}
	}
	return false
}

func hotp(secret string, counter int64) string {
	key, err := decodeSecret(secret)
	if err != nil {
		return ""
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	val := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", val%1000000)
}

func decodeSecret(secret string) ([]byte, error) {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	secret = strings.TrimRight(secret, "=")
	if pad := len(secret) % 8; pad != 0 {
		secret += strings.Repeat("=", 8-pad)
	}
	return base32Decoder.DecodeString(secret)
}
