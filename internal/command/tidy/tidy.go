package tidy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	var message, author, group, saveTo, apply string
	var squash, plan, yes bool
	cmd := &cobra.Command{
		Use:   "tidy",
		Short: "Shape the branch's commits for review",
		Long: `Proposes the commits a reviewer should see: by default one per port
directory, with the subject dockhand's commands wrote or the one your own
commits give, and every uncommitted edit included. The final files are
exactly what you have; tidy never changes a file.

A plan made only of dockhand's own edits applies without review, which is
what a script gets. Anything else is shown for review first, on a terminal,
or applied as shown with --yes.
--squash --message "port: what changed" makes one commit of the whole
branch, applied as given, since the message is yours. Before rewriting,
tidy keeps the old history as a checkpoint that dockhand undo brings
back.

--group rearranges the proposed commits: "2 1+3" makes commit 2 first, then
one commit of 1 and 3. On a terminal, [g] does the same.

--plan --out <file> saves the plan for review; edit its messages there if
you like. --apply <file> applies it, as long as the branch's base, its
commits, and its files are as they were when it was saved.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if message != "" && !squash {
				return errors.New("--message requires --squash. Add --squash to create a single commit with this message.")
			}
			if saveTo != "" && !plan {
				return errors.New("--out saves the plan --plan shows; add --plan")
			}
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if apply != "" {
				return applySaved(ctx, e, streams, apply)
			}
			branch, err := where.Resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			proposal, err := e.PlanTidy(ctx, engine.TidyRequest{Branch: branch, Squash: squash, Message: message, Author: author})
			if err != nil {
				return err
			}
			if group != "" && !proposal.Keep {
				if proposal, err = e.Regroup(ctx, proposal, group, author); err != nil {
					return fmt.Errorf("--group: %w", err)
				}
			}
			streams.Emit(view.TidyView(proposal))
			out := streams.Out
			fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), view.DescribeWork(proposal))
			if proposal.Keep && len(proposal.Warnings) > 0 {
				// What the rules warn of is said, as submit says it, with
				// the plan to rewrite a message in (the rust and cargo run).
				if len(proposal.LeftOut) > 0 {
					fmt.Fprintln(out, "All tracked changes are committed. The commits pass the required MacPorts checks, with these warnings:")
				} else {
					fmt.Fprintln(out, "All changes are committed. The commits pass the required MacPorts checks, with these warnings:")
				}
				for _, warning := range proposal.Warnings {
					fmt.Fprintf(out, "  %s\n", warning)
				}
				view.WriteFindings(out, proposal)
				if !plan || saveTo == "" {
					fmt.Fprintln(out, "To rewrite a message: dockhand tidy --plan --out tidy.toml, edit it there, then dockhand tidy --apply tidy.toml")
					return nil
				}
				data, err := proposal.Save()
				if err != nil {
					return err
				}
				if err := os.WriteFile(saveTo, data, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(out, "Saved the commits as they are to %s. Edit their messages there, then: dockhand tidy --apply %s\n", saveTo, saveTo)
				return nil
			}
			if proposal.Keep {
				// An untracked file in a changed port's directory is
				// uncommitted, whatever Git's index says (net-snmp's
				// patch, check-469).
				if len(proposal.LeftOut) > 0 || len(proposal.Ignored) > 0 {
					fmt.Fprintln(out, "The commits already follow MacPorts' rules, and every tracked change is committed; nothing to tidy.")
				} else {
					fmt.Fprintln(out, "The commits already follow MacPorts' rules, and nothing is uncommitted; nothing to tidy.")
				}
				view.WriteFindings(out, proposal)
				return nil
			}
			view.WriteTidyPlan(out, proposal, false)
			if plan {
				if saveTo == "" {
					return nil
				}
				data, err := proposal.Save()
				if err != nil {
					return err
				}
				if err := os.WriteFile(saveTo, data, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(out, "Saved the plan to %s. Edit its messages there if you like, then: dockhand tidy --apply %s\n", saveTo, saveTo)
				return nil
			}
			// A squash with its message given, or a grouping chosen, is the
			// person's own explicit plan, so it needs no review.
			_, err = Decide(ctx, e, streams, proposal, squash && message != "" || group != "", yes, author)
			return err
		},
	}
	where.Register(cmd, s, "tidy")
	cmd.Flags().BoolVar(&squash, "squash", false, "make one commit of the whole branch")
	cmd.Flags().StringVar(&message, "message", "", "the message of the commit --squash makes")
	cmd.Flags().StringVar(&author, "author", "", "attribute a commit that combines several people's commits: \"Name <email>\"")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the proposed commits and change nothing")
	cmd.Flags().StringVar(&group, "group", "", "rearrange the proposed commits: \"2 1+3\" puts 2 first, then 1 and 3 as one")
	cmd.Flags().StringVar(&saveTo, "out", "", "with --plan, save the plan to this file")
	cmd.Flags().StringVar(&apply, "apply", "", "apply a plan saved with --plan --out")
	cmd.MarkFlagsMutuallyExclusive("apply", "plan")
	cmd.MarkFlagsMutuallyExclusive("apply", "squash")
	cmd.MarkFlagsMutuallyExclusive("apply", "group")
	cmd.MarkFlagsMutuallyExclusive("apply", "branch")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "apply the plan as shown, without asking; a plan made only of dockhand's own edits applies without it")
	settings.SupportsJSON(cmd)
	return cmd
}

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
		return true, applyTidy(ctx, e, streams, proposal)
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
			return true, applyTidy(ctx, e, streams, proposal)
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

// applySaved applies a plan saved with --plan --out. Saving it was the
// review, so it is applied as it stands.
func applySaved(ctx context.Context, e *engine.Engine, streams term.Streams, file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	proposal, err := e.LoadTidyPlan(ctx, data)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	streams.Emit(view.TidyView(proposal))
	fmt.Fprintf(streams.Out, "%s · the plan saved in %s\n", proposal.Branch.ShortName(), file)
	view.WriteTidyPlan(streams.Out, proposal, true)
	if blocking := proposal.Blocking(); len(blocking) > 0 {
		return fmt.Errorf("the plan can't be applied yet: %s", strings.Join(blocking, "; "))
	}
	return applyTidy(ctx, e, streams, proposal)
}

func applyTidy(ctx context.Context, e *engine.Engine, streams term.Streams, plan engine.TidyPlan) error {
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

// undoCommand puts back what a tidy or rebase replaced: the branch's
// latest, or the checkpoint named (the command-line UX review's smaller
// items: git restore restores files, and dockhand's restores history).
func UndoCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use: "undo [<checkpoint>]",
		// v0.3.0's restore points here, not run as an alias.
		SuggestFor: []string{"restore"},
		Short:      "Put back the history the latest tidy or rebase replaced",
		Long: `Puts a branch's commits back as they were before its latest tidy or
rebase, or the checkpoint named, such as tidy-3 or rebase-4, when nothing
has been committed since. After a tidy, the files are not touched: edits
that tidy committed read as uncommitted again. After a rebase, the files go
back as they were before it, with the master the branch started from; a
change to one of those files stops it.

With no checkpoint named, the branch is -b, -p, or --pr, else the one
checked out here.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if len(args) == 1 {
				if where.Explicit() {
					return errors.New("a checkpoint names its branch already; name the checkpoint or the branch, not both")
				}
				return restoreCheckpoint(ctx, e, streams, args[0])
			}
			branch, err := where.Resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			name, err := e.LatestCheckpoint(ctx, branch)
			if err != nil {
				return err
			}
			return restoreCheckpoint(ctx, e, streams, name)
		},
	}
	where.Register(cmd, s, "undo in")
	settings.SupportsJSON(cmd)
	return cmd
}

// restoreCheckpoint puts a branch back as it was before a checkpoint, and
// says what it did.
func restoreCheckpoint(ctx context.Context, e *engine.Engine, streams term.Streams, name string) error {
	checkpoint, branch, err := e.Restore(ctx, name)
	if err != nil {
		return err
	}
	streams.Emit(map[string]any{"checkpoint": checkpoint.Name(), "branch": branch.ShortName(), "head": checkpoint.Before, "base": branch.Base})
	if checkpoint.Kind != model.CheckpointRebase {
		fmt.Fprintf(streams.Out, "Restored %s to its history before %s (%s). The files are unchanged.\n", branch.Name, checkpoint.Name(), checkpoint.Before.Short())
		return nil
	}
	fmt.Fprintf(streams.Out, "Restored %s to its history and files before %s (%s), on master %s again.\n", branch.Name, checkpoint.Name(), checkpoint.Before.Short(), branch.Base.Short())
	return nil
}
