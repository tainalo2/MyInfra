package store

import (
	"strings"
	"testing"
)

func TestSplitSchemaMatchesStatements(t *testing.T) {
	got := splitSQL(schema)
	var want []string
	for _, stmt := range strings.Split(schema, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt != "" {
			want = append(want, stmt)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("statement %d\n%s\n%s", i, got[i], want[i])
		}
	}
}

func TestSplitSQLKeepsFunctionBody(t *testing.T) {
	sql := "SELECT 1;\nCREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n  PERFORM 1;\nEND;\n$$ LANGUAGE plpgsql;\nSELECT 2;"
	parts := splitSQL(sql)
	if len(parts) != 3 {
		t.Fatalf("%d: %#v", len(parts), parts)
	}
	if !strings.Contains(parts[1], "PERFORM 1;") {
		t.Fatal(parts[1])
	}
}
