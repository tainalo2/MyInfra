package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestProvisioningURIRoundTrip(t *testing.T) {
	issuer := "MyInfra"
	for _, account := range []string{"admin", "jean.dupont", ""} {
		secret, err := GenerateSecret()
		if err != nil {
			t.Fatal(err)
		}
		raw := ProvisioningURI(issuer, account, secret)
		key, err := otp.NewKeyFromURL(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if key.Secret() != secret {
			t.Fatalf("secret mismatch: got %q want %q", key.Secret(), secret)
		}
		code, err := totp.GenerateCode(key.Secret(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !Validate(secret, code) {
			t.Fatalf("validate failed for %q", raw)
		}
	}
}
