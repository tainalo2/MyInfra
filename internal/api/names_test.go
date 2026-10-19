package api

import (
	"reflect"
	"testing"

	"myinfra/internal/store"
)

func hosts(names []store.Name) []string {
	var out []string
	for _, name := range names {
		out = append(out, name.Hostname)
	}
	return out
}

func TestNameChanges(t *testing.T) {
	current := []store.Name{{ID: "1", Hostname: "kuma.devisgenerator.fr"}, {ID: "2", Hostname: "old.devisgenerator.fr"}}
	wanted := []store.Name{{ID: "n1", Hostname: "kuma.devisgenerator.fr"}, {ID: "n2", Hostname: "status.devisgenerator.fr"}}
	add, remove := nameChanges(current, wanted)
	if !reflect.DeepEqual(hosts(add), []string{"status.devisgenerator.fr"}) {
		t.Fatalf("add = %v", hosts(add))
	}
	if !reflect.DeepEqual(hosts(remove), []string{"old.devisgenerator.fr"}) {
		t.Fatalf("remove = %v", hosts(remove))
	}
	// A kept name keeps its row: its record and route stay untouched.
	if remove[0].ID != "2" {
		t.Fatalf("removed name lost its id: %+v", remove[0])
	}
}

func TestNameChangesNothing(t *testing.T) {
	same := []store.Name{{Hostname: "a.devisgenerator.fr"}}
	add, remove := nameChanges(same, same)
	if len(add) != 0 || len(remove) != 0 {
		t.Fatalf("add=%v remove=%v", hosts(add), hosts(remove))
	}
	add, remove = nameChanges(same, nil)
	if len(add) != 0 || !reflect.DeepEqual(hosts(remove), []string{"a.devisgenerator.fr"}) {
		t.Fatalf("emptying: add=%v remove=%v", hosts(add), hosts(remove))
	}
}
