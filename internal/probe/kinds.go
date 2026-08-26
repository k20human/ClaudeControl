package probe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
)

// detailLimit keeps a chatty command from filling the pane. The first line is
// almost always the one that says what went wrong.
const detailLimit = 120

// Command reports on a command's exit status.
//
// Anything a shell can decide can be a probe this way — pg_isready, systemctl
// is-active, a script of your own — which is why the other two kinds are
// conveniences rather than necessities.
func Command(name string, argv []string) Probe {
	return Probe{Name: name, Check: func(ctx context.Context) (Health, string) {
		if len(argv) == 0 {
			return HealthDown, "no command configured"
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return HealthUp, trim(string(out))
		}
		detail := trim(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return HealthDown, detail
	}}
}

// HTTP reports on a status code below 400.
func HTTP(name, url string) Probe {
	return Probe{Name: name, Check: func(ctx context.Context) (Health, string) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return HealthDown, err.Error()
		}
		// No shared client: a probe is rare, and a client of its own keeps one
		// slow endpoint from holding a connection another probe wants.
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return HealthDown, trim(err.Error())
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return HealthDown, resp.Status
		}
		return HealthUp, resp.Status
	}}
}

// TCP reports on whether a port accepts a connection.
func TCP(name, address string) Probe {
	return Probe{Name: name, Check: func(ctx context.Context) (Health, string) {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", address)
		if err != nil {
			return HealthDown, trim(err.Error())
		}
		_ = conn.Close()
		return HealthUp, "connected"
	}}
}

// trim reduces output to its first line, bounded.
func trim(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > detailLimit {
		s = s[:detailLimit] + "…"
	}
	return s
}

// String makes a probe printable in a test failure.
func (p Probe) String() string { return fmt.Sprintf("probe(%s)", p.Name) }
