// Package usage reports how much of the account's rate-limit budget is spent.
//
// These figures are not on disk. Claude Code caches session statistics in
// ~/.claude/stats-cache.json, but that file holds long-run history and goes
// stale for weeks; the live five-hour and weekly budgets come from an endpoint
// Claude Code calls with the OAuth token it stores. This package reads that
// token and asks the same endpoint.
//
// That endpoint is internal and carries no compatibility promise. Everything
// here therefore treats a missing field, an unexpected shape or a refused
// request as "unavailable" and says so, rather than filling the gap with a
// number nobody can check. A blank reading and a zero reading must never look
// alike.
package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultEndpoint is where Claude Code asks the same question.
const DefaultEndpoint = "https://api.anthropic.com/api/oauth/usage"

// Window is one rate-limit budget: how much of it is spent, and when it
// refills.
type Window struct {
	Percent  float64
	ResetsAt time.Time

	// Resets records whether ResetsAt means anything. A budget can report a
	// share without a reset time, and zero time would read as 1 January year 1.
	Resets bool
}

// Until says how long is left before a budget refills, rather than at what
// clock time it does: a clock time has to be compared against another clock to
// mean anything.
func Until(at, now time.Time) string {
	d := at.Sub(now)
	if d <= 0 {
		return "now"
	}
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", max(int(d.Minutes()), 1))
}

// Scoped is a budget that applies to one model rather than to everything.
type Scoped struct {
	Window
	Model string
}

// Snapshot is one reading of the account's budgets.
type Snapshot struct {
	FiveHour Window
	SevenDay Window
	Scoped   []Scoped
	At       time.Time
}

// Token is the credential Claude Code stores, with its expiry.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

// Expired reports whether the token is past its stated life. Sending a dead
// token would only earn a 401; saying so is more useful.
func (t Token) Expired(now time.Time) bool {
	return !t.ExpiresAt.IsZero() && now.After(t.ExpiresAt)
}

// DefaultCredentialsPath follows the same CLAUDE_CONFIG_DIR override Claude
// Code honours, so a relocated configuration is found rather than reported
// missing.
func DefaultCredentialsPath() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, ".credentials.json")
}

// ReadToken loads the OAuth token. The value is never logged or returned in an
// error: an error message travels further than the value it describes.
func ReadToken(path string) (Token, error) {
	if path == "" {
		return Token{}, errors.New("usage: no credentials path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Token{}, fmt.Errorf("usage: read credentials: %w", err)
	}
	var doc struct {
		OAuth struct {
			AccessToken string  `json:"accessToken"`
			ExpiresAt   float64 `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Token{}, fmt.Errorf("usage: parse credentials: %w", err)
	}
	if doc.OAuth.AccessToken == "" {
		return Token{}, fmt.Errorf("usage: %s carries no access token", path)
	}
	t := Token{Value: doc.OAuth.AccessToken}
	if doc.OAuth.ExpiresAt > 0 {
		// Milliseconds since the epoch, as Claude Code writes it.
		t.ExpiresAt = time.UnixMilli(int64(doc.OAuth.ExpiresAt))
	}
	return t, nil
}

// Client asks the endpoint for a snapshot.
type Client struct {
	// Endpoint defaults to DefaultEndpoint.
	Endpoint string
	// CredentialsPath defaults to DefaultCredentialsPath.
	CredentialsPath string
	// HTTP defaults to a client with a short timeout: this is decoration on a
	// terminal, and it must never be what makes the interface wait.
	HTTP *http.Client
	// Now defaults to time.Now, and exists so expiry is testable.
	Now func() time.Time
}

func (c *Client) endpoint() string {
	if c.Endpoint != "" {
		return c.Endpoint
	}
	return DefaultEndpoint
}

func (c *Client) credentials() string {
	if c.CredentialsPath != "" {
		return c.CredentialsPath
	}
	return DefaultCredentialsPath()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// wire mirrors the endpoint's payload. Pointers mark what the endpoint is
// allowed to leave out.
type wire struct {
	FiveHour wireWindow `json:"five_hour"`
	SevenDay wireWindow `json:"seven_day"`
	Limits   []struct {
		Kind     string  `json:"kind"`
		Percent  float64 `json:"percent"`
		ResetsAt *string `json:"resets_at"`
		Scope    struct {
			Model struct {
				DisplayName string `json:"display_name"`
			} `json:"model"`
		} `json:"scope"`
	} `json:"limits"`
}

type wireWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

func (w wireWindow) window() Window {
	out := Window{}
	if w.Utilization != nil {
		out.Percent = *w.Utilization
	}
	if w.ResetsAt != nil {
		if t, err := time.Parse(time.RFC3339, *w.ResetsAt); err == nil {
			out.ResetsAt, out.Resets = t, true
		}
	}
	return out
}

// Fetch reads one snapshot.
func (c *Client) Fetch(ctx context.Context) (Snapshot, error) {
	tok, err := ReadToken(c.credentials())
	if err != nil {
		return Snapshot{}, err
	}
	if tok.Expired(c.now()) {
		return Snapshot{}, fmt.Errorf("usage: the stored token expired %s ago; run claude to refresh it",
			c.now().Sub(tok.ExpiresAt).Round(time.Minute))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(), nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("usage: request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.Value)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("usage: %w", err)
	}
	defer resp.Body.Close()

	// Bounded, because nothing here is worth an unbounded read from the
	// network.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Snapshot{}, fmt.Errorf("usage: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("usage: endpoint answered %s", resp.Status)
	}

	var w wire
	if err := json.Unmarshal(body, &w); err != nil {
		return Snapshot{}, fmt.Errorf("usage: parse response: %w", err)
	}

	snap := Snapshot{
		FiveHour: w.FiveHour.window(),
		SevenDay: w.SevenDay.window(),
		At:       c.now(),
	}
	for _, l := range w.Limits {
		// Only the per-model caps add anything: the account-wide ones repeat
		// five_hour and seven_day.
		if l.Kind != "weekly_scoped" || l.Scope.Model.DisplayName == "" {
			continue
		}
		s := Scoped{Model: l.Scope.Model.DisplayName}
		s.Percent = l.Percent
		if l.ResetsAt != nil {
			if t, err := time.Parse(time.RFC3339, *l.ResetsAt); err == nil {
				s.ResetsAt, s.Resets = t, true
			}
		}
		snap.Scoped = append(snap.Scoped, s)
	}
	if w.FiveHour.Utilization == nil && w.SevenDay.Utilization == nil {
		return Snapshot{}, errors.New("usage: the response carried no budget; the endpoint's shape has changed")
	}
	return snap, nil
}
