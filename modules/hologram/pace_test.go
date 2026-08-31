package hologram

import (
	"sync/atomic"
	"testing"
	"time"
)

// The panel asks for the next frame at its own pace. It used to ask at the end
// of every frame it drew, which against the application's sixty-a-second timer
// meant sixty full-screen repaints a second — of which, measured, two thirds
// was the repainting rather than the sphere.
func TestThePacerAsksAtItsOwnRateHoweverOftenItIsCalled(t *testing.T) {
	var woke int64
	p := newPacer(20) // one every 50ms
	defer p.stop()

	// Called far more often than the rate, as a drawing loop would.
	stop := time.After(500 * time.Millisecond)
	for done := false; !done; {
		select {
		case <-stop:
			done = true
		default:
			p.ask(func() { atomic.AddInt64(&woke, 1) })
			time.Sleep(2 * time.Millisecond)
		}
	}
	time.Sleep(100 * time.Millisecond)

	got := atomic.LoadInt64(&woke)
	// Half a second at twenty a second is ten, and the loop asked two hundred
	// and fifty times. A little either way is scheduling, not a rate.
	if got < 6 || got > 16 {
		t.Errorf("%d frames asked for in half a second at 20/s", got)
	}
}

// A closed panel stops asking. One that went on waking a closed application
// would be a leak of the same kind as a goroutine.
func TestAStoppedPacerAsksForNothing(t *testing.T) {
	var woke int64
	p := newPacer(60)
	p.stop()
	for i := 0; i < 20; i++ {
		p.ask(func() { atomic.AddInt64(&woke, 1) })
	}
	time.Sleep(120 * time.Millisecond)
	if got := atomic.LoadInt64(&woke); got != 0 {
		t.Errorf("a stopped pacer asked %d times", got)
	}
}

// A rate nobody can honour is a mistake worth naming rather than clamping in
// silence: the application's own loop is the ceiling.
func TestAnImpossibleRateIsRefused(t *testing.T) {
	for _, bad := range []any{0, -5, 120, 1000} {
		if _, err := New(map[string]any{"style": "sphere", "fps": bad}); err == nil {
			t.Errorf("fps %v was accepted", bad)
		}
	}
	for _, good := range []any{1, 20, 60} {
		if _, err := New(map[string]any{"style": "sphere", "fps": good}); err != nil {
			t.Errorf("fps %v was refused: %v", good, err)
		}
	}
}

// Left unsaid, the panel keeps the pace that was measured to cost a third of
// what asking on every frame cost.
func TestTheDefaultRateIsTheModestOne(t *testing.T) {
	m, err := New(map[string]any{"style": "sphere"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	got := m.(*Module).pace.interval
	if want := time.Second / DefaultFPS; got != want {
		t.Errorf("interval = %v, want %v", got, want)
	}
}
