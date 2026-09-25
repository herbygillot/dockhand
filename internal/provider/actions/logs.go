package actions

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Built is what one runner's job log says about one subport, read from the
// markers MacPorts' workflow writes (.github/workflows/main.yml): its
// "::group::" headings and "::error::" lines. Nothing is inferred beyond
// them.
type Built struct {
	// Listed is true when the workflow named the subport among the ones
	// it would build.
	Listed bool
	// Lint, Dependencies, and Install say a step failed.
	Lint, Dependencies, Install bool
	// Installing is true when the workflow began installing it.
	Installing bool
	// Tested is true when its tests ran, and TestsFailed when they failed.
	Tested, TestsFailed bool
}

// Outcome is the subport's result on this runner, as the markers show it:
// passed once installing began and nothing failed; failed at lint or
// install; not run when the workflow never reached it.
func (b Built) Outcome() (model.Outcome, model.Phase) {
	switch {
	case b.Lint:
		return model.OutcomeFailed, model.PhaseLint
	case b.Dependencies || b.Install:
		return model.OutcomeFailed, model.PhaseInstall
	case b.Installing:
		return model.OutcomePassed, ""
	}
	return model.OutcomeNotRun, ""
}

// Tests is the subport's test outcome on this runner. The workflow counts
// failed tests against nothing, so neither does dockhand's default policy.
func (b Built) Tests() model.TestOutcome {
	switch {
	case b.TestsFailed:
		return model.TestsFailed
	case b.Tested:
		return model.TestsPassed
	}
	return model.TestsNone
}

// GitHub's job logs begin each line with a timestamp.
var timestamp = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[0-9:.]+Z `)

// ReadLog reads one job's log into what it says about each subport.
func ReadLog(log []byte) map[string]*Built {
	built := map[string]*Built{}
	get := func(name string) *Built {
		name = strings.TrimSpace(name)
		if built[name] == nil {
			built[name] = &Built{}
		}
		return built[name]
	}
	listing := false
	scanner := bufio.NewScanner(bytes.NewReader(log))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := timestamp.ReplaceAllString(scanner.Text(), "")
		switch {
		case line == "##[group]Listing subports" || line == "::group::Listing subports":
			listing = true
		case listing && (strings.HasPrefix(line, "##[endgroup]") || strings.HasPrefix(line, "::endgroup::")):
			listing = false
		case listing && line != "" && !strings.ContainsAny(line, " :"):
			get(line).Listed = true
		case strings.HasPrefix(trimMarker(line), "port lint "):
			name, _, _ := strings.Cut(strings.TrimPrefix(trimMarker(line), "port lint "), ":")
			get(name).Lint = true
		case strings.HasPrefix(trimMarker(line), "Failed to install dependencies for "):
			get(strings.TrimPrefix(trimMarker(line), "Failed to install dependencies for ")).Dependencies = true
		case strings.HasPrefix(trimMarker(line), "Failed to install "):
			get(strings.TrimPrefix(trimMarker(line), "Failed to install ")).Install = true
		case strings.HasPrefix(trimMarker(line), "Tests failed for "):
			get(strings.TrimPrefix(trimMarker(line), "Tests failed for ")).TestsFailed = true
		case strings.HasPrefix(groupTitle(line), "Installing dependencies for "):
		case strings.HasPrefix(groupTitle(line), "Installing "):
			get(strings.TrimPrefix(groupTitle(line), "Installing ")).Installing = true
		case strings.HasPrefix(groupTitle(line), "Testing "):
			get(strings.TrimPrefix(groupTitle(line), "Testing ")).Tested = true
		}
	}
	return built
}

// groupTitle is a group heading's title, as the workflow writes it or as
// GitHub's log shows it.
func groupTitle(line string) string {
	for _, prefix := range []string{"##[group]", "::group::"} {
		if title, ok := strings.CutPrefix(line, prefix); ok {
			return title
		}
	}
	return ""
}

// trimMarker is an error line's message: "::error::message", or with a
// file named, "::error file=…::message", as the workflow writes them, or
// "##[error]message", as GitHub's log shows them.
func trimMarker(line string) string {
	if message, ok := strings.CutPrefix(line, "##[error]"); ok {
		return message
	}
	if rest, ok := strings.CutPrefix(line, "::error"); ok {
		if _, message, ok := strings.Cut(rest, "::"); ok {
			return message
		}
	}
	return ""
}
