package settings_test

import (
	"math"
	"testing"

	"claudecontrol/internal/settings"
)

// Apply must clamp. A slider cannot leave its domain, and a value that came
// from a hand-edited file must be brought back into range rather than passed
// through to whatever it drives.
func TestApplyClampsASliderToItsDomain(t *testing.T) {
	got := 0.0
	s := settings.Setting{
		Kind: settings.KindSlider, Min: 0.1, Max: 0.9,
		Get: func() any { return got },
		Set: func(v any) error { got = v.(float64); return nil },
	}
	for _, c := range []struct{ in, want float64 }{
		{0.5, 0.5}, {-3, 0.1}, {12, 0.9}, {0.1, 0.1}, {0.9, 0.9},
	} {
		if err := s.Apply(c.in); err != nil {
			t.Fatalf("Apply(%g): %v", c.in, err)
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Apply(%g) set %g, want %g", c.in, got, c.want)
		}
	}
}

// Applying goes straight through to whatever the setting drives — that is the
// whole reason a slider is worth having.
func TestApplyReachesTheTargetImmediately(t *testing.T) {
	applied := false
	s := settings.Setting{
		Kind: settings.KindToggle,
		Get:  func() any { return false },
		Set:  func(any) error { applied = true; return nil },
	}
	if err := s.Apply(true); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !applied {
		t.Fatal("Apply did not reach the target")
	}
}

// A choice outside the list is refused rather than silently accepted.
func TestApplyRefusesAnUnlistedChoice(t *testing.T) {
	s := settings.Setting{
		Kind: settings.KindChoice, Choices: []string{"braille", "half-block"},
		Get: func() any { return "braille" },
		Set: func(any) error { return nil },
	}
	if err := s.Apply("crayon"); err == nil {
		t.Fatal("an unlisted choice was accepted")
	}
	if err := s.Apply("half-block"); err != nil {
		t.Fatalf("a listed choice was refused: %v", err)
	}
}

// Fraction is where the slider's handle sits, between zero and one.
func TestFractionMapsTheDomainOntoZeroToOne(t *testing.T) {
	v := 0.5
	s := settings.Setting{
		Kind: settings.KindSlider, Min: 0, Max: 2,
		Get: func() any { return v },
		Set: func(x any) error { v = x.(float64); return nil },
	}
	for _, c := range []struct{ value, want float64 }{
		{0, 0}, {1, 0.5}, {2, 1},
	} {
		v = c.value
		if got := s.Fraction(); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Fraction at %g = %g, want %g", c.value, got, c.want)
		}
	}
}

// A degenerate domain must not divide by zero.
func TestFractionOfAnEmptyDomainIsZero(t *testing.T) {
	s := settings.Setting{
		Kind: settings.KindSlider, Min: 1, Max: 1,
		Get: func() any { return 1.0 },
		Set: func(any) error { return nil },
	}
	if got := s.Fraction(); got != 0 {
		t.Fatalf("Fraction = %g, want 0", got)
	}
}
