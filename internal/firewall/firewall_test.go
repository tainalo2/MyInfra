package firewall

import (
	"strings"
	"testing"
)

func TestScriptClosesClusterPortsOnASingleMachine(t *testing.T) {
	script, err := Script(Spec{})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"policy drop",
		"tcp dport { 22, 80, 443 } accept",
		"LLMNR=no",
		"PasswordAuthentication no",
		"PermitRootLogin prohibit-password",
		"nft -c -f /etc/myinfra/firewall.nft",
		"ufw --force disable",
		"while [ \"$i\" -lt 30 ]",
		"apt-get install -y nftables",
		"nftables n'a pas pu être installé.",
	} {
		if !strings.Contains(script, part) {
			t.Fatalf("missing %s", part)
		}
	}
	for _, closed := range []string{"6443", "10250", "2379", "8472", "51820", "flush ruleset"} {
		if strings.Contains(script, closed) {
			t.Fatalf("single machine still mentions %s", closed)
		}
	}
}

func TestScriptAllowsClusterPortsOnlyFromPeers(t *testing.T) {
	script, err := Script(Spec{
		Peers:         []string{"10.1.0.2", " 10.0.0.1 "},
		ControlPlanes: []string{"10.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "elements = { 10.0.0.1, 10.1.0.2 }") {
		t.Fatal(script)
	}
	for _, part := range []string{
		"ip saddr @cluster tcp dport { 6443, 10250 } accept",
		"ip saddr @cluster udp dport { 8472, 51820 } accept",
		"ip saddr @control tcp dport { 2379, 2380 } accept",
		"10.42.0.0/16",
		"flannel.1",
		"iifname \"myinfra\"",
	} {
		if !strings.Contains(script, part) {
			t.Fatalf("missing %s", part)
		}
	}
	if strings.Contains(script, "10.1.0.2, 10.0.0.1") {
		t.Fatal("peers are not sorted")
	}
}

func TestScriptRejectsAnUnreadableAddress(t *testing.T) {
	_, err := Script(Spec{Peers: []string{"10.0.0.1 }; flush ruleset"}})
	if err == nil || !strings.Contains(err.Error(), "illisible") {
		t.Fatal(err)
	}
}
