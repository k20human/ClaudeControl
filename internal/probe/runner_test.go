package probe_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/probe"
)

func TestTheRunnerPublishesWhatItFound(t *testing.T) {
	b := bus.New()
	defer b.Close()
	ch := b.SubscribeState(probe.HealthTopic)

	r := probe.NewRunner(b, 20*time.Millisecond, time.Second)
	r.Add(probe.Command("ok", []string{"true"}))
	r.Start()
	defer r.Stop()

	select {
	case v := <-ch:
		results, ok := v.([]probe.Result)
		if !ok {
			t.Fatalf("published %T, want []probe.Result", v)
		}
		if len(results) != 1 || results[0].Name != "ok" {
			t.Fatalf("published %+v, want one result named ok", results)
		}
		if results[0].Health != probe.HealthUp {
			t.Errorf("health = %v, want up", results[0].Health)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was published")
	}
}

// Probes keep being asked, so a service that comes back is noticed.
func TestTheRunnerKeepsAsking(t *testing.T) {
	var calls atomic.Int64
	b := bus.New()
	defer b.Close()

	r := probe.NewRunner(b, 20*time.Millisecond, time.Second)
	r.Add(probe.Probe{Name: "counter", Check: func(context.Context) (probe.Health, string) {
		calls.Add(1)
		return probe.HealthUp, ""
	}})
	r.Start()
	defer r.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the probe ran %d times in three seconds", calls.Load())
}

// One slow probe must not hold the others up: they are asked concurrently and
// bounded by the timeout.
func TestASlowProbeDoesNotDelayTheOthers(t *testing.T) {
	b := bus.New()
	defer b.Close()
	ch := b.SubscribeState(probe.HealthTopic)

	r := probe.NewRunner(b, 50*time.Millisecond, 150*time.Millisecond)
	r.Add(probe.Probe{Name: "slow", Check: func(ctx context.Context) (probe.Health, string) {
		<-ctx.Done()
		return probe.HealthDown, "timed out"
	}})
	r.Add(probe.Command("quick", []string{"true"}))
	r.Start()
	defer r.Stop()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case v := <-ch:
			for _, res := range v.([]probe.Result) {
				if res.Name == "quick" && res.Health == probe.HealthUp {
					return
				}
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("the quick probe never reported while a slow one was running")
}

// Stopping is idempotent and leaves nothing running.
func TestStopIsSafeTwice(t *testing.T) {
	r := probe.NewRunner(bus.New(), 10*time.Millisecond, time.Second)
	r.Add(probe.Command("ok", []string{"true"}))
	r.Start()
	r.Stop()
	r.Stop()
}
