package api

import "testing"

func TestGPUConflictNamesTheHolder(t *testing.T) {
	if got := gpuConflict(1, map[string]int{"OBS": 1}, 1); got != "Le GPU est déjà utilisé par OBS." {
		t.Fatal(got)
	}
	if got := gpuConflict(2, map[string]int{"OBS": 1, "Encode": 1}, 1); got != "Le GPU est déjà utilisé par Encode et OBS." {
		t.Fatal(got)
	}
	if got := gpuConflict(2, map[string]int{"OBS": 1}, 1); got != "" {
		t.Fatal(got)
	}
	if got := gpuConflict(0, nil, 1); got != "Aucune machine ici n'a de GPU." {
		t.Fatal(got)
	}
	if got := gpuConflict(1, nil, 2); got != "Il n'y a pas assez de GPU pour cette application." {
		t.Fatal(got)
	}
	if got := gpuConflict(0, map[string]int{"OBS": 1}, 0); got != "" {
		t.Fatal(got)
	}
}
