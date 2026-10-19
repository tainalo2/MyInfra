package store

import (
	"encoding/json"
	"testing"
)

func TestMergeKeepsRowsFromBothSides(t *testing.T) {
	local := SyncDoc{Users: []syncUser{{ID: "a", PasswordHash: "1", Seq: 1, Origin: "a"}}}
	remote := SyncDoc{Users: []syncUser{{ID: "b", PasswordHash: "2", Seq: 1, Origin: "b"}}}
	got := mergeDocs(local, remote)
	if len(got.Users) != 2 {
		t.Fatalf("%+v", got.Users)
	}
}

func TestMergeEmptyRemoteKeepsLocal(t *testing.T) {
	local := SyncDoc{Users: []syncUser{{ID: "a", PasswordHash: "1", Seq: 2, Origin: "a"}}}
	got := mergeDocs(local, SyncDoc{})
	if len(got.Users) != 1 || got.Users[0].ID != "a" {
		t.Fatalf("%+v", got.Users)
	}
}

func TestMergeHigherSeqWins(t *testing.T) {
	local := SyncDoc{Users: []syncUser{{ID: "a", PasswordHash: "old", Seq: 1, Origin: "a"}}}
	remote := SyncDoc{Users: []syncUser{{ID: "a", PasswordHash: "new", Seq: 2, Origin: "b"}}}
	got := mergeDocs(local, remote)
	if len(got.Users) != 1 || got.Users[0].PasswordHash != "new" {
		t.Fatalf("%+v", got.Users)
	}
}

func TestMergeSameSeqUsesOrigin(t *testing.T) {
	local := SyncDoc{Users: []syncUser{{ID: "a", PasswordHash: "left", Seq: 4, Origin: "a"}}}
	remote := SyncDoc{Users: []syncUser{{ID: "a", PasswordHash: "right", Seq: 4, Origin: "b"}}}
	got := mergeDocs(local, remote)
	if got.Users[0].PasswordHash != "right" {
		t.Fatalf("%+v", got.Users[0])
	}
}

func TestMergeTombRemovesRow(t *testing.T) {
	local := SyncDoc{Users: []syncUser{{ID: "a", PasswordHash: "x", Seq: 1, Origin: "a"}}}
	remote := SyncDoc{Tombs: []syncTomb{{Table: "users", ID: "a", Seq: 3, Origin: "b", Payload: json.RawMessage(`{"id":"a","passwordHash":"x","seq":1,"origin":"a"}`)}}}
	got := mergeDocs(local, remote)
	if len(got.Users) != 0 || len(got.Tombs) != 1 {
		t.Fatalf("users %+v tombs %+v", got.Users, got.Tombs)
	}
}

func TestMergeNewerChildRestoresParent(t *testing.T) {
	user := syncUser{ID: "u", PasswordHash: "p", Seq: 1, Origin: "a"}
	raw, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	local := SyncDoc{
		Users:   []syncUser{user},
		Secrets: []syncSecret{{ID: "s", OwnerID: "u", Kind: "ssh_key", Name: "k", Seq: 4, Origin: "b"}},
	}
	remote := SyncDoc{Tombs: []syncTomb{{Table: "users", ID: "u", Seq: 2, Origin: "a", Payload: raw}}}
	got := mergeDocs(local, remote)
	if len(got.Users) != 1 || got.Users[0].Seq != 4 {
		t.Fatalf("user %+v", got.Users)
	}
	if len(got.Secrets) != 1 {
		t.Fatal(got.Secrets)
	}
}

func TestMergeParentDeleteDropsOlderChild(t *testing.T) {
	user := syncUser{ID: "u", PasswordHash: "p", Seq: 1, Origin: "a"}
	raw, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	local := SyncDoc{
		Users:   []syncUser{user},
		Secrets: []syncSecret{{ID: "s", OwnerID: "u", Kind: "ssh_key", Name: "k", Seq: 1, Origin: "a"}},
	}
	remote := SyncDoc{Tombs: []syncTomb{{Table: "users", ID: "u", Seq: 5, Origin: "b", Payload: raw}}}
	got := mergeDocs(local, remote)
	if len(got.Users) != 0 || len(got.Secrets) != 0 {
		t.Fatalf("users %+v secrets %+v", got.Users, got.Secrets)
	}
	if len(got.Tombs) != 2 {
		t.Fatalf("%+v", got.Tombs)
	}
}

func TestHashIgnoresOrder(t *testing.T) {
	left := SyncDoc{Users: []syncUser{{ID: "b", Seq: 1, Origin: "a"}, {ID: "a", Seq: 1, Origin: "a"}}}
	right := SyncDoc{Users: []syncUser{{ID: "a", Seq: 1, Origin: "a"}, {ID: "b", Seq: 1, Origin: "a"}}}
	if left.Hash() != right.Hash() {
		t.Fatal(left.Hash(), right.Hash())
	}
}
