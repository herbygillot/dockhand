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
)

// cleanStates are the kinds of branch clean takes: --closed and --archived
// add to merged branches, which stay unless --merged=false leaves them, as
// the help's default says (clean --archived left three merged branches,
// unsaid, in the dogfood run with be3f3e06).
func cleanStates(merged, closed, archived bool) []model.BranchState {
	var states []model.BranchState
	if merged {
		states = append(states, model.BranchMerged)
	}
	if closed {
		states = append(states, model.BranchClosed)
	}
	if archived {
		states = append(states, model.BranchArchived)
	}
	return states
}

func cleanCommand(s *settings, streams Streams) *cobra.Command {
	var merged, closed, archived, legacy, yes, automatic bool
	cmd := &cobra.Command{
		Use:   "clean [branch...] [--merged] [--closed] [--archived]",
		Short: "Remove what merged branches leave behind",
		Long: `Removes a merged branch's worktree, local branch, and your fork's branch,
each only while it still holds the merged commit. A worktree with edits or
untracked files, and work that went on past the merge, are kept. The
branch's record stays: status --all lists it as cleaned, and status
<branch> still finds it.

--closed and --archived also take the worktrees of branches whose pull
request was closed without merging, or that were archived, and only their
worktrees: their work isn't merged, so the Git branch, your fork's branch,
and the checkpoints stay, and dockhand path or any command that needs the
worktree checks it out again. A worktree with edits or untracked files is
kept. Merged branches are cleaned with them; --merged=false leaves those.

Whichever branches it cleans, it also removes what checks left in their
providers when the process running them died: a Tart clone, which a check
deletes when it ends, and a later attempt of the same check when it starts.
One is removed only when a check of this checkout made it and no process
is running that check; one no check of this checkout made is kept, since
another database may be using it.

--legacy also sorts the branches earlier dockhand made before v3,
dockhand/bump/<port>-<id>, which nothing tracks: one master has every
commit of, by its change, goes, with your fork's branch where it holds the
same commit; one whose port master has at another version, as a newer
update would leave it, is named for you to look at; and the rest are left
for dockhand adopt. Without it, clean says how many there are.

It reads your open pull requests' state first, as status --refresh does,
so a branch merged since dockhand last looked is cleaned with the rest.

Naming branches cleans those alone, each as its state has it, merged,
closed, or archived, and nothing else: no other branch, nothing a check
left, and no branch from before v3. An open branch has nothing to clean.

It shows what it would remove first; on a terminal it asks, and a script
passes --yes. archive hides a branch; clean removes a merged one's files;
cancel stops a check. None means another.`,
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
			// Which branches merged is their pull requests' to say, read
			// now: two merged a minute before were left out, unsaid, until
			// status --refresh (field testing, 2026-10-02).
			if err := refreshPullRequests(ctx, e, streams.Err); err != nil {
				fmt.Fprintf(streams.Err, "%v; clean goes by the states last read\n", err)
			}
			var plans []engine.CleanBranch
			if len(args) > 0 {
				if legacy {
					return errors.New("--legacy sorts the branches from before v3, not branches named")
				}
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
				// --legacy names what to clean by itself: clean --legacy
				// --merged=false refused as naming nothing (field testing's
				// cleanup, af03bbab).
				states := cleanStates(merged, closed, archived)
				if len(states) == 0 && !legacy {
					return errors.New("nothing to clean: --merged, --closed, or --legacy names what")
				}
				if len(states) > 0 {
					if plans, err = e.PlanClean(ctx, states...); err != nil {
						return err
					}
				}
			}
			session, err := startSession(ctx, e, model.SessionForeground)
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
			var older []engine.LegacyBranch
			if legacy {
				if older, err = e.PlanLegacy(ctx); err != nil {
					return err
				}
			}
			streams.emit(map[string]any{"branches": cleanView(plans), "leftovers": leftoversView(leftovers), "legacy": legacyView(older), "applied": false})
			removable := writeClean(streams.Out, plans, false) + writeLeftovers(streams.Out, leftovers, false) + writeLegacy(streams.Out, older, false)
			if !legacy && len(args) == 0 {
				if names, err := e.LegacyBranchNames(ctx); err == nil && len(names) > 0 {
					fmt.Fprintf(streams.Out, "· %s from before v3 (dockhand/bump/…), which nothing tracks; dockhand clean --legacy sorts them\n", prose.Plural(len(names), "branch"))
				}
			}
			if removable == 0 {
				fmt.Fprintln(streams.Out, "Nothing to remove.")
				return nil
			}
			if !yes {
				if !streams.terminal() {
					fmt.Fprintln(streams.Out, "Nothing was removed; --yes removes these.")
					return nil
				}
				ok, err := confirm(streams, fmt.Sprintf("? Remove %s? [y/N] ", prose.Plural(removable, "item")))
				if err != nil || !ok {
					fmt.Fprintln(streams.Out, "Nothing was removed.")
					return err
				}
			}
			done, err := e.ApplyClean(ctx, plans)
			removed, leftoverErr := e.RemoveLeftovers(ctx, session, leftovers)
			older, legacyErr := e.RemoveLegacy(ctx, older)
			streams.emit(map[string]any{"branches": cleanView(done), "leftovers": leftoversView(removed), "legacy": legacyView(older), "applied": true})
			// What was done is one line a branch, its plan having been
			// shown in full: thirty branches' blocks printed twice were
			// a lot to read (field testing's cleanup, af03bbab).
			fmt.Fprintln(streams.Out)
			writeCleanDone(streams.Out, done)
			writeLeftovers(streams.Out, removed, true)
			if n := removedLegacy(older); n > 0 {
				fmt.Fprintf(streams.Out, "removed  %s from before v3\n", prose.Plural(n, "branch"))
			}
			return errors.Join(err, leftoverErr, legacyErr)
		},
	}
	cmd.Flags().BoolVar(&merged, "merged", true, "merged branches: their worktree, local branch, and fork branch")
	cmd.Flags().BoolVar(&closed, "closed", false, "also branches whose pull request closed unmerged: their worktree only")
	cmd.Flags().BoolVar(&archived, "archived", false, "also archived branches: their worktree only")
	// archive takes the worktree itself now (the command-line UX review,
	// §5); --archived stays for one archived before, hidden.
	_ = cmd.Flags().MarkHidden("archived")
	cmd.Flags().BoolVar(&legacy, "legacy", false, "also sort the branches from before v3, removing those master has")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking")
	cmd.Flags().BoolVar(&automatic, "automatic", false, "run automatic cleanup's pass, when it is due, as a command starts it once its work is done")
	_ = cmd.Flags().MarkHidden("automatic")
	return cmd
}

