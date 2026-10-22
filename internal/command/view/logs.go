package view

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// phaseMissing says a build recorded no step a --phase names: none at
// all, where its provider records none, or not that one, with the ones it
// recorded, as a build that failed at fetch records no install.
func PhaseMissing(port, phase string, result model.TargetResult) error {
	if len(result.Steps) == 0 {
		return fmt.Errorf("%s's build recorded no steps, so its %s step can't be picked out of its log; logs without --phase prints it whole", port, phase)
	}
	var names []string
	for _, step := range result.Steps {
		if !slices.Contains(names, string(step.Name)) {
			names = append(names, string(step.Name))
		}
	}
	return fmt.Errorf("%s's build recorded no %s step; it recorded %s", port, phase, prose.And(names))
}

// portBuildWords say how a build of a port went, for choosing its log.
func PortBuildWords(result model.TargetResult) string {
	switch {
	case result.Outcome == model.OutcomeFailed && result.Phase != "":
		return result.Phase.FailureWords()
	case result.Tests.Failed():
		return string(result.Outcome) + ", its tests " + strings.ReplaceAll(string(result.Tests), "-", " ")
	}
	return string(result.Outcome)
}

// writePortLog prints a port's log. Where its provider recorded where each
// step of the build began, it lists them first, and prints from the port's
// own build on, leaving out its dependencies' installs before it: hugo's
// own phases began near line 46,400 of 47,000 (the hugo exercise). all
// prints the whole log alone, as it is printed where no steps were
// recorded.
func WritePortLog(out io.Writer, places config.Places, result model.TargetResult, data []byte, all bool) error {
	if all {
		_, err := out.Write(data)
		return err
	}
	// A log with no steps recorded is printed whole; one of GitHub's runners
	// whose log lacked the workflow's headings is said to be (D20). A
	// provider that records no steps, as the command provider, says nothing.
	if len(result.Steps) == 0 {
		if len(result.Builders) > 0 {
			fmt.Fprintf(out, "No build-step markers were found for %s. Showing the full log.\n\n", result.Target)
		}
		_, err := out.Write(data)
		return err
	}
	fmt.Fprintf(out, "Steps in %s:\n", places.Tilde(buildlog.Where(result.Log)))
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, step := range result.Steps {
		fmt.Fprintf(tw, "  %s\tline %d\n", step.Name, step.Line)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if own, ok := result.OwnBuild(); ok && own.Line > 1 {
		if rest, ok := buildlog.From(data, own.Line); ok {
			fmt.Fprintf(out, "Showing from line %d, where the build for %s begins. Use --full to include the previous %d lines, including dependency installation.\n\n", own.Line, result.Target, own.Line-1)
			_, err := out.Write(rest)
			return err
		}
		fmt.Fprintf(out, "The log ends before line %d, where %s's own build was recorded to begin, so it's printed whole.\n", own.Line, result.Target)
	}
	fmt.Fprintln(out)
	_, err := out.Write(data)
	return err
}

// writeLogs says what a check recorded, its state first: a run whose
// process ended is said as stopped, where it read "running" (field
// testing's rust check, 2026-10-15).
func WriteLogs(out io.Writer, places config.Places, logs engine.RunLogs, stopped bool) error {
	if stopped {
		name := logs.Run.Name()
		fmt.Fprintf(out, "%s · stopped: the process running it ended; dockhand wait %s resumes it\n", name, name)
	} else {
		fmt.Fprintf(out, "%s · %s", logs.Run.Name(), logs.Run.State)
	}
	// A passed check's detail is "passed" again.
	if !stopped && logs.Run.Detail != "" && logs.Run.Detail != string(logs.Run.State) {
		fmt.Fprintf(out, ": %s", logs.Run.Detail)
	}
	if !stopped {
		fmt.Fprintln(out)
	}
	if len(logs.Executions) == 0 && logs.Run.State.Terminal() {
		fmt.Fprintln(out, "  No provider runs are recorded. The check either never reached a provider, or cleanup removed the provider records after the branch ended (cleanup.after).")
	}
	for _, execution := range logs.Executions {
		x := execution.Execution
		fmt.Fprintf(out, "  %s, attempt %d, run %s: %s", engine.DescribeEnvironment(x.Environment), x.Attempt, x.ID, x.State)
		if x.Detail != "" {
			fmt.Fprintf(out, " (%s)", x.Detail)
		}
		if x.Reused {
			fmt.Fprint(out, ", reusing earlier builds: nothing was built")
		}
		fmt.Fprintln(out)
		if x.ProviderRef != "" {
			fmt.Fprintf(out, "    %s\n", x.ProviderRef)
		}
		for _, result := range execution.Results {
			line := fmt.Sprintf("    %s %s", result.Target, result.Outcome)
			if result.Phase == model.PhaseDependencies {
				line = fmt.Sprintf("    %s: %s", result.Target, result.Phase.FailureWords())
			} else if result.Phase != "" {
				line += " at " + string(result.Phase)
			}
			if result.Detail != "" {
				line += ": " + result.Detail
			}
			if result.ReusedFrom != "" {
				line += ", as built by run " + string(result.ReusedFrom)
			}
			if result.Log != "" {
				line += "  " + places.Tilde(buildlog.Where(result.Log))
			}
			fmt.Fprintln(out, line)
			// A Git-fetched target's build says what it fetched, or that
			// its provider couldn't say.
			if fetch, ok := execution.Git[result.Target]; ok {
				fmt.Fprintf(out, "      %s\n", engine.FetchedWords(fetch.Expected, fetch.Fetched))
			}
			// A result with several builders shows each one's part.
			for _, part := range result.Builders {
				line := fmt.Sprintf("      %s %s", part.Builder, part.Outcome)
				if part.Outcome == model.OutcomeNotRun {
					line = fmt.Sprintf("      %s didn't build it", part.Builder)
				}
				if part.Phase != "" {
					line += " at " + string(part.Phase)
				}
				if part.Log != "" {
					line += "  " + places.Tilde(buildlog.Where(part.Log))
				}
				fmt.Fprintln(out, line)
			}
		}
	}
	return nil
}
