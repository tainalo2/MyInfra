package api

import (
	"net"
	"testing"
)

func TestReachBaseUsesLANWhenPublicIsLoopback(t *testing.T) {
	if got := reachBase("http://127.0.0.1:8080", "192.168.1.20"); got != "http://192.168.1.20:8080" {
		t.Fatal(got)
	}
	if got := reachBase("https://myinfra.example", "192.168.1.20"); got != "https://myinfra.example" {
		t.Fatal(got)
	}
	if got := reachBase("http://127.0.0.1:8080", ""); got != "http://127.0.0.1:8080" {
		t.Fatal(got)
	}
}

func TestPickLANPrefersHomeNetwork(t *testing.T) {
	got := pickLAN([]net.IP{
		net.ParseIP("172.17.0.1"),
		net.ParseIP("10.8.0.2"),
		net.ParseIP("192.168.1.20"),
		net.ParseIP("8.8.8.8"),
	})
	if got != "192.168.1.20" {
		t.Fatal(got)
	}
}
