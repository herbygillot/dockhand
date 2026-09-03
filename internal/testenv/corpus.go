package testenv

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// The Portfile corpus is the other external thing a test depends on,
// and it is external in the same sense the tools are: it is a sample of
// the ports tree, checked in so that a claim about real Portfiles can
// be made without one on the machine. Five packages reached for it, each
// spelling its own way up out of its own directory — "../testdata" here,
// "../macports/testdata" there — which is a path that breaks on the
// next package to want it and says nothing about what the corpus IS.
//
// So the location is resolved from this file's own compiled-in path
// rather than from the caller's working directory. That is stable under
// `go test ./...` from anywhere in the tree, and it makes the corpus a
// named thing a test asks for, like tclsh.

// corpusRoot is the testdata directory beside internal/macports,
// resolved from this source file rather than from the caller's working
// directory.
func corpusRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("testenv: cannot locate this package's own source; the corpus path is unresolvable")
	}
	return filepath.Join(filepath.Dir(self), "..", "macports", "testdata")
}

// Portfile returns one corpus Portfile by its fixture name — the
// category-and-port form the corpus uses, e.g. "math__ivy". A name the
// corpus does not hold fails the test rather than skipping it: the
// corpus is checked in, so its absence is a broken checkout, never a
// machine without a tool.
func Portfile(t *testing.T, name string) []byte {
	t.Helper()
	return fixture(t, filepath.Join(corpusRoot(t), "portfiles", name))
}

// PortfileDir writes one corpus Portfile into a fresh temporary portdir
// and returns the directory — the form every evaluating test wants,
// since a Portfile is only evaluable as the contents of a portdir.
func PortfileDir(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Portfile"), Portfile(t, name), 0o644); err != nil {
		t.Fatalf("staging %s: %v", name, err)
	}
	return dir
}

// Portfiles lists every Portfile fixture name in the corpus, sorted,
// for the sweeps that must cover all of them.
func Portfiles(t *testing.T) []string {
	t.Helper()
	return names(t, filepath.Join(corpusRoot(t), "portfiles"))
}

// Portgroup returns one corpus PortGroup by file name, e.g.
// "perl5-1.0.tcl". The PortGroups are the corpus's other half: they are
// what the style table was mined from, and what the differential tests
// run against a plain tclsh.
func Portgroup(t *testing.T, name string) []byte {
	t.Helper()
	return fixture(t, filepath.Join(corpusRoot(t), "portgroups", name))
}

// Portgroups lists every PortGroup file name in the corpus, sorted, for
// the sweeps that must cover all of them.
func Portgroups(t *testing.T) []string {
	t.Helper()
	return names(t, filepath.Join(corpusRoot(t), "portgroups"))
}

// names lists one corpus half, sorted. The order is fixed here rather
// than left to the filesystem so that a sweep reports its failures in
// the same order on every machine.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the corpus: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func fixture(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the corpus fixture: %v", err)
	}
	return body
}
