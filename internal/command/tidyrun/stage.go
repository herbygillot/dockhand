package tidyrun

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// from tidy/tidy.go

// decideTidy applies a tidy plan, or on a terminal reviews it first. An
// explicit plan, the person's own, applies as given; without a terminal,
// or with --yes, a plan made only of dockhand's own edits applies too,
// and anything else needs review. It reports whether the plan applied.
func Decide(ctx context.Context, e *engine.Engine, streams term.Streams, proposal engine.TidyPlan, explicit, yes bool, author string) (bool, error) {
	out := streams.Out
	// --yes applies the plan as shown, whosever its edits are, as --yes
	// does elsewhere: a hand edit's plan tidy proposed itself had no way
	// to be taken without a terminal (field testing's seventh report).
	if explicit || !streams.Terminal() || yes {
		answered := model.AnsweredNobody
		if yes {
			answered = model.AnsweredYes
		}
		if err := proposal.Gate().AppliesUnasked(branchstate.Mode{Answered: answered}, explicit); err != nil {
			return false, err
		}
		return true, Apply(ctx, e, streams, proposal)
	}
	for {
		answer, err := streams.Ask("Review diff [d] · Change groups [g] · Edit message [e] · Apply [a] · Cancel [q]\n> ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "d":
			diff, err := e.Repo.DiffTrees(ctx, proposal.BaseTree, proposal.Final)
			if err != nil {
				return false, err
			}
			fmt.Fprint(out, string(diff))
		case "g":
			spec, err := streams.Ask("Enter commit numbers in order; use + to combine them (for example, 2 1+3): ")
			if err != nil {
				return false, err
			}
			regrouped, err := e.Regroup(ctx, proposal, spec, author)
			if err != nil {
				fmt.Fprintf(streams.Err, "Not changed: %v\n", err)
				continue
			}
			proposal = regrouped
			view.WriteTidyPlan(out, proposal, false)
		case "e":
			if err := editSubjects(streams, &proposal); err != nil {
				return false, err
			}
			view.WriteTidyPlan(out, proposal, false)
		case "a":
			if blocking := proposal.Blocking(); len(blocking) > 0 {
				fmt.Fprintf(streams.Err, "Can't apply this plan: %s\n", strings.Join(blocking, "; "))
				continue
			}
			return true, Apply(ctx, e, streams, proposal)
		case "q", "":
			fmt.Fprintln(out, "Nothing changed.")
			return false, nil
		}
	}
}

// editSubjects asks for each commit's subject, keeping the rest of its
// message.
func editSubjects(streams term.Streams, plan *engine.TidyPlan) error {
	for i := range plan.Groups {
		group := &plan.Groups[i]
		answer, err := streams.Ask(fmt.Sprintf("Subject for commit %d [%s]: ", i+1, group.Subject()))
		if err != nil {
			return err
		}
		if answer != "" {
			group.SetSubject(answer)
		}
	}
	return nil
}

func Apply(ctx context.Context, e *engine.Engine, streams term.Streams, plan engine.TidyPlan) error {
	result, err := e.ApplyTidy(ctx, plan)
	if err != nil {
		return err
	}
	applied := view.TidyView(plan)
	applied.Applied = &view.TidyAppliedJSON{Checkpoint: result.Checkpoint.Name(), Commits: result.Commits, Kept: result.Kept}
	streams.Emit(applied)
	name := result.Checkpoint.Name()
	made := fmt.Sprintf("Created %s", prose.Plural(len(result.Commits), "commit"))
	if result.Kept > 0 {
		as := "they were"
		if result.Kept == 1 {
			as = "it was"
		}
		made = fmt.Sprintf("Kept %s as %s, and created %d", prose.Plural(result.Kept, "commit"), as, len(result.Commits)-result.Kept)
	}
	fmt.Fprintf(streams.Out, "%s. The files are unchanged.\nCheckpoint %s keeps the old history (dockhand undo %s).\n", made, name, name)
	if len(result.Narrowed) > 0 {
		fmt.Fprintf(streams.Out, "Removed %s from the sparse checkout; the branch has no changes there.\n", strings.Join(result.Narrowed, ", "))
	}
	if warning := view.ModifiedBuildWarning(plan, name); warning != "" {
		fmt.Fprintln(streams.Err, warning)
	}
	switch {
	case plan.Branch.PullRequest == nil || len(plan.History) == 0:
	case result.Kept == len(plan.History):
		// Every commit it has stays, so submitting adds the rest to it.
		fmt.Fprintf(streams.Out, "#%d gains %s when you submit, on top of the %s it has.\n", plan.Branch.PullRequest.Number, prose.Plural(len(result.Commits)-result.Kept, "commit"), prose.Plural(len(plan.History), "commit"))
	default:
		fmt.Fprintf(streams.Out, "#%d still shows %s until you submit; submit will replace its history, if no one else has pushed.\n", plan.Branch.PullRequest.Number, prose.Plural(len(plan.History), "commit"))
	}
	return nil
}

// Stage commits a branch's edits as tidy would plan them, previewed as
// tidy previews them, where it has any, and returns the branch as it is
// after; applied is false where the person declined, which undone says.
// An unambiguous plan applies without asking (Decide). It's the tidy
// stage ship and submit each take (Design v3 §3's chaining).
func Stage(ctx context.Context, e *engine.Engine, streams term.Streams, branch model.Branch, undone string) (model.Branch, bool, error) {
	out := streams.Out
	proposal, err := e.PlanTidy(ctx, engine.TidyRequest{Branch: branch})
	if err != nil {
		return branch, false, err
	}
	if !proposal.Keep {
		fmt.Fprintf(out, "\n%s · tidying %s\n", branch.ShortName(), view.DescribeWork(proposal))
		streams.Emit(view.TidyView(proposal))
		view.WriteTidyPlan(out, proposal, false)
		// A plan of dockhand's own edits is applied unasked; one with the
		// person's is reviewed on a terminal, and held where nobody looks,
		// --yes or no terminal: --yes skips ship's question, and approves no
		// plan it hasn't shown (Codex's review of 832f059e, high 1). The
		// hold names tidy, as serve's does, where tidy's own refusal
		// offered --yes, which ship's --yes isn't.
		if !streams.Terminal() {
			if err := proposal.Gate().CommitUnasked(branchstate.Mode{Answered: model.AnsweredNobody}); err != nil {
				return branch, false, fmt.Errorf("%w; %s", err, undone)
			}
		}
		applied, err := Decide(ctx, e, streams, proposal, false, proposal.Unambiguous(), "")
		if err != nil {
			return branch, false, fmt.Errorf("%w; %s", err, undone)
		}
		if !applied {
			fmt.Fprintf(out, "%s.\n", prose.Sentence(undone))
			return branch, false, nil
		}
	}
	branch, err = e.Resolve(ctx, branch.ShortName())
	return branch, err == nil, err
}
