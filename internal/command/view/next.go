package view

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/term"
)

// RenderNext is a Next as the command line it names: the one place a
// verdict's next step becomes words (architecture, 2026-10-22). reason
// adds the words a row's own next carries, as a submit after someone
// else's push showing the comparison.
func RenderNext(next branchstate.Next, reason branchstate.Reason) string {
	branch := " --branch " + next.Branch
	args := ""
	if len(next.Args) > 0 {
		args = " " + strings.Join(next.Args, " ")
	}
	if next.Verb == "" {
		return next.External
	}
	var line string
	switch next.Verb {
	case branchstate.VerbRefresh:
		return "dockhand status --refresh (refreshes the branch status); dockhand adopt <new name> (if you renamed the branch)"
	case branchstate.VerbAdopt:
		return "dockhand adopt <new name> (if you renamed the branch)"
	case branchstate.VerbStatus, branchstate.VerbArchive:
		line = "dockhand " + string(next.Verb) + " " + next.Branch
	case branchstate.VerbWait, branchstate.VerbLogs, branchstate.VerbEdit, refusal.VerbExplain:
		line = "dockhand " + string(next.Verb) + args
	case refusal.VerbSetup:
		return "dockhand setup" + args
	case branchstate.VerbRerunCI:
		return "dockhand retry --ci --branch " + next.Branch
	case branchstate.VerbOpenChecks:
		number, _ := strconv.Atoi(next.Args[1])
		return "open " + github.PullRequestChecksURL(next.Args[0], number)
	default:
		line = "dockhand " + string(next.Verb) + branch + args
	}
	if next.After == branchstate.AfterFork {
		line = "fork macports/macports-ports on GitHub and add it as a Git remote, then " + line
	}
	if reason == branchstate.SomeoneElsePushed {
		line += " (it shows the comparison)"
	}
	return line
}

// CheckState is the checks column: what is running, else what the latest
// check says about the files as they are now.
func CheckState(s engine.BranchStatus) string {
	suffix := ""
	if !s.Current {
		suffix = ", for older work"
	}
	switch s.CheckStanding() {
	case branchstate.CheckStopped:
		return fmt.Sprintf("stopped (%s)", s.Stopped.Name())
	case branchstate.Checking:
		run := *s.Checking()
		return fmt.Sprintf("%s (%s)", run.State, run.Name())
	case branchstate.NotChecked:
		return "—"
	case branchstate.CheckIncomplete:
		// A passed check whose results no longer all stand, as after its
		// environment changed, isn't a pass for the branch now. An archived
		// branch asks nothing of its checks, as its attention doesn't, so
		// its check is said as it ended: archived duckdb-cxx14's read
		// "needs another check" (the dogfood run with 3086fdb3).
		if s.Branch.State == model.BranchArchived {
			return fmt.Sprintf("passed (%s)", s.Latest.Name())
		}
		return "previous check passed; a new check is required"
	case branchstate.CheckedOlder:
		return "passed for an earlier version of the files"
	case branchstate.CheckPassed:
		// Untracked files a check took in with --include are compared as
		// it had them, and said: they're in no commit yet.
		if len(s.Included) > 0 {
			return fmt.Sprintf("passed for %s, including untracked %s", engine.Describe(*s.LatestRevision), prose.And(s.Included))
		}
		// A check of the working files that tidy then committed unchanged
		// checked the commit's files, as submit credits it.
		if s.LatestRevision.Kind == model.RevisionCommit || s.Commits > 0 && len(s.Edited) == 0 {
			return "passed for this commit"
		}
		return "passed for " + engine.Describe(*s.LatestRevision)
	case branchstate.CheckFailed:
		if s.Evidence == nil {
			return fmt.Sprintf("failed (%s)", s.Latest.Name())
		}
		failed := len(s.Evidence.Failed())
		return fmt.Sprintf("%d failed, %d passed%s", failed, len(s.Evidence.Targets)-failed, suffix)
	}
	return "needs attention" + suffix
}

// WriteNext says what moves a branch on, as its attention row does.
func WriteNext(out io.Writer, status engine.BranchStatus) {
	for _, row := range AttentionFor(status) {
		fmt.Fprintf(out, "Next: %s\n", row.Next)
	}
}

// SayNext is a refusal whose producer gave what moves it on, as a login
// to sign in to again or a Tart image to set up, with that said last, as
// status says a branch's: its words say what happened, and Next the
// command (R3-31). A composed exit error says its own.
func SayNext(err error) error {
	r := refusal.Of(err)
	if r == nil || len(r.Next) == 0 || errors.As(err, new(*term.ExitError)) {
		return err
	}
	return nextSaid{err, RenderSteps(r.Next)}
}

// RenderSteps are a refusal's steps, each as renderNext renders it, in
// order.
func RenderSteps(steps []refusal.Next) []string {
	var lines []string
	for _, step := range steps {
		lines = append(lines, RenderNext(step, ""))
	}
	return lines
}

// nextSaid is an error with a Next: line for each step, reaching the
// error beneath.
type nextSaid struct {
	error
	steps []string
}

func (n nextSaid) Error() string {
	return n.error.Error() + "\nNext: " + strings.Join(n.steps, "\nNext: ")
}

func (n nextSaid) Unwrap() error { return n.error }
