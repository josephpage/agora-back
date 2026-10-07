package store

import (
	"strings"
	"testing"
)

func TestMigrationStatements(t *testing.T) {
	up, err := MigrationStatements("up")
	if err != nil || len(up) == 0 {
		t.Fatalf("up: %v %d", err, len(up))
	}
	down, err := MigrationStatements("down")
	if err != nil || len(down) != len(up) {
		t.Fatalf("down: %v %d/%d", err, len(down), len(up))
	}
	for _, s := range up {
		if !createIndexRe.MatchString(s) {
			t.Errorf("up statement must be CREATE INDEX CONCURRENTLY IF NOT EXISTS: %s", s)
		}
		if strings.Contains(s, ";") {
			t.Errorf("statement not split: %s", s)
		}
	}
	for _, s := range down {
		if !strings.HasPrefix(s, "DROP INDEX CONCURRENTLY IF EXISTS ") {
			t.Errorf("down statement: %s", s)
		}
	}
}
