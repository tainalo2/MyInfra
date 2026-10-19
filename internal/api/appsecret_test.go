package api

import "testing"

func TestEntriesOK(t *testing.T) {
	if err := entriesOK(map[string]string{"TOKEN": "valeur"}); err != nil {
		t.Fatal(err)
	}
	if err := entriesOK(map[string]string{}); err == nil {
		t.Fatal("secret vide")
	}
	if err := entriesOK(map[string]string{"bad-key": "valeur"}); err == nil {
		t.Fatal("clé refusée")
	}
	if err := entriesOK(map[string]string{"TOKEN": "a\x00b"}); err == nil {
		t.Fatal("valeur refusée")
	}
}

func TestMergeSecretEnvAppWins(t *testing.T) {
	out := mergeSecretEnv(map[string]string{"TOKEN": "app"}, []map[string]string{{"TOKEN": "shared", "ONLY": "shared"}})
	if out["TOKEN"] != "app" || out["ONLY"] != "shared" {
		t.Fatalf("%v", out)
	}
}
