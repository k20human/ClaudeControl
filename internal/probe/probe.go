// Package probe answers one question about one thing: is it up?
//
// Three kinds cover what a developer actually watches — a command's exit
// status, an HTTP health endpoint, a listening port. Anything more elaborate
// belongs in a command, which is why the first kind exists.
package probe

import (
	"context"
	"time"
)

// Health is what a probe found.
type Health int

const (
	// HealthUnknown is a probe that has not run yet.
	HealthUnknown Health = iota
	// HealthUp is a probe that succeeded.
	HealthUp
	// HealthDown is a probe that failed, for the reason in its detail.
	HealthDown
)

func (h Health) String() string {
	switch h {
	case HealthUp:
		return "up"
	case HealthDown:
		return "down"
	default:
		return "unknown"
	}
}

// Result is one answer.
type Result struct {
	Name   string
	Health Health
	Detail string
	Took   time.Duration
}

// Probe is a named check.
type Probe struct {
	Name  string
	Check func(ctx context.Context) (Health, string)
}
