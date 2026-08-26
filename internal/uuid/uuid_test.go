package uuid_test

import (
	"regexp"
	"testing"

	"claudecontrol/internal/uuid"
)

// Claude Code validates the value it is given, so the format is a contract and
// not a detail: version 4, variant 1, lower case, canonical grouping.
var canonical = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewProducesACanonicalVersion4UUID(t *testing.T) {
	for i := 0; i < 50; i++ {
		got, err := uuid.New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if !canonical.MatchString(got) {
			t.Fatalf("New() = %q, which is not a canonical v4 UUID", got)
		}
	}
}

func TestNewDoesNotRepeatItself(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		got, err := uuid.New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if seen[got] {
			t.Fatalf("New() returned %q twice", got)
		}
		seen[got] = true
	}
}
