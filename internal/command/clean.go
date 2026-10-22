package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// cleanStates are the kinds of branch clean takes: --closed adds to merged
// branches, which stay unless --merged=false leaves them, as the help's
// default says (clean --archived left three merged branches, unsaid, in
// the dogfood run with be3f3e06). An archived branch is cleaned by name:
// archive takes its worktree itself.
func cleanStates(merged, closed bool) []model.BranchState {
	var states []model.BranchState
	if merged {
		states = append(states, model.BranchMerged)
	}
	if closed {
		states = append(states, model.BranchClosed)
	}
	return states
}

func cleanCommand(s *settings, streams term.Streams) *cobra.Command {
	var merged, closed, yes, automatic, forget bool
	cmd := &cobra.Command{
		Use:   "clean [branch...] [--merged] [--closed]",
		Short: "Remove what merged branches leave behind",
		Long: `Removes a merged branch's worktree, local branch, and your fork's branch,
each only while it still holds the merged commit. A worktree with edits or
untracked files, and work that went on past the merge, are kept. The
branch's record stays: status --all lists it as cleaned, and status
<branch> still finds it.

--closed also takes the worktrees of branches whose pull request was
closed without merging, and only their worktrees: their work isn't merged,
so the Git branch, your fork's branch, and the checkpoints stay, and
dockhand path or any command that needs the worktree checks it out again.
A worktree with edits or untracked files is kept. Merged branches are
cleaned with them; --merged=false leaves those.

Whichever branches it cleans, it also removes what checks left in their
providers when the process running them died: a Tart clone, which a check
deletes when it ends, and a later attempt of the same check when it starts.
One is removed only when a check of this checkout made it and no process
is running that check; one no check of this checkout made is kept, since
another database may be using it.

It reads your open pull requests' state first, as status --refresh does,
so a branch merged since dockhand last looked is cleaned with the rest.

Naming branches cleans those alone, each as its state has it, merged,
closed, or archived, and nothing else: no other branch, nothing a check
left, and no branch from before v3. An open branch has nothing to clean.

It shows what it would remove first; on a terminal it asks, and a script
passes --yes. archive hides a branch; clean removes a merged one's files;
cancel stops a check. None means another.

--delete forgets archived branches named: it deletes each one's Git branch,
unmerged commits included, which it lists first, and its worktree, and
forgets its record, keeping of its checks only what another branch's
results were reused from. It asks, and without a terminal needs --yes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if automatic {
				if len(args) > 0 {
					return errors.New("--automatic cleans what's due, not branches named")
				}
				return cleanAutomatically(ctx, e, streams, s.file)
			}
			if forget {
				return forgetBranches(ctx, e, streams, args, yes)
			}
			// Which branches merged is their pull requests' to say, read
			// now: two merged a minute before were left out, unsaid, until
			// status --refresh (field testing, 2026-10-02).
			if err := refreshPullRequests(ctx, e, streams.Err); err != nil {
				fmt.Fprintf(streams.Err, "%v; clean goes by the states last read\n", err)
			}
			var plans []engine.CleanBranch
			if len(args) > 0 {
				var named []model.Branch
				for _, name := range args {
					branch, err := e.ResolveRecord(ctx, name)
					if err != nil {
						return err
					}
					named = append(named, branch)
				}
				if plans, err = e.PlanCleanBranches(ctx, named); err != nil {
					return err
				}
			} else {
				states := cleanStates(merged, closed)
				if len(states) == 0 {
					return errors.New("nothing to clean: --merged or --closed names what")
				}
				if plans, err = e.PlanClean(ctx, states...); err != nil {
					return err
				}
			}
			session, err := e.Session(ctx, model.SessionForeground)
			if err != nil {
				return err
			}
			defer session.End(context.WithoutCancel(ctx))
			// Branches named are cleaned alone: what other checks left is
			// for a clean that names none.
			var leftovers []engine.Leftover
			if len(args) == 0 {
				if leftovers, err = e.PlanLeftovers(ctx, session); err != nil {
					return err
				}
			}
			streams.Emit(map[string]any{"branches": cleanView(plans), "leftovers": leftoversView(leftovers), "applied": false})
			removable := writeClean(streams.Out, plans, false, s.places()) + writeLeftovers(streams.Out, leftovers, false)
			if removable == 0 {
				fmt.Fprintln(streams.Out, "Nothing to remove.")
				return nil
			}
			ok, _, err := streams.Decide(yes, term.Decision{Question: fmt.Sprintf("? Remove %s? [y/N] ", prose.Plural(removable, "item")), Undone: "nothing was removed", ByYes: "--yes removes these"})
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(streams.Out, "Nothing was removed.")
				return nil
			}
			done, err := e.ApplyClean(ctx, plans)
			removed, leftoverErr := e.RemoveLeftovers(ctx, session, leftovers)
			streams.Emit(map[string]any{"branches": cleanView(done), "leftovers": leftoversView(removed), "applied": true})
			// What was done is one line a branch, its plan having been
			// shown in full: thirty branches' blocks printed twice were
			// a lot to read (field testing's cleanup, af03bbab).
			fmt.Fprintln(streams.Out)
			writeCleanDone(streams.Out, done, s.places())
			writeLeftovers(streams.Out, removed, true)
			return errors.Join(err, leftoverErr)
		},
	}
	cmd.Flags().BoolVar(&merged, "merged", true, "merged branches: their worktree, local branch, and fork branch")
	cmd.Flags().BoolVar(&closed, "closed", false, "also branches whose pull request closed unmerged: their worktree only")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking")
	cmd.Flags().BoolVar(&forget, "delete", false, "forget the archived branches named: their Git branch, worktree, and record")
	cmd.Flags().BoolVar(&automatic, "automatic", false, "run automatic cleanup's pass, when it is due, as a command starts it once its work is done")
	_ = cmd.Flags().MarkHidden("automatic")
	supportsJSON(cmd)
	return cmd
}

// cleanAutomatically is decision 36's automatic pass, as a command starts it
// apart from itself once its own work is done, and says what it removed.
func cleanAutomatically(ctx context.Context, e *engine.Engine, streams term.Streams, file config.File) error {
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	report, ran, err := e.CleanupIfDue(ctx, session, engine.CleanupEvery, file.Cleanup.Free(), file.Cleanup.Age(), func(why engine.CleanupReason) {
		fmt.Fprintf(streams.Out, "%s cleaning up: %s\n", time.Now().Format(time.RFC3339), why.Words)
	})
	if !ran {
		return err
	}
	for _, line := range report.Words(file.Cleanup.Age()) {
		fmt.Fprintf(streams.Out, "  %s\n", line)
	}
	fmt.Fprintf(streams.Out, "  removed %s\n", prose.Plural(report.Removed(), "item"))
	return err
}

// cleanupAfter starts automatic cleanup apart from this process once a
// command's own work is done, when it is due (decision 36): a day since the
// last, or free space short, which it says. The command doesn't wait for
// it. serve cleans up itself, and clean is cleaning.
func cleanupAfter(s *settings, streams term.Streams, command string) {
	e := s.opened
	if e == nil || !s.file.Cleanup.On() || command == "clean" || command == "serve" {
		return
	}
	due, why := e.CleanupDue(engine.CleanupEvery, s.file.Cleanup.Free())
	if !due {
		return
	}
	if why.LowSpace {
		fmt.Fprintf(streams.Err, "Cleaning up in the background: %s.\n", why.Words)
	}
	if err := s.host.startCleanup(s.openedWith); err != nil {
		fmt.Fprintf(streams.Err, "Couldn't start cleaning up in the background: %v\n", err)
	}
}

// writeClean lists what clean would do, or did, and counts what it would
// remove.
func writeClean(out io.Writer, plans []engine.CleanBranch, done bool, places config.Places) int {
	count := 0
	for _, plan := range plans {
		line := plan.Branch.ShortName()
		pr := plan.Branch.PullRequest
		switch {
		case plan.Branch.State == model.BranchArchived:
			line += " (archived)"
		case pr != nil && plan.Branch.State == model.BranchClosed:
			line += fmt.Sprintf(" (#%d, closed unmerged)", pr.Number)
		case pr != nil:
			line += fmt.Sprintf(" (#%d, merged at %s)", pr.Number, plan.Merged.Short())
		}
		fmt.Fprintln(out, line)
		for _, step := range plan.Steps {
			why := ""
			if step.Why != "" {
				why = ": " + step.Why
			}
			switch {
			case step.Kept != "":
				fmt.Fprintf(out, "  keep     %s: %s\n", cleanWords(places, step.What), step.Kept)
			case done && step.Done:
				fmt.Fprintf(out, "  removed  %s%s\n", cleanWords(places, step.What), why)
			default:
				fmt.Fprintf(out, "  remove   %s%s\n", cleanWords(places, step.What), why)
				count++
			}
		}
		// An unmerged branch's Git branch stays, but for one with nothing
		// master lacks, which goes with its worktree; one master
		// supersedes is said whether or not it has a worktree.
		branchStep := slices.ContainsFunc(plan.Steps, func(s engine.CleanStep) bool { return s.Kind == engine.CleanBranchRef })
		switch {
		case plan.Branch.State == model.BranchMerged || branchStep:
		case plan.Superseded != "":
			fmt.Fprintf(out, "  look     branch %s: %s; git branch -D %s removes it once you've looked\n", plan.Branch.Name, plan.Superseded, plan.Branch.Name)
		case slices.ContainsFunc(plan.Steps, func(s engine.CleanStep) bool { return s.Kept == "" }):
			fmt.Fprintf(out, "  keep     branch %s: dockhand path %s checks it out again\n", plan.Branch.Name, plan.Branch.ShortName())
		}
	}
	return count
}

// writeLeftovers lists what checks left in providers, what clean does with
// each, or did, and counts what it would remove.
func writeLeftovers(out io.Writer, leftovers []engine.Leftover, done bool) int {
	if len(leftovers) == 0 {
		return 0
	}
	fmt.Fprintln(out, "Left by checks")
	count := 0
	for _, leftover := range leftovers {
		switch {
		case leftover.Kept != "":
			fmt.Fprintf(out, "  keep     %s: %s\n", leftover.What, leftover.Kept)
		case done && leftover.Done:
			fmt.Fprintf(out, "  removed  %s, left by %s\n", leftover.What, leftover.Run.Name())
		default:
			fmt.Fprintf(out, "  remove   %s, left by %s\n", leftover.What, leftover.Run.Name())
			count++
		}
	}
	return count
}

// cleanWords abbreviates a worktree's path under your home.
func cleanWords(places config.Places, what string) string {
	if path, ok := strings.CutPrefix(what, "worktree "); ok {
		return "worktree " + places.Tilde(path)
	}
	return what
}

// writeCleanDone says what clean did, one line a branch: what it removed,
// and what it kept and why.
func writeCleanDone(out io.Writer, plans []engine.CleanBranch, places config.Places) {
	for _, plan := range plans {
		var removed, kept []string
		for _, step := range plan.Steps {
			switch {
			case step.Kept != "":
				kept = append(kept, cleanWords(places, step.What)+" ("+step.Kept+")")
			case step.Done:
				removed = append(removed, cleanWords(places, step.What))
			}
		}
		line := plan.Branch.ShortName()
		if len(removed) > 0 {
			line += ": removed " + strings.Join(removed, ", ")
		}
		if len(kept) > 0 {
			sep := ": "
			if len(removed) > 0 {
				sep = "; "
			}
			line += sep + "kept " + strings.Join(kept, ", ")
		}
		if len(removed) > 0 || len(kept) > 0 {
			fmt.Fprintln(out, line)
		}
	}
}

// forgetBranches is clean --delete: the archived branches named, each said
// with what goes, its unmerged commits listed, then forgotten once asked.
func forgetBranches(ctx context.Context, e *engine.Engine, streams term.Streams, names []string, yes bool) error {
	if len(names) == 0 {
		return errors.New("--delete forgets archived branches named; name each")
	}
	var plans []engine.ForgetPlan
	for _, name := range names {
		branch, err := e.ResolveRecord(ctx, name)
		if err != nil {
			return err
		}
		plan, err := e.PlanForget(ctx, branch)
		if err != nil {
			return err
		}
		plans = append(plans, plan)
	}
	out := streams.Out
	for _, plan := range plans {
		fmt.Fprintf(out, "%s · archived\n", plan.Branch.ShortName())
		if plan.Worktree != "" {
			fmt.Fprintf(out, "  worktree %s\n", plan.Worktree)
		}
		if plan.Head != "" {
			fmt.Fprintf(out, "  branch %s at %s\n", plan.Branch.Name, model.ObjectID(plan.Head).Short())
		}
		if len(plan.Unmerged) > 0 {
			fmt.Fprintf(out, "  ! %s master doesn't have, deleted with it:\n", prose.Plural(len(plan.Unmerged), "commit"))
			for _, commit := range plan.Unmerged {
				fmt.Fprintf(out, "    %s %s\n", model.ObjectID(commit.ID).Short(), commit.Subject)
			}
		}
		fmt.Fprintln(out, "  its record, but for what other branches' results reused of its checks")
	}
	ok, _, err := streams.Decide(yes, term.Decision{Question: fmt.Sprintf("? Forget %s? [y/N] ", prose.Plural(len(plans), "branch")), Undone: "nothing was forgotten", ByYes: "--yes forgets these"})
	if err != nil || !ok {
		if err == nil {
			fmt.Fprintln(out, "Nothing was forgotten.")
		}
		return err
	}
	var forgotten []string
	for _, plan := range plans {
		if _, err := e.Forget(ctx, plan); err != nil {
			return err
		}
		fmt.Fprintf(out, "Forgot %s.\n", plan.Branch.ShortName())
		forgotten = append(forgotten, plan.Branch.ShortName())
	}
	streams.Emit(map[string]any{"forgotten": forgotten})
	return nil
}
