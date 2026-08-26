// Package procs finds processes this application did not start, so a service
// already running can be managed rather than started a second time.
//
// Everything here reads /proc. That is Linux-only and deliberate: the
// alternative is parsing the output of ps, which varies between systems and
// cannot report a working directory at all.
package procs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Proc is a running process, identified precisely enough to be sure it is
// still the same one later.
type Proc struct {
	Pid     int
	PPID    int
	PGID    int
	Cmdline []string
	Cwd     string

	// State is the letter /proc reports: R running, S sleeping, Z zombie and
	// so on. A zombie has already ended and is only waiting to be collected by
	// its parent, so it must not be counted as running — nor signalled, which
	// would achieve nothing.
	State byte

	// Start is the process's start time in clock ticks since boot. It is what
	// distinguishes this process from a later one that happens to be given the
	// same number: signalling a recycled pid would kill something unrelated.
	Start uint64
}

// Owned reports whether the process leads its own group, which is true of
// everything started on a pseudo-terminal and false of anything launched from
// a shell. It decides how the process can be stopped: a group its shell also
// belongs to must never be signalled wholesale.
func (p Proc) Owned() bool { return p.PGID == p.Pid }

// Alive reports whether the process is still running and still the same one.
func (p Proc) Alive() bool {
	got, err := Read(p.Pid)
	if err != nil {
		return false
	}
	return got.Start == p.Start && got.State != 'Z'
}

// Read loads one process, or reports that it is gone.
func Read(pid int) (Proc, error) {
	base := filepath.Join("/proc", strconv.Itoa(pid))
	raw, err := os.ReadFile(filepath.Join(base, "stat"))
	if err != nil {
		return Proc{}, fmt.Errorf("procs: %d: %w", pid, err)
	}
	p, err := parseStat(pid, string(raw))
	if err != nil {
		return Proc{}, err
	}
	// Both of these are denied for other users' processes, which is not an
	// error worth failing on: the process is real, we simply cannot see that
	// much of it.
	if cwd, err := os.Readlink(filepath.Join(base, "cwd")); err == nil {
		p.Cwd = cwd
	}
	if raw, err := os.ReadFile(filepath.Join(base, "cmdline")); err == nil {
		p.Cmdline = splitCmdline(raw)
	}
	return p, nil
}

// parseStat reads the fields this package needs out of /proc/<pid>/stat.
//
// The second field is the executable name in parentheses and may itself
// contain spaces and parentheses, so the fields after it are found from the
// last closing parenthesis rather than by splitting the whole line.
func parseStat(pid int, stat string) (Proc, error) {
	end := strings.LastIndex(stat, ")")
	if end < 0 || end+2 > len(stat) {
		return Proc{}, fmt.Errorf("procs: %d: unreadable stat", pid)
	}
	fields := strings.Fields(stat[end+2:])
	// After the name come state, ppid, pgrp, ... and starttime, which is the
	// twenty-second field of the line and so the twentieth of this slice.
	if len(fields) < 20 {
		return Proc{}, fmt.Errorf("procs: %d: stat has %d fields after the name", pid, len(fields))
	}
	state := byte(0)
	if len(fields[0]) > 0 {
		state = fields[0][0]
	}
	ppid, _ := strconv.Atoi(fields[1])
	pgid, _ := strconv.Atoi(fields[2])
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return Proc{}, fmt.Errorf("procs: %d: unreadable start time: %w", pid, err)
	}
	return Proc{Pid: pid, PPID: ppid, PGID: pgid, Start: start, State: state}, nil
}

func splitCmdline(raw []byte) []string {
	parts := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// All lists every process this user can see.
func All() ([]Proc, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("procs: %w", err)
	}
	out := make([]Proc, 0, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		// A process can end between the listing and the read, which is not a
		// failure: it simply is not there any more.
		if p, err := Read(pid); err == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// Find returns the processes running argv in dir.
//
// The match is on the command line as written, which is exactly what npm
// reports: a `npm run debug` started in a project directory appears in /proc
// with that command line and that working directory. A directory alone would
// not do — a project can run two different scripts at once.
func Find(dir string, argv []string) ([]Proc, error) {
	if dir == "" || len(argv) == 0 {
		return nil, nil
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		want = dir
	}
	all, err := All()
	if err != nil {
		return nil, err
	}
	var out []Proc
	for _, p := range all {
		if p.Cwd != want || !sameArgv(p.Cmdline, argv) {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func sameArgv(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Descendants lists a process and everything it started, deepest last.
func Descendants(pid int) []int {
	all, err := All()
	if err != nil {
		return []int{pid}
	}
	children := map[int][]int{}
	for _, p := range all {
		children[p.PPID] = append(children[p.PPID], p.Pid)
	}
	out := []int{pid}
	for i := 0; i < len(out); i++ {
		out = append(out, children[out[i]]...)
	}
	return out
}

// Terminate ends a process and everything it started.
//
// The subtree, never the process group. A process this application started
// leads its own group and could be signalled wholesale, but one adopted from a
// shell shares that shell's group — and signalling it would kill the terminal
// the person is sitting in front of. Walking the children costs a scan of
// /proc and cannot make that mistake.
func (p Proc) Terminate(grace time.Duration) error {
	if !p.Alive() {
		return nil
	}
	// The parent first, so it has the chance to stop its own children the way
	// it means to.
	tree := Descendants(p.Pid)
	for _, pid := range tree {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !anyAlive(tree) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, pid := range tree {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}

func anyAlive(pids []int) bool {
	for _, pid := range pids {
		// Read rather than a bare stat: an ended process whose parent has not
		// collected it still has a directory in /proc, and waiting on a zombie
		// would spend the whole grace period for nothing.
		if p, err := Read(pid); err == nil && p.State != 'Z' {
			return true
		}
	}
	return false
}
