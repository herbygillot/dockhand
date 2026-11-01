package view

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// Row is a branch as a line of a table: the words status's overview and
// dockhand console both show of it, from this one projection, so the two
// can't disagree.
type Row struct {
	ID     model.BranchID
	Bucket branchstate.Bucket
	// Mark is what a row needing you is, as its attention marks it, ⋯ for
	// one under way, and Unmarked for the rest.
	Mark string
	// Name is its short name, as a command names it, and Branch the name
	// a row shows, an archived one's said, as clean says it.
	Name, Branch string
	// Ports is the first port it changes, with the version its update
	// chose, and how many more: "jq 1.8.1 +2".
	Ports string
	// Work is its commits and edits, Check its checks' standing, and State
	// what it's doing, the two together.
	Work, Check, State string
	// PR is its pull request's number, review, CI mark, and when MacPorts'
	// maintainer timeout ends, where a port waits on it.
	PR string
	// Next is the command that moves it on: its attention's where it needs
	// you, what follows its work where it's under way, none where it waits
	// on others or is done.
	Next string
}

// BranchRow is a branch's row, by the bucket its facts put it in
// (branchstate.BucketOf).
func BranchRow(status engine.BranchStatus) Row {
	facts := status.Facts()
	row := Row{ID: status.Branch.ID, Bucket: branchstate.BucketOf(facts), Mark: Unmarked, Name: status.Branch.ShortName(), Branch: status.Branch.ShortName(),
		Ports: portsWords(status), Work: workWords(status), Check: CheckState(status), PR: prWords(status)}
	if status.Branch.State == model.BranchArchived {
		row.Branch += " (archived)"
	}
	row.State = row.Work
	if row.Check != "—" {
		row.State += " · " + row.Check
	}
	switch row.Bucket {
	case branchstate.BucketNeedsYou:
		if attention := AttentionFor(status); len(attention) > 0 {
			row.Mark, row.Next = attention[0].Kind.Mark(), attention[0].Next
		}
	case branchstate.BucketWorking:
		row.Mark = Working
		if step, ok := branchstate.Working(facts); ok {
			row.Next = RenderNext(step, "")
		}
	}
	return row
}

// portsWords is the first port a branch changes, or was started for, with
// the version its update chose, and how many more there are.
func portsWords(status engine.BranchStatus) string {
	ports, _ := status.ChangedPorts()
	if len(ports) == 0 {
		ports = status.Branch.Ports
	}
	if len(ports) == 0 || status.Cleaned() {
		return "—"
	}
	words := ports[0]
	for _, found := range status.Releases {
		if found.Port == ports[0] && found.Release.Version != "" {
			words += " " + found.Release.Version
			break
		}
	}
	if len(ports) > 1 {
		words += fmt.Sprintf(" +%d", len(ports)-1)
	}
	return words
}

// workWords are a branch's commits and edits, or what became of them.
func workWords(s engine.BranchStatus) string {
	switch {
	case s.Cleaned() && s.Branch.CleanKept != "":
		return "cleaned, " + s.Branch.CleanKept
	case s.Cleaned():
		return "cleaned"
	// A merged or closed branch clean kept is kept, and why, never
	// cleaned.
	case s.Branch.State != model.BranchOpen && s.Branch.CleanKept != "":
		return s.Branch.CleanKept
	case s.Missing:
		return "branch gone"
	case s.Commits == 0 && len(s.Edited) == 0:
		return "nothing yet"
	case s.Commits == 0:
		return "edits, uncommitted"
	case len(s.Edited) > 0:
		return prose.Plural(s.Commits, "commit") + " + edits"
	}
	return prose.Plural(s.Commits, "commit")
}

// prWords are a branch's pull request: its number, whether it's a draft,
// its state or review, its CI's mark, when MacPorts' maintainer timeout
// ends, and whether what's here is pushed.
func prWords(s engine.BranchStatus) string {
	pr := s.Branch.PullRequest
	if pr == nil {
		return "—"
	}
	words := fmt.Sprintf("#%d", pr.Number)
	if pr.Draft {
		words += " draft"
	}
	if observed := pr.Observed; observed != nil {
		switch {
		case observed.State != model.PullRequestOpen:
			words += " " + string(observed.State)
		case observed.Review == model.ReviewChangesRequested:
			words += " changes requested"
		case observed.Review == model.ReviewApproved:
			words += " approved"
		}
		switch observed.Checks {
		case model.ChecksPassing:
			words += ", CI ✓"
		case model.ChecksFailing:
			words += ", CI ✗"
		case model.ChecksPending:
			words += ", CI …"
		}
		words += mergeRowWords(s)
	}
	if !s.Missing && !s.Pushed() {
		words += ", not pushed"
	}
	return words
}
