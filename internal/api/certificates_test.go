package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func testCertificate(t *testing.T, name string, end time.Time) map[string]any {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(end.Unix()), Subject: pkix.Name{CommonName: name}, NotBefore: end.Add(-90 * 24 * time.Hour), NotAfter: end, DNSNames: []string{name}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return map[string]any{"domain": map[string]any{"main": name}, "certificate": certPEM, "key": []byte("clé"), "Store": "default"}
}

func testFile(t *testing.T, account string, certs ...map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"devisgenerator-fr": map[string]any{"Account": map[string]string{"Email": account}, "Certificates": certs}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMergeCertificatesKeepsTheLongestAndDropsExpired(t *testing.T) {
	now := time.Now()
	kept := testFile(t, "ancien",
		testCertificate(t, "kuma.devisgenerator.fr", now.Add(80*24*time.Hour)),
		testCertificate(t, "vieux.devisgenerator.fr", now.Add(-time.Hour)),
	)
	fresh := testFile(t, "nouveau",
		testCertificate(t, "kuma.devisgenerator.fr", now.Add(10*24*time.Hour)),
		testCertificate(t, "site.devisgenerator.fr", now.Add(60*24*time.Hour)),
	)
	merged, err := mergeCertificates(kept, fresh, now)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]certificateResolver
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatal(err)
	}
	part := doc["devisgenerator-fr"]
	if !strings.Contains(string(part.Account), "nouveau") {
		t.Fatalf("compte : %s", part.Account)
	}
	ends := map[string]time.Time{}
	for _, raw := range part.Certificates {
		name, end := certificateOf(raw)
		ends[name] = end
	}
	if len(ends) != 2 {
		t.Fatalf("certificats : %v", ends)
	}
	if ends["kuma.devisgenerator.fr"].Before(now.Add(70 * 24 * time.Hour)) {
		t.Fatal("le certificat le plus durable doit rester")
	}
	if _, ok := ends["vieux.devisgenerator.fr"]; ok {
		t.Fatal("un certificat expiré doit partir")
	}
	again, _ := mergeCertificates(merged, merged, now)
	if string(again) != string(merged) {
		t.Fatal("la fusion doit être stable, sinon chaque passage réécrit la base")
	}
}

func TestParseCertificateFiles(t *testing.T) {
	raw := []byte(`{"a":{}}`)
	out := "acme-devisgenerator-fr.json " + base64.StdEncoding.EncodeToString(raw) + "\n../etc/passwd " + base64.StdEncoding.EncodeToString(raw) + "\nacme-x.json pasdubase64\n"
	files := parseCertificateFiles(out)
	if len(files) != 1 || string(files["acme-devisgenerator-fr.json"]) != string(raw) {
		t.Fatalf("%v", files)
	}
}

func TestParseCertificateErrorsExplainsTheRateLimit(t *testing.T) {
	line := `2026-10-19T09:27:13Z ERR Unable to obtain ACME certificate for domains error="unable to generate a certificate for the domains [kuma.devisgenerator.fr]: acme: error: 429 :: POST :: https://acme-v02.api.letsencrypt.org/acme/new-order :: urn:ietf:params:acme:error:rateLimited :: too many certificates (5) already issued for this exact set of identifiers in the last 168h0m0s, retry after 2026-10-20 02:42:08 UTC: see https://letsencrypt.org/docs/rate-limits/"`
	found := parseCertificateErrors(line+"\n", []string{"kuma.devisgenerator.fr", "site.devisgenerator.fr"})
	trouble, ok := found["kuma.devisgenerator.fr"]
	if !ok || !strings.Contains(trouble.Detail, "5 cette semaine") {
		t.Fatalf("%+v", found)
	}
	if want := time.Date(2026, 10, 20, 2, 42, 8, 0, time.UTC); !trouble.Retry.Equal(want) {
		t.Fatalf("réessai %v", trouble.Retry)
	}
	if _, ok := found["site.devisgenerator.fr"]; ok {
		t.Fatal("un autre nom n'est pas concerné")
	}
}
