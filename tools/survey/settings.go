package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	portsurvey "github.com/herbygillot/dockhand/internal/macports/survey"
)

// settings are what a survey was run with that decide what it measures,
// recorded in its run.json: two surveys compare only where they agree on
// each (#222). A run without -db doesn't observe each port in every
// context before assessing it (Service.Observe), so its outcomes and
// contexts left aren't a -db run's; on 2026-11-12 one said 472 contexts
// left where main's -db survey left 82.
type settings struct {
	// Mode is what each port was evaluated against, -mode.
	Mode string `json:"mode"`
	// Record is whether the run wrote a record, -db, and so observed each
	// port in every context before assessing it.
	Record bool `json:"record"`
	// Recording is whether the record kept what each evaluation read,
	// -recording; false without a record.
	Recording bool `json:"recording"`
	// Selection is the ports surveyed: every port, or how many were named
	// with a digest of their names, and any maintainers or categories.
	Selection string `json:"selection"`
	// Commit is the tree's, and Prefix the MacPorts that evaluated.
	Commit string `json:"commit"`
	Prefix string `json:"prefix"`
}

// selectionOf says a request's selection as settings record it.
func selectionOf(selection portsurvey.Selection) string {
	if selection.All {
		return "every port"
	}
	var parts []string
	if len(selection.Ports) > 0 {
		ports := slices.Sorted(slices.Values(selection.Ports))
		ports = slices.Compact(ports)
		sum := sha256.Sum256([]byte(strings.Join(ports, "\n")))
		parts = append(parts, fmt.Sprintf("%d ports, sha256:%x", len(ports), sum[:6]))
	}
	for _, named := range []struct {
		label  string
		values []string
	}{{"maintainers", selection.Maintainers}, {"not maintainers", selection.NotMaintainers}, {"categories", selection.Categories}} {
		if len(named.values) > 0 {
			parts = append(parts, named.label+" "+strings.Join(slices.Sorted(slices.Values(named.values)), ","))
		}
	}
	return strings.Join(parts, "; ")
}

// differences are the settings two runs disagree on, each said with both
// values.
func (s settings) differences(other settings) []string {
	var found []string
	differ := func(name string, a, b any) {
		if a != b {
			found = append(found, fmt.Sprintf("%s: %v against %v", name, a, b))
		}
	}
	differ("mode", s.Mode, other.Mode)
	differ("record (-db)", s.Record, other.Record)
	differ("recording", s.Recording, other.Recording)
	differ("selection", s.Selection, other.Selection)
	differ("tree commit", s.Commit, other.Commit)
	differ("prefix", s.Prefix, other.Prefix)
	return found
}

// errUnrecorded is a run made before its settings were recorded.
var errUnrecorded = errors.New("records no settings")

// readSettings reads what a journal's run was made with, from its
// run.json.
func readSettings(journal string) (settings, error) {
	data, err := os.ReadFile(journal + ".run.json")
	if errors.Is(err, os.ErrNotExist) {
		return settings{}, fmt.Errorf("%s: no run.json beside it, so it %w", journal, errUnrecorded)
	}
	if err != nil {
		return settings{}, err
	}
	var made struct {
		Settings *settings `json:"settings"`
	}
	if err := json.Unmarshal(data, &made); err != nil {
		return settings{}, fmt.Errorf("%s.run.json: %w", journal, err)
	}
	if made.Settings == nil {
		return settings{}, fmt.Errorf("%s: made before surveys recorded their settings, it %w", journal, errUnrecorded)
	}
	return *made.Settings, nil
}

// alike holds two journals to like for like: it refuses a pair whose runs
// differ in a setting, naming each, or one that records none, unless the
// comparison is unchecked, when it says what it couldn't check.
func alike(out io.Writer, basePath, newPath string, unchecked bool) error {
	base, baseErr := readSettings(basePath)
	next, nextErr := readSettings(newPath)
	if err := errors.Join(baseErr, nextErr); err != nil {
		if !unchecked {
			return fmt.Errorf("%w; rerun it, or pass -unchecked to compare without knowing the runs measured the same thing", err)
		}
		fmt.Fprintf(out, "unchecked: %v\n", err)
		return nil
	}
	if found := base.differences(next); len(found) > 0 {
		if !unchecked {
			return fmt.Errorf("the runs measure different things, so they don't compare:\n  %s", strings.Join(found, "\n  "))
		}
		fmt.Fprintf(out, "unchecked: the runs differ:\n  %s\n", strings.Join(found, "\n  "))
	}
	return nil
}

// pairFlags are the flags a pair doesn't hand on to its two runs: its
// own, and where each run writes, which the pair names.
var pairFlags = []string{"pair", "out", "db", "journal", "compare", "top", "unchecked"}

// runPair is a before/after pair as one command: the before and after
// survey binaries run one after the other with the same flags, each with
// a record of its own in out, and the two compared (#222).
func runPair(stdout, stderr io.Writer, before, after, out string, args []string, top int) error {
	if out == "" {
		return errors.New("-pair takes -out, the directory its two records go in")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, side := range []struct{ name, binary string }{{"before", before}, {"after", after}} {
		db := filepath.Join(out, side.name+".db")
		fmt.Fprintf(stderr, "pair: the %s run, %s, into %s\n", side.name, side.binary, db)
		cmd := exec.Command(side.binary, append(slices.Clone(args), "-db", db)...)
		cmd.Stdout, cmd.Stderr = stderr, stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("the %s run: %w", side.name, err)
		}
	}
	return compareJournals(stdout, filepath.Join(out, "before.jsonl"), filepath.Join(out, "after.jsonl"), top, false)
}
