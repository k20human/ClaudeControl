// Package settings draws the menu. Nothing here knows what any particular
// setting means: it renders whatever the modules published.
package settings

import (
	"fmt"
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/render"
	stg "claudecontrol/internal/settings"
)

// Layout of a row. The bar starts at a fixed column so every slider lines up,
// which is what lets the eye compare two of them at a glance.
const (
	LabelX   = 3
	BarX     = 24
	BarW     = 28
	ValueX   = BarX + BarW + 2
	FirstRow = 3
)

var (
	bg       = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	fgLabel  = color.RGBA{R: 0xc7, G: 0xd2, B: 0xe0, A: 0xff}
	fgDim    = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	fgAccent = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	bgRow    = color.RGBA{R: 0x24, G: 0x2e, B: 0x3d, A: 0xff}
)

// Panel renders a list of settings.
type Panel struct {
	items    []stg.Setting
	selected int
}

// NewPanel builds a panel over the settings it is given.
func NewPanel(items []stg.Setting) *Panel { return &Panel{items: items} }

// Items are the settings on show.
func (p *Panel) Items() []stg.Setting { return p.items }

// Selected is the highlighted row.
func (p *Panel) Selected() int { return p.selected }

// Move changes the highlighted row, stopping at either end.
func (p *Panel) Move(delta int) {
	p.selected += delta
	if p.selected >= len(p.items) {
		p.selected = len(p.items) - 1
	}
	if p.selected < 0 {
		p.selected = 0
	}
}

// Adjust nudges the highlighted setting by whole steps.
//
// Whole steps rather than free movement: a slider that lands on 0.70 is one you
// can describe and reproduce, and 0.6973 is not.
func (p *Panel) Adjust(steps int) error {
	s, ok := p.current()
	if !ok {
		return nil
	}
	switch s.Kind {
	case stg.KindSlider:
		step := s.Step
		if step <= 0 {
			step = (s.Max - s.Min) / 20
		}
		return s.Apply(s.Float() + float64(steps)*step)
	case stg.KindToggle:
		return s.Apply(!s.Bool())
	case stg.KindChoice:
		if len(s.Choices) == 0 {
			return nil
		}
		at := 0
		for i, c := range s.Choices {
			if c == s.String() {
				at = i
			}
		}
		at = (at + steps) % len(s.Choices)
		if at < 0 {
			at += len(s.Choices)
		}
		return s.Apply(s.Choices[at])
	}
	return nil
}

// ClickAt handles a click, selecting a row and, on a bar, jumping to the value
// the pointer is over.
func (p *Panel) ClickAt(x, y int, area uv.Rectangle) error {
	row := y - area.Min.Y - FirstRow
	if row < 0 || row >= len(p.items) {
		return nil
	}
	p.selected = row

	s := p.items[row]
	barStart := area.Min.X + BarX
	if s.Kind != stg.KindSlider || x < barStart || x >= barStart+BarW {
		// Off the bar: selecting is the whole gesture. A click on the label
		// must not move a value the user was only pointing at.
		return nil
	}
	frac := float64(x-barStart) / float64(BarW-1)
	return s.Apply(s.Min + frac*(s.Max-s.Min))
}

func (p *Panel) current() (stg.Setting, bool) {
	if p.selected < 0 || p.selected >= len(p.items) {
		return stg.Setting{}, false
	}
	return p.items[p.selected], true
}

// SliderBar renders a bar of the given width, filled in proportion.
func SliderBar(fraction float64, width int) string {
	if width < 1 {
		return ""
	}
	filled := int(stg.Clamp(fraction, 0, 1) * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// Draw paints the panel.
func (p *Panel) Draw(scr uv.Screen, area uv.Rectangle) {
	render.Fill(scr, area, bg)
	render.Text(scr, area.Min.X+LabelX, area.Min.Y+1, "SETTINGS", fgAccent, bg)

	for i, s := range p.items {
		y := area.Min.Y + FirstRow + i
		if y >= area.Max.Y-2 {
			break
		}
		rowBg := color.Color(bg)
		if i == p.selected {
			rowBg = bgRow
			render.Fill(scr, uv.Rect(area.Min.X, y, area.Dx(), 1), bgRow)
		}
		render.Text(scr, area.Min.X+LabelX, y, s.Label, fgLabel, rowBg)

		switch s.Kind {
		case stg.KindSlider:
			render.Text(scr, area.Min.X+BarX, y, SliderBar(s.Fraction(), BarW), fgAccent, rowBg)
			render.Text(scr, area.Min.X+ValueX, y, fmt.Sprintf("%.2f", s.Float()), fgLabel, rowBg)
		case stg.KindToggle:
			mark := "[ ]"
			if s.Bool() {
				mark = "[x]"
			}
			render.Text(scr, area.Min.X+BarX, y, mark, fgAccent, rowBg)
		case stg.KindChoice:
			render.Text(scr, area.Min.X+BarX, y, s.String(), fgAccent, rowBg)
		}
	}

	render.Text(scr, area.Min.X+LabelX, area.Max.Y-2,
		"←→ adjust   ↑↓ select   s save   esc close",
		fgDim, bg)
}
