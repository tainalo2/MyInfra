package notify

import "testing"

func TestEnabledWithoutConfig(t *testing.T) {
	if Enabled() {
		t.Fatal("les alertes ne doivent pas être obligatoires au démarrage")
	}
}
