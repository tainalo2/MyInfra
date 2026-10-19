package store

import (
	"net"
	"testing"
)

func TestNormalizePeerURLRejectsUnsafeTargets(t *testing.T) {
	for _, raw := range []string{
		"http://example.com",
		"http://169.254.42.42",
		"https://169.254.169.254/latest",
		"https://user:secret@example.com",
		"ftp://example.com",
		"https://metadata.google.internal",
	} {
		if _, err := NormalizePeerURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{
		"https://myinfra.example",
		"http://127.0.0.1:8080",
		"http://10.1.2.3:8080",
		"http://[::1]:8080",
	} {
		if _, err := NormalizePeerURL(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}

func TestPeerIPAllowedBlocksLinkLocal(t *testing.T) {
	if PeerIPAllowed(net.ParseIP("169.254.42.42")) || PeerIPAllowed(net.ParseIP("fe80::1")) {
		t.Fatal("link local")
	}
	if !PeerIPAllowed(net.ParseIP("78.232.61.31")) || !PeerIPAllowed(net.ParseIP("10.0.0.8")) || !PeerIPAllowed(net.ParseIP("127.0.0.1")) {
		t.Fatal("usable address")
	}
}
