package auth

import "testing"

func TestNormalizeTOTPCode(t *testing.T) {
	if NormalizeTOTPCode("123 456") != "123456" {
		t.Fatal("spaces")
	}
	if NormalizeTOTPCode("12-34-56") != "123456" {
		t.Fatal("dashes")
	}
}
