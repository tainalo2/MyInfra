package market

import "testing"

// Every starter names existing articles that need nothing typed, and keeps
// an odd number of control planes.
func TestStartersAreComplete(t *testing.T) {
	seen := map[string]bool{}
	clusters := map[string]bool{}
	for _, starter := range starters {
		if seen[starter.ID] || clusters[starter.Cluster] {
			t.Fatalf("%s en double", starter.ID)
		}
		seen[starter.ID], clusters[starter.Cluster] = true, true
		planes := 0
		for _, server := range starter.Servers {
			if server.ControlPlane {
				planes++
			}
		}
		if planes%2 == 0 {
			t.Fatalf("%s : %d plans de contrôle", starter.ID, planes)
		}
		if len(starter.Apps) == 0 {
			t.Fatalf("%s sans application", starter.ID)
		}
		for _, id := range starter.Apps {
			article, ok := find(id)
			if !ok {
				t.Fatalf("%s : article %s introuvable", starter.ID, id)
			}
			if len(article.Inputs) > 0 {
				t.Fatalf("%s : %s demande une saisie", starter.ID, id)
			}
			if _, _, err := Expand(id, id+".exemple.fr", nil); err != nil {
				t.Fatalf("%s : %s : %v", starter.ID, id, err)
			}
		}
	}
}
