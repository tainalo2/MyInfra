package store

import (
	"encoding/json"
	"testing"
	"time"
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

func TestMergeUpdatesAndJobs(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	local := SyncDoc{
		Updates: []syncUpdate{{
			Kind: "server", TargetID: "s1", OwnerID: "u", Current: "1", Available: "2", Level: "patch",
			Detail: json.RawMessage(`{}`), CheckedAt: when, Seq: 1, Origin: "a",
		}},
		UpdateJobs: []syncUpdateJob{{ID: "j1", OwnerID: "u", ServerID: "s1", Status: "running", Steps: []byte("[]"), CreatedAt: when, Seq: 1, Origin: "a"}},
	}
	remote := SyncDoc{
		Updates: []syncUpdate{{
			Kind: "server", TargetID: "s2", OwnerID: "u", Current: "a", Available: "b", Level: "security",
			Detail: json.RawMessage(`{}`), CheckedAt: when, Seq: 1, Origin: "b",
		}, {
			Kind: "server", TargetID: "s1", OwnerID: "u", Current: "1", Available: "3", Level: "minor",
			Detail: json.RawMessage(`{}`), CheckedAt: when, Seq: 2, Origin: "b",
		}},
		UpdateJobs: []syncUpdateJob{{ID: "j2", OwnerID: "u", ClusterID: "c1", Status: "running", Steps: []byte("[]"), CreatedAt: when, Seq: 1, Origin: "b"}},
	}
	got := mergeDocs(local, remote)
	if len(got.Updates) != 2 {
		t.Fatalf("updates %+v", got.Updates)
	}
	var s1 syncUpdate
	for _, row := range got.Updates {
		if row.TargetID == "s1" {
			s1 = row
		}
	}
	if s1.Available != "3" || s1.Level != "minor" {
		t.Fatalf("s1 %+v", s1)
	}
	if len(got.UpdateJobs) != 2 {
		t.Fatalf("jobs %+v", got.UpdateJobs)
	}
}

func TestHashIgnoresOrder(t *testing.T) {
	left := SyncDoc{Users: []syncUser{{ID: "b", Seq: 1, Origin: "a"}, {ID: "a", Seq: 1, Origin: "a"}}}
	right := SyncDoc{Users: []syncUser{{ID: "a", Seq: 1, Origin: "a"}, {ID: "b", Seq: 1, Origin: "a"}}}
	if left.Hash() != right.Hash() {
		t.Fatal(left.Hash(), right.Hash())
	}
}
