package cert

import "testing"

func TestEmailUsesTheSelectedZone(t *testing.T) {
	t.Setenv("MYINFRA_ACME_EMAIL", "")
	if Zone("site.devisgenerator.fr", "") != "devisgenerator.fr" {
		t.Fatal(Zone("site.devisgenerator.fr", ""))
	}
	if Email("devisgenerator.fr") != "certif@devisgenerator.fr" || Resolver("devisgenerator.fr") != "devisgenerator-fr" {
		t.Fatal(Email("devisgenerator.fr"), Resolver("devisgenerator.fr"))
	}
	if Zone("site.devisgenerator.fr", "Autre.FR") != "autre.fr" {
		t.Fatal("explicit")
	}
}

func TestEmailFromTheEnvironment(t *testing.T) {
	t.Setenv("MYINFRA_ACME_EMAIL", "ops@example.org")
	if Email("devisgenerator.fr") != "ops@example.org" {
		t.Fatal(Email("devisgenerator.fr"))
	}
	t.Setenv("MYINFRA_ACME_EMAIL", "pas une adresse")
	if Email("devisgenerator.fr") != "certif@devisgenerator.fr" {
		t.Fatal(Email("devisgenerator.fr"))
	}
}
