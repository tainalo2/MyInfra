package dnsplan

import (
	"strings"
	"testing"

	"myinfra/internal/dns"
)

var yes = true

var zone = []dns.Entry{
	{Name: "moydomain.name", Type: "A", Content: "203.0.113.1"},
	{Name: "moydomain.name", Type: "MX", Content: "mx1.old.net", Priority: 10},
	{Name: "moydomain.name", Type: "TXT", Content: "v=spf1 include:old.net -all"},
	{Name: "www.moydomain.name", Type: "A", Content: "203.0.113.1", TTL: 300, Proxied: &yes},
	{Name: "blog.moydomain.name", Type: "CNAME", Content: "ghost.example.org"},
	{Name: "*.dev.moydomain.name", Type: "A", Content: "203.0.113.9"},
}

var managed = map[string]string{"cloud.moydomain.name": "nextcloud"}

func plan(c Change) Plan {
	c.Zone = "moydomain.name"
	return Make(c, zone, managed)
}

func TestFreeNameNeedsNoAgreement(t *testing.T) {
	p := plan(Change{Name: "docs.moydomain.name", Type: "CNAME", Values: []dns.Value{{Content: "MonOrg.github.io."}}})
	if p.Refused != "" || p.Confirm || len(p.After) != 1 || p.After[0].Content != "monorg.github.io" {
		t.Fatalf("%+v", p)
	}
}

func TestReplacingAsksAndKeepsProxy(t *testing.T) {
	p := plan(Change{Name: "www.moydomain.name", Type: "A", Values: []dns.Value{{Content: "198.51.100.7"}}})
	if !p.Confirm || len(p.Removes) != 1 || p.After[0].Proxied == nil || !*p.After[0].Proxied {
		t.Fatalf("%+v", p)
	}
	recap := p.Recap()
	if !strings.Contains(recap, "Avant :\n  A 203.0.113.1 (TTL 300 s) [proxy Cloudflare]") || !strings.Contains(recap, "Après :\n  A 198.51.100.7") {
		t.Fatal(recap)
	}
	if same := plan(Change{Name: "www.moydomain.name", Type: "A", Values: []dns.Value{{Content: "203.0.113.1"}}}); !same.Noop || same.Confirm {
		t.Fatalf("%+v", same)
	}
}

func TestDeletingAsks(t *testing.T) {
	p := plan(Change{Name: "blog.moydomain.name", Type: "CNAME", Delete: true})
	if !p.Confirm || len(p.Removes) != 1 || len(p.After) != 0 {
		t.Fatalf("%+v", p)
	}
	if p := plan(Change{Name: "none.moydomain.name", Type: "A", Delete: true}); p.Refused == "" {
		t.Fatal("rien à supprimer")
	}
}

func TestCNAMEStandsAlone(t *testing.T) {
	// A CNAME where an A is: the A goes, with agreement.
	p := plan(Change{Name: "www.moydomain.name", Type: "CNAME", Values: []dns.Value{{Content: "site.netlify.app"}}})
	if !p.Confirm || len(p.Removes) != 1 || p.Removes[0].Type != "A" || len(p.After) != 1 {
		t.Fatalf("%+v", p)
	}
	// An A where a CNAME is: the CNAME goes.
	p = plan(Change{Name: "blog.moydomain.name", Type: "A", Values: []dns.Value{{Content: "198.51.100.7"}}})
	if !p.Confirm || len(p.Removes) != 1 || p.Removes[0].Type != "CNAME" {
		t.Fatalf("%+v", p)
	}
	if p := plan(Change{Name: "moydomain.name", Type: "CNAME", Values: []dns.Value{{Content: "x.net"}}}); p.Refused == "" {
		t.Fatal("CNAME à la racine")
	}
}

func TestWildcardIsShadowed(t *testing.T) {
	p := plan(Change{Name: "api.dev.moydomain.name", Type: "A", Values: []dns.Value{{Content: "198.51.100.7"}}})
	if !p.Confirm || !strings.Contains(strings.Join(p.Impacts, " "), "*.dev.moydomain.name") {
		t.Fatalf("%+v", p)
	}
}

func TestSensitiveNamesAsk(t *testing.T) {
	for label, c := range map[string]Change{
		"mx":    {Name: "moydomain.name", Type: "MX", Values: []dns.Value{{Content: "mail.protonmail.ch", Priority: 10}}},
		"spf":   {Name: "moydomain.name", Type: "TXT", Values: []dns.Value{{Content: "v=spf1 include:_spf.protonmail.ch -all"}}},
		"dkim":  {Name: "protonmail._domainkey.moydomain.name", Type: "CNAME", Values: []dns.Value{{Content: "x.protonmail.ch"}}},
		"dmarc": {Name: "_dmarc.moydomain.name", Type: "TXT", Values: []dns.Value{{Content: "v=DMARC1; p=none"}}},
		"acme":  {Name: "_acme-challenge.moydomain.name", Type: "TXT", Values: []dns.Value{{Content: "token"}}},
	} {
		if p := plan(c); p.Refused != "" || !p.Confirm || len(p.Impacts) == 0 {
			t.Fatalf("%s : %+v", label, p)
		}
	}
}

func TestRefusals(t *testing.T) {
	for label, c := range map[string]Change{
		"géré":     {Name: "cloud.moydomain.name", Type: "A", Values: []dns.Value{{Content: "198.51.100.7"}}},
		"ns":       {Name: "moydomain.name", Type: "NS", Values: []dns.Value{{Content: "ns1.x.net"}}},
		"srv":      {Name: "_sip._tcp.moydomain.name", Type: "SRV", Values: []dns.Value{{Content: "x"}}},
		"ipv4":     {Name: "x.moydomain.name", Type: "A", Values: []dns.Value{{Content: "2001:db8::1"}}},
		"ipv6":     {Name: "x.moydomain.name", Type: "AAAA", Values: []dns.Value{{Content: "203.0.113.1"}}},
		"hors":     {Name: "x.other.org", Type: "A", Values: []dns.Value{{Content: "203.0.113.1"}}},
		"vide":     {Name: "x.moydomain.name", Type: "TXT"},
		"ttl":      {Name: "x.moydomain.name", Type: "A", Values: []dns.Value{{Content: "203.0.113.1"}}, TTL: 5},
		"doublons": {Name: "x.moydomain.name", Type: "A", Values: []dns.Value{{Content: "203.0.113.1"}, {Content: "203.0.113.01"}}},
	} {
		if p := plan(c); p.Refused == "" {
			t.Fatalf("%s accepté : %+v", label, p)
		}
	}
	// A TXT on a name MyInfra uses is fine: its addresses stay.
	p := plan(Change{Name: "cloud.moydomain.name", Type: "TXT", Values: []dns.Value{{Content: "google-site-verification=x"}}})
	if p.Refused != "" || p.Confirm {
		t.Fatalf("%+v", p)
	}
}
