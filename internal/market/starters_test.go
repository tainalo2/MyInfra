package market

import "testing"

// The starters of the built-in library name existing packages that need
// nothing typed; the library checks the rest when it is read.
func TestStartersAreComplete(t *testing.T) {
	starters := Starters()
	if len(starters) == 0 {
		t.Fatal("aucun starter")
	}
	for _, starter := range starters {
		if starter.Name == "" || starter.Translations["en"].Name == "" || starter.Version == "" {
			t.Fatalf("%+v", starter)
		}
		for _, app := range starter.Apps {
			if _, _, err := Expand(app.ID, app.Subdomain+".exemple.fr", nil); err != nil {
				t.Fatalf("%s : %s : %v", starter.ID, app.ID, err)
			}
		}
	}
}
