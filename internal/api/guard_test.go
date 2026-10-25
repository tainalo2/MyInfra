package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForeignBrowser(t *testing.T) {
	same := &http.Request{Header: http.Header{"Origin": []string{"https://myinfra.example"}, "Sec-Fetch-Site": []string{"same-origin"}}, Host: "myinfra.example"}
	if foreignBrowser(same) {
		t.Fatal("same origin")
	}
	sibling := &http.Request{Header: http.Header{"Sec-Fetch-Site": []string{"same-site"}}, Host: "myinfra.example"}
	if !foreignBrowser(sibling) {
		t.Fatal("sibling site")
	}
	curl := &http.Request{Header: http.Header{}, Host: "myinfra.example"}
	if foreignBrowser(curl) {
		t.Fatal("curl")
	}
}

func TestTokenMatchIgnoresLength(t *testing.T) {
	if !tokenMatch("secret-token", "secret-token") {
		t.Fatal("match")
	}
	if tokenMatch("secret-token", "secret-tokex") || tokenMatch("x", "secret-token") {
		t.Fatal("mismatch")
	}
}

func TestReplicaDoesNotDriveMachines(t *testing.T) {
	reached := false
	handler := func(w http.ResponseWriter, r *http.Request) { reached = true }
	replica := &API{Replica: true}
	rec := httptest.NewRecorder()
	replica.drives(handler)(rec, httptest.NewRequest(http.MethodPost, "/api/transfer", nil))
	if reached || rec.Code != http.StatusConflict {
		t.Fatalf("replica reached=%v code=%d", reached, rec.Code)
	}
	main := &API{}
	main.drives(handler)(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/transfer", nil))
	if !reached {
		t.Fatal("main copy")
	}
}

func TestLonelyReplica(t *testing.T) {
	cases := []struct {
		replica, devKey bool
		peers           int
		want            bool
	}{
		{false, false, 0, false},
		{true, false, 0, true},
		{true, true, 2, true},
		{true, false, 1, false},
	}
	for _, c := range cases {
		if got := lonelyReplica(c.replica, c.devKey, c.peers); got != c.want {
			t.Fatalf("%+v: %v", c, got)
		}
	}
}

func TestLoginGateIsPerAddress(t *testing.T) {
	var gates gateSet
	attacker := gates.of("203.0.113.9")
	for i := 0; i < 6; i++ {
		if attacker.begin() {
			attacker.end(false)
		}
	}
	if attacker.begin() {
		attacker.end(false)
		t.Fatal("attaquant non freiné")
	}
	owner := gates.of("198.51.100.4")
	if !owner.begin() {
		t.Fatal("propriétaire bloqué")
	}
	owner.end(true)
}

func TestClientIPTrustsOnlyLocalProxies(t *testing.T) {
	direct := httptest.NewRequest(http.MethodPost, "/api/session", nil)
	direct.RemoteAddr = "203.0.113.9:4000"
	direct.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(direct); got != "203.0.113.9" {
		t.Fatal(got)
	}
	proxied := httptest.NewRequest(http.MethodPost, "/api/session", nil)
	proxied.RemoteAddr = "10.42.0.7:4000"
	proxied.Header.Set("X-Forwarded-For", "198.51.100.4, 10.42.0.1")
	if got := clientIP(proxied); got != "198.51.100.4" {
		t.Fatal(got)
	}
	pod := httptest.NewRequest(http.MethodPost, "/api/join", nil)
	pod.RemoteAddr = "10.42.0.7:4000"
	pod.Header.Set("X-Forwarded-For", "10.42.1.2")
	if publicClientIP(pod) != "" {
		t.Fatal(publicClientIP(pod))
	}
	if announcedIP("78.232.61.31") != "78.232.61.31" || !routableIP("78.232.61.31") || routableIP("10.42.1.2") {
		t.Fatal("adresse")
	}
}

func TestDirectAddress(t *testing.T) {
	if got, problem := directAddress("  "); got != "" || problem != "" {
		t.Fatalf("empty: %q %q", got, problem)
	}
	if got, problem := directAddress(" 203.0.113.10 "); got != "203.0.113.10" || problem != "" {
		t.Fatalf("public: %q %q", got, problem)
	}
	if got, _ := directAddress("192.168.1.20"); got != "192.168.1.20" {
		t.Fatalf("home network: %q", got)
	}
	for _, raw := range []string{"127.0.0.1", "0.0.0.0", "srv.example.com", "203.0.113.10:22", "1.2.3"} {
		if _, problem := directAddress(raw); problem == "" {
			t.Fatalf("%q is accepted", raw)
		}
	}
}

func TestCleanSSHUser(t *testing.T) {
	if got, problem := cleanSSHUser(" "); got != "root" || problem != "" {
		t.Fatalf("empty: %q %q", got, problem)
	}
	if got, problem := cleanSSHUser(" debian "); got != "debian" || problem != "" {
		t.Fatalf("debian: %q %q", got, problem)
	}
	for _, raw := range []string{"Debian", "deb ian", "a;b", "-x", "$(id)"} {
		if _, problem := cleanSSHUser(raw); problem == "" {
			t.Fatalf("%q is accepted", raw)
		}
	}
}
