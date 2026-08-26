// Package settings describes what a module lets you change.
//
// A module publishes descriptions; the menu is built from them. Nothing in the
// menu is written per module, so adding a setting is one line rather than a
// line plus a widget.
package settings

import "fmt"

// Kind is how a setting is presented and edited.
type Kind int

const (
	// KindSlider is a bounded number.
	KindSlider Kind = iota
	// KindToggle is on or off.
	KindToggle
	// KindChoice is one of a fixed list.
	KindChoice
)

// Setting is one thing a module lets you change.
type Setting struct {
	Key   string
	Label string
	Kind  Kind

	// Slider only.
	Min, Max, Step float64

	// Choice only.
	Choices []string

	Get func() any
	Set func(any) error
}

// Clamp confines v to the range.
func Clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// Apply validates a value and passes it on.
//
// Clamping happens here rather than in each module: a value can arrive from a
// slider, from a keystroke, or from a hand-edited file, and every one of those
// paths would otherwise need the same guard.
func (s Setting) Apply(v any) error {
	switch s.Kind {
	case KindSlider:
		f, ok := toFloat(v)
		if !ok {
			return fmt.Errorf("settings: %s wants a number, got %T", s.Key, v)
		}
		return s.Set(Clamp(f, s.Min, s.Max))
	case KindToggle:
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("settings: %s wants a boolean, got %T", s.Key, v)
		}
		return s.Set(b)
	case KindChoice:
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("settings: %s wants a string, got %T", s.Key, v)
		}
		for _, c := range s.Choices {
			if c == str {
				return s.Set(str)
			}
		}
		return fmt.Errorf("settings: %s has no choice %q (want one of %v)", s.Key, str, s.Choices)
	}
	return fmt.Errorf("settings: %s has no kind", s.Key)
}

// Float is the current value as a number, or zero.
func (s Setting) Float() float64 {
	f, _ := toFloat(s.Get())
	return f
}

// Bool is the current value as a flag.
func (s Setting) Bool() bool {
	b, _ := s.Get().(bool)
	return b
}

// String is the current value as text.
func (s Setting) String() string {
	str, _ := s.Get().(string)
	return str
}

// Fraction is where a slider's handle sits, from zero to one.
func (s Setting) Fraction() float64 {
	span := s.Max - s.Min
	if span <= 0 {
		return 0
	}
	return Clamp((s.Float()-s.Min)/span, 0, 1)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}