// writeLegacy says what clean does with the branches from before v3, as
// planned or done, and how many it would remove.
func writeLegacy(out io.Writer, branches []engine.LegacyBranch, done bool) int {
	if len(branches) == 0 {
		return 0
	}
	count := 0
	fmt.Fprintln(out, "Branches from before v3 (dockhand/bump/…):")
	for _, branch := range branches {
		name := branch.Name
		if branch.ForkOnly {
			name = branch.Fork + " (on your fork only)"
		}
		switch {
		case branch.Kind == engine.LegacyOnMaster && branch.Kept != "":
			fmt.Fprintf(out, "  keep     %s: %s\n", name, branch.Kept)
		case branch.Kind == engine.LegacyOnMaster:
			verb := "remove "
			if done && branch.Done {
				verb = "removed"
			} else {
				count++
			}
			fmt.Fprintf(out, "  %s  %s: %s\n", verb, name, branch.Detail)
			if branch.Fork != "" && !branch.ForkOnly {
				fmt.Fprintf(out, "  %s  %s, which holds the same commit\n", verb, branch.Fork)
				if !done || !branch.Done {
					count++
				}
			}
			if branch.ForkKept != "" {
				fmt.Fprintf(out, "  keep     your fork's branch: %s\n", branch.ForkKept)
			}
		case branch.Kind == engine.LegacySuperseded && branch.ForkOnly:
			fmt.Fprintf(out, "  look     %s: %s; removing it from your fork is yours once you've looked\n", name, branch.Detail)
		case branch.Kind == engine.LegacySuperseded:
			fmt.Fprintf(out, "  look     %s: %s; git branch -D %s removes it once you've looked\n", name, branch.Detail, branch.Name)
		case branch.ForkOnly:
			fmt.Fprintf(out, "  keep     %s: %s; fetch it here, and dockhand adopt %s takes it up\n", name, branch.Detail, branch.Name)
		default:
			fmt.Fprintf(out, "  keep     %s: %s; dockhand adopt %s takes it up\n", name, branch.Detail, branch.Name)
		}
	}
	return count
}

