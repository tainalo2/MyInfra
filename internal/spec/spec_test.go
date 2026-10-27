package spec

import (
	"strings"
	"testing"
)

func TestCheckExposedPorts(t *testing.T) {
	ok := Service{Ports: []Port{
		{Port: 21116, Expose: 21116},
		{Port: 21116, Protocol: "udp", Expose: 21116},
		{Port: 51820, Protocol: "udp", Expose: 51821},
	}}
	if problems := Check(ok); len(problems) > 0 {
		t.Fatal(problems)
	}
	for _, bad := range []Service{
		{Ports: []Port{{Port: 53, Protocol: "sctp"}}},
		{Ports: []Port{{Port: 51820, Protocol: "udp", Expose: 51820}}},
		{Ports: []Port{{Port: 2222, Expose: 22}}},
		{Ports: []Port{{Port: 8443, Expose: 443}}},
		{Ports: []Port{{Port: 1000, Expose: 70000}}},
		{Ports: []Port{{Port: 1000, Expose: 9000}, {Port: 1001, Expose: 9000}}},
		{Ports: []Port{{Port: 1000}, {Port: 1000, Protocol: "tcp"}}},
		{Ports: []Port{{Name: "u53", Port: 53}}},
	} {
		if len(Check(bad)) == 0 {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if got := Exposed(ok); len(got) != 3 || got[1].Proto() != "udp" {
		t.Fatal(got)
	}
	// 51820/udp is the cluster's own WireGuard; tcp is free.
	if Reserved(51820, "tcp") || !Reserved(51820, "udp") || !strings.Contains(Check(Service{Ports: []Port{{Port: 1, Expose: 6443}}})[0].Message, "6443/tcp") {
		t.Fatal("reserved ports")
	}
}
