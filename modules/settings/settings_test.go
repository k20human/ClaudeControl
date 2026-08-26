package settings_test

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	stg "claudecontrol/internal/settings"
	panel "claudecontrol/modules/settings"
)

func knob(key string, v *float64) stg.Setting {
	return stg.Setting{
		Key: key, Label: key, Kind: stg.KindSlider, Min: 0, Max: 1, Step: 0.1,
		Get: func() any { return *v },
		Set: func(x any) error { *v = x.(float64); return nil },
	}
}

// The bar has to fill in proportion, or a slider lies about where it is.
func TestSliderBarFillsInProportion(t *testing.T) {
	const w = 20
	for _, c := range []struct{ frac float64 }{{0}, {0.25}, {0.5}, {1}} {
		bar := panel.SliderBar(c.frac, w)
		if got := len([]rune(bar)); got != w {
			t.Fatalf("bar for %g is %d runes wide, want %d", c.frac, got, w)
		}
		filled := strings.Count(bar, "█")
		want := int(c.frac * w)
		if filled < want-1 || filled > want+1 {
			t.Errorf("bar for %g filled %d of %d, want about %d", c.frac, filled, w, want)
		}
	}
}

// Adjusting moves by whole steps, so a slider lands on round numbers rather
// than on whatever a pointer happened to be over.
func TestAdjustMovesByWholeSteps(t *testing.T) {
	v := 0.5
	p := panel.NewPanel([]stg.Setting{knob("speed", &v)})
	if err := p.Adjust(2); err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if v < 0.69 || v > 0.71 {
		t.Fatalf("value = %g after two steps of 0.1 from 0.5", v)
	}
	if err := p.Adjust(-100); err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if v != 0 {
		t.Fatalf("value = %g, want it clamped to the minimum", v)
	}
}

func TestSelectionStopsAtBothEnds(t *testing.T) {
	a, b := 0.0, 0.0
	p := panel.NewPanel([]stg.Setting{knob("one", &a), knob("two", &b)})
	p.Move(-5)
	if got := p.Selected(); got != 0 {
		t.Errorf("Selected = %d after moving up past the start, want 0", got)
	}
	p.Move(5)
	if got := p.Selected(); got != 1 {
		t.Errorf("Selected = %d after moving down past the end, want 1", got)
	}
}

// Clicking a bar jumps to that value: the gesture the user asked for.
func TestClickingABarJumpsToThatValue(t *testing.T) {
	v := 0.0
	p := panel.NewPanel([]stg.Setting{knob("speed", &v)})
	area := uv.Rect(0, 0, 60, 10)
	if err := p.ClickAt(panel.BarX+15, panel.FirstRow, area); err != nil {
		t.Fatalf("ClickAt: %v", err)
	}
	if v <= 0 {
		t.Fatalf("value = %g; clicking part-way along the bar did nothing", v)
	}
	if v > 1 {
		t.Fatalf("value = %g, past the maximum", v)
	}
}

// A click on the label selects without moving anything: pointing at a value is
// not the same as changing it.
func TestClickingTheLabelOnlySelects(t *testing.T) {
	a, b := 0.25, 0.75
	p := panel.NewPanel([]stg.Setting{knob("one", &a), knob("two", &b)})
	area := uv.Rect(0, 0, 60, 10)
	if err := p.ClickAt(panel.LabelX, panel.FirstRow+1, area); err != nil {
		t.Fatalf("ClickAt: %v", err)
	}
	if p.Selected() != 1 {
		t.Errorf("Selected = %d, want 1", p.Selected())
	}
	if b != 0.75 {
		t.Errorf("the value moved to %g on a click that only selected", b)
	}
}
