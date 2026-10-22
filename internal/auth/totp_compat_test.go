package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestTOTPAgreesWithPquerna(t *testing.T) {
	for i := 0; i < 50; i++ {
		secret, err := GenerateSecret()
		if err != nil {
			t.Fatal(err)
		}
		code, err := totp.GenerateCode(secret, time.Now())
		if err != nil {
			t.Fatalf("generate %q: %v", secret, err)
		}
		if !Validate(secret, code) {
			t.Fatalf("validate failed secret=%q code=%q", secret, code)
		}
	}
}
