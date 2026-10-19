package box

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	master := make([]byte, 32)
	if _, err := rand.Read(master); err != nil {
		t.Fatal(err)
	}
	b, err := New(master)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b.Seal([]byte("clé privée"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("clé privée")) {
		t.Fatalf("got %q", got)
	}
}

func TestWrongMaster(t *testing.T) {
	a, _ := New(bytes.Repeat([]byte{1}, 32))
	b, _ := New(bytes.Repeat([]byte{2}, 32))
	sealed, err := a.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Open(sealed); err == nil {
		t.Fatal("expected error")
	}
}

func TestRejectsShortMaster(t *testing.T) {
	if _, err := New([]byte("court")); err == nil {
		t.Fatal("expected error")
	}
}
