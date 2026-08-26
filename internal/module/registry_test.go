package module_test

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/module"
)

type nothing struct{}

func (nothing) Init(module.Context) error    { return nil }
func (nothing) Resize(int, int) error        { return nil }
func (nothing) Draw(uv.Screen, uv.Rectangle) {}
func (nothing) Close() error                 { return nil }

func factory(map[string]any) (module.Module, error) { return nothing{}, nil }

// Registering the same name twice can only be a programming mistake made at
// init time, and it must be loud: the alternative is one of the two modules
// silently never being reachable.
func TestRegisteringTwiceIsFatal(t *testing.T) {
	module.Register("test-duplicate", factory)
	defer func() {
		if recover() == nil {
			t.Fatal("registering the same name twice was allowed")
		}
	}()
	module.Register("test-duplicate", factory)
}

// The error names what is available, because the usual cause is a typo in a
// configuration file and a list is what turns that into a fix.
func TestAnUnknownNameListsWhatExists(t *testing.T) {
	module.Register("test-known", factory)
	_, err := module.New("test-unknown-xyz", nil)
	if err == nil {
		t.Fatal("an unknown module was built")
	}
	if got := err.Error(); !contains(got, "test-known") {
		t.Errorf("error = %q, want it to list the registered names", got)
	}
}

func TestNamesAreSorted(t *testing.T) {
	module.Register("zzz-late", factory)
	module.Register("aaa-early", factory)
	names := module.Names()
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("Names() is not sorted: %v", names)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