func legacyView(branches []engine.LegacyBranch) []map[string]any {
	view := []map[string]any{}
	for _, branch := range branches {
		view = append(view, map[string]any{"name": branch.Name, "head": branch.Head, "kind": string(branch.Kind), "detail": branch.Detail,
			"fork_only": branch.ForkOnly, "fork": branch.Fork, "fork_kept": branch.ForkKept, "kept": branch.Kept, "removed": branch.Done})
	}
	return view
}

// cleanAutomatically is decision 36's automatic pass, as a command starts it
// apart from itself once its own work is done: it runs when still due,
// since another process may have run it meanwhile, stamping first so a
// pass that fails isn't tried again at once, and says what it removed.
func cleanAutomatically(ctx context.Context, e *engine.Engine, streams Streams, file config.File) error {
	due, why := e.CleanupDue(engine.CleanupEvery, file.Cleanup.Free())
	if !due {
		return nil
	}
	if err := e.StampCleanup(); err != nil {
		return err
	}
	session, err := startSession(ctx, e, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	fmt.Fprintf(streams.Out, "%s cleaning up: %s\n", time.Now().Format(time.RFC3339), why.Words)
	report, err := e.Cleanup(ctx, session, file.Cleanup.Age())
	for _, name := range report.Caches {
		fmt.Fprintf(streams.Out, "  removed %s from Tart's cache, unused for %s\n", name, engine.CacheUnused)
	}
	if words := engine.LogCleanupWords(report.Logs, file.Cleanup.Age()); words != "" {
		fmt.Fprintf(streams.Out, "  %s\n", words)
	}
	if words := engine.HistoryWords(report.History, report.Assessments, file.Cleanup.Age()); words != "" {
		fmt.Fprintf(streams.Out, "  %s\n", words)
	}
	fmt.Fprintf(streams.Out, "  removed %s\n", prose.Plural(report.Removed(), "item"))
	return err
}

// cleanupAfter starts automatic cleanup apart from this process once a
// command's own work is done, when it is due (decision 36): a day since the
// last, or free space short, which it says. The command doesn't wait for
// it. serve cleans up itself, and clean is cleaning.
func (s *settings) cleanupAfter(streams Streams, command string) {
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
	if err := startCleanup(s.openedWith); err != nil {
		fmt.Fprintf(streams.Err, "Couldn't start cleaning up in the background: %v\n", err)
	}
}

// writeClean lists what clean would do, or did, and counts what it would
// remove.
func writeClean(out io.Writer, plans []engine.CleanBranch, done bool) int {
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
			line += fmt.Sprintf(" (#%d, merged at %s)", pr.Number, engine.Short(plan.Merged))
		}
		fmt.Fprintln(out, line)
		for _, step := range plan.Steps {
			why := ""
			if step.Why != "" {
				why = ": " + step.Why
			}
			switch {
			case step.Kept != "":
				fmt.Fprintf(out, "  keep     %s: %s\n", cleanWords(step.What), step.Kept)
			case done && step.Done:
				fmt.Fprintf(out, "  removed  %s%s\n", cleanWords(step.What), why)
			default:
				fmt.Fprintf(out, "  remove   %s%s\n", cleanWords(step.What), why)
				count++
			}
		}
		// An unmerged branch's Git branch stays, but for one with nothing
		// master lacks, which goes with its worktree; one master
		// supersedes is said whether or not it has a worktree.
		branchStep := slices.ContainsFunc(plan.Steps, func(s engine.CleanStep) bool { return strings.HasPrefix(s.What, "branch ") })
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
func cleanWords(what string) string {
	if path, ok := strings.CutPrefix(what, "worktree "); ok {
		return "worktree " + tilde(path)
	}
	return what
}

// writeCleanDone says what clean did, one line a branch: what it removed,
// and what it kept and why.
func writeCleanDone(out io.Writer, plans []engine.CleanBranch) {
	for _, plan := range plans {
		var removed, kept []string
		for _, step := range plan.Steps {
			switch {
			case step.Kept != "":
				kept = append(kept, cleanWords(step.What)+" ("+step.Kept+")")
			case step.Done:
				removed = append(removed, cleanWords(step.What))
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

// removedLegacy counts the branches from before v3 clean removed.
func removedLegacy(branches []engine.LegacyBranch) int {
	n := 0
	for _, branch := range branches {
		if branch.Done {
			n++
		}
	}
	return n
}
