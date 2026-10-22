package auth

import (
	"testing"
	"time"
)

func TestTOTPValidate(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	code := hotp(secret, time.Now().Unix()/30)
	if !Validate(secret, code) {
		t.Fatal("expected match")
	}
}
