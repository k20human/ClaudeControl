package hologram

import (
	"math"
	"testing"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/holo"
	"claudecontrol/internal/module"
	"claudecontrol/internal/transcript"
)

// Each state has to move differently, or the panel says nothing about what is
// happening. These are the distinctions that carry the meaning: thinking is
// quick and coherent, waiting is nearly still and swept, a dead session loses
// its coherence.
func TestEachStateMovesDifferently(t *testing.T) {
	idle, working := moodFor("idle"), moodFor("working")
	waiting, exited := moodFor("waiting"), moodFor("exited")

	if working.speed <= idle.speed {
		t.Errorf("thinking (%.2f) is no quicker than resting (%.2f)", working.speed, idle.speed)
	}
	if working.scatter >= idle.scatter {
		t.Errorf("thinking scatters %.2f, resting %.2f; thinking should be the coherent one",
			working.scatter, idle.scatter)
	}
	if waiting.rotation >= idle.rotation/2 {
		t.Errorf("waiting turns at %.2f against %.2f at rest; it should be nearly still",
			waiting.rotation, idle.rotation)
	}
	if waiting.pulse <= 0 {
		t.Error("waiting has no sweeping band, which is the whole of its signature")
	}
	if working.pulse != 0 {
		t.Errorf("thinking sweeps at %.2f; the band belongs to waiting alone", working.pulse)
	}
	if exited.scatter <= working.scatter || exited.scatter <= idle.scatter {
		t.Errorf("a dead session scatters %.2f, no more than a live one", exited.scatter)
	}
}

// An unknown state must fall back on rest rather than on the zero value, which
// would stop the sphere dead.
func TestAnUnknownStateRests(t *testing.T) {
	if got := moodFor("something-new"); got != moods["idle"] {
		t.Errorf("moodFor(unknown) = %+v, want the resting mood", got)
	}
}

// A mood that arrived instantly would read as a cut. Easing has to move a
// useful part of the way in its own time constant, and to arrive.
func TestAMoodEasesRatherThanJumping(t *testing.T) {
	from, to := moodFor("idle"), moodFor("working")

	half := from.ease(to, moodTau)
	span := to.speed - from.speed
	moved := (half.speed - from.speed) / span
	if moved < 0.5 || moved > 0.8 {
		t.Errorf("one time constant moved %.0f%% of the way; want somewhere near two thirds",
			moved*100)
	}

	// A single frame barely moves.
	frame := from.ease(to, 1.0/60)
	if got := (frame.speed - from.speed) / span; got > 0.05 {
		t.Errorf("one frame moved %.1f%% of the way; the change would look like a cut", got*100)
	}

	// And it gets there.
	cur := from
	for i := 0; i < 60*8; i++ {
		cur = cur.ease(to, 1.0/60)
	}
	if math.Abs(cur.speed-to.speed) > 0.01 {
		t.Errorf("after eight seconds the speed is %.3f, want %.3f", cur.speed, to.speed)
	}
}

// The configured parameters are the author's taste. A mood scales them; it
// must never replace them.
func TestAMoodScalesTheConfigurationRatherThanReplacingIt(t *testing.T) {
	base := holo.DefaultParams()
	base.Speed, base.Rotation, base.Breath = 0.30, 0.20, 6
	fast, slow := moodFor("working").apply(base), moodFor("waiting").apply(base)

	if fast.Speed <= base.Speed || slow.Speed >= base.Speed {
		t.Errorf("thinking %.3f and waiting %.3f do not bracket the configured %.3f",
			fast.Speed, slow.Speed, base.Speed)
	}
	// Density is left alone on purpose: changing it rebuilds the particles,
	// which reads as a glitch at the exact moment a transition should be
	// smooth.
	if fast.Density != base.Density || slow.Density != base.Density {
		t.Errorf("a mood changed the density from %.2f", base.Density)
	}
	// Breathing turned off in the configuration stays off.
	quiet := base
	quiet.Breath = 0
	if got := moodFor("idle").apply(quiet); got.Breath != 0 {
		t.Errorf("a mood revived breathing the configuration had turned off: %.2f", got.Breath)
	}
}

// A turn is what a spark marks, and the panel is quiet exactly when the
// sessions are.
func TestATurnFiresSparksAndSilenceFiresNone(t *testing.T) {
	b := bus.New()
	defer b.Close()

	built, err := New(map[string]any{"style": "sphere"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m := built.(*Module)
	woke := make(chan struct{}, 8)
	if err := m.Init(module.Context{Bus: b, Wake: func() {
		select {
		case woke <- struct{}{}:
		default:
		}
	}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Resize(40, 20); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	sph := m.renderer.(*sphereRenderer)
	// Half a second of animation with nothing happening.
	for i := 0; i < 30; i++ {
		m.Step(1.0 / 60)
	}
	if got := sph.sphere.Sparks(); got != 0 {
		t.Fatalf("%d sparks in flight with nothing happening", got)
	}

	b.PublishState(transcript.SessionTopic, transcript.SessionMetrics{
		SessionID: "s",
		Metrics:   transcript.Metrics{Model: "claude-opus-5", Context: 34_000, Thinking: 1_200},
	})
	<-woke

	m.mu.Lock()
	got := sph.sphere.Sparks()
	ctx, think := m.sig.Context, m.sig.Thinking
	m.mu.Unlock()
	if got == 0 {
		t.Error("a turn fired no spark")
	}
	if ctx != 34_000 || think != 1_200 {
		t.Errorf("the signal carries %d context and %d thinking after the turn", ctx, think)
	}
}

// A turn always shows something, a hard one shows more, and no turn can flood
// the sphere.
func TestSparksPerTurnAreProportionalAndCapped(t *testing.T) {
	if got := sparksFor(transcript.Metrics{}); got < 1 {
		t.Errorf("a turn with no thinking fired %d sparks, want at least one", got)
	}
	small := sparksFor(transcript.Metrics{Thinking: 400})
	large := sparksFor(transcript.Metrics{Thinking: 2_000})
	if large <= small {
		t.Errorf("a turn that thought harder fired %d against %d", large, small)
	}
	if got := sparksFor(transcript.Metrics{Thinking: 10_000_000}); got > 6 {
		t.Errorf("an enormous turn fired %d sparks; the cap is not holding", got)
	}
}
