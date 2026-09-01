package platform

import (
	"fmt"
	"os/exec"
	"sync"
)

// Tool names an external binary dockhand drives. Every component that
// needs one asks THIS provider — one site finds binaries and doles
// them out, which is also what keeps doctor honest: its assessment
// runs through the identical lookup as the component that will
// actually exec the tool, so what doctor reports available cannot
// drift from what work finds at run time.
type Tool string

// The tools dockhand knows. The constant's value is the binary name.
const (
	Git        Tool = "git"
	Gh         Tool = "gh"
	Tart       Tool = "tart"
	Curl       Tool = "curl"
	Tar        Tool = "tar"
	Tclsh      Tool = "tclsh"
	PortTclsh  Tool = "port-tclsh"
	Go2Port    Tool = "go2port"
	Cargo2Port Tool = "cargo2port"
)

// lookup is the one PATH search, behind a seam for tests.
var (
	lookup    = exec.LookPath
	toolCache sync.Map // Tool -> string path, successes only
)

// Find resolves a tool to its executable path, memoized per process:
// PATH does not change under a run, and the memo is what makes
// "checked once by doctor, used everywhere" a single fact rather than
// nine independent lookups.
func Find(t Tool) (string, error) {
	if p, ok := toolCache.Load(t); ok {
		return p.(string), nil
	}
	path, err := lookup(string(t))
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH", t)
	}
	toolCache.Store(t, path)
	return path, nil
}

// FindWith resolves a tool, trying an explicit fallback path when the
// PATH search misses — doctor's port-tclsh probe falls back to the
// conventional prefix location this way.
func FindWith(t Tool, fallback string) (string, error) {
	if path, err := Find(t); err == nil {
		return path, nil
	}
	if fallback != "" {
		if path, err := lookup(fallback); err == nil {
			toolCache.Store(t, path)
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found on PATH", t)
}

// Have reports availability without caring where.
func Have(t Tool) bool {
	_, err := Find(t)
	return err == nil
}

// StubLookup replaces the PATH search for a test and clears the memo;
// the returned restore puts both back. Test seams are the one
// legitimate mutation of this package's state.
func StubLookup(fn func(string) (string, error)) (restore func()) {
	prev := lookup
	lookup = fn
	toolCache = sync.Map{}
	return func() {
		lookup = prev
		toolCache = sync.Map{}
	}
}
