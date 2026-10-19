package exo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationMatchesDocumentedMessage(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	got, err := Authorization("POST", "https://api-ch-gva-2.exoscale.com/v2/instance", []byte(`{"name":"a"}`), "EXOkey", "secret", now)
	if err != nil {
		t.Fatal(err)
	}
	expires := "1700000300"
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte("POST /v2/instance\n{\"name\":\"a\"}\n\n\n" + expires))
	want := "EXO2-HMAC-SHA256 credential=EXOkey,expires=" + expires + ",signature=" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("\n%s\n%s", got, want)
	}
}

func TestAuthorizationSignsQueryValues(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	got, err := Authorization("GET", "https://x/v2/template?visibility=public&family=debian", nil, "k", "s", now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "signed-query-args=family;visibility,") {
		t.Fatal(got)
	}
	mac := hmac.New(sha256.New, []byte("s"))
	mac.Write([]byte("GET /v2/template\n\ndebianpublic\n\n1700000300"))
	if !strings.HasSuffix(got, "signature="+base64.StdEncoding.EncodeToString(mac.Sum(nil))) {
		t.Fatal(got)
	}
}

func TestHost(t *testing.T) {
	if Host("", "de-fra-1") != "https://api-de-fra-1.exoscale.com" || Host("http://x/", "z") != "http://x" || Host("", "") != "https://api-ch-gva-2.exoscale.com" {
		t.Fatal("hôtes")
	}
}
