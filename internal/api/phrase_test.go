package api

import "testing"

func TestSelfPhrases(t *testing.T) {
	for _, phrase := range []string{
		"détruire myinfra",
		"destroy myinfra",
		"destruir myinfra",
		"distruggere myinfra",
		"myinfra zerstören",
	} {
		if !selfPhraseOK(phrase) {
			t.Fatalf("phrase refusée: %s", phrase)
		}
	}
	if selfPhraseOK("destroy") {
		t.Fatal("une phrase trop courte est acceptée")
	}
}
