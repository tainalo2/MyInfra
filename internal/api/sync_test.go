package api

import (
	"bytes"
	"testing"
)

func TestSyncTokenDependsOnKey(t *testing.T) {
	left := syncToken(bytes.Repeat([]byte{1}, 32))
	right := syncToken(bytes.Repeat([]byte{1}, 32))
	if left != right || len(left) != 64 {
		t.Fatal(left)
	}
	if syncToken(bytes.Repeat([]byte{2}, 32)) == left {
		t.Fatal("jeton identique pour deux clés")
	}
}
