package branchflags

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/spf13/cobra"
)

// branchChoice is the branch an authoring command was pointed at.
type BranchChoice struct {
	Branch string
	// new is --new's name, or newNamed where it named none; empty where
	// --new wasn't given.
	New string
}

// newNamed is --new given without a name: the branch is named for the
// port, or the version an update moves to.
const NewNamed = "\x00"

// isNew is whether --new was given; newName is the name it gave, if any.
func (c BranchChoice) IsNew() bool { return c.New != "" }

func (c BranchChoice) NewName() string {
	if c.New == NewNamed {
		return ""
	}
	return c.New
}

// nextIn is a Next: line's steps, run in a branch: as they are where it's
// checked out, and after a cd into its worktree elsewhere, since a check
// from outside takes its committed head, not its edits. A Next: names
// only what would be accepted (the release bar's rule), so the branch's
// steps run where the branch is.
func NextIn(ctx context.Context, e *engine.Engine, branch model.Branch, steps ...string) string {
	line := strings.Join(steps, ", then ")
	if current, err := e.Current(ctx); err == nil && current.ID == branch.ID {
		return line
	}
	return fmt.Sprintf(`cd "$(dockhand path %s)", then %s`, branch.ShortName(), line)
}

// chosenBranch is the branch an authoring command works in, and whether it
// was started for it; or, for forVersion, none yet: an update starts it,
// named for the version it finds.
type ChosenBranch struct {
	Branch     model.Branch
	Started    bool
	ForVersion bool
}

// chooseBranch is the branch an authoring command works in, as
// engine.Select finds it: -b's, an existing one; one --new starts; the one
// checked out here; or the port's own open branch, one started for it
// where none changes it, said first. Where several change it, a terminal
// is asked once which, or for a new one, and a script is told them.
func ChooseBranch(ctx context.Context, e *engine.Engine, streams term.Streams, where BranchChoice, port string, work engine.Work) (ChosenBranch, error) {
	selection, err := e.Select(ctx, engine.Selector{Branch: where.Branch, New: where.IsNew(), NewName: where.NewName(), Port: port, Policy: engine.SelectOrStart}, work)
	if err != nil {
		return ChosenBranch{}, err
	}
	return SettleSelection(ctx, e, streams, selection, port, work, !where.IsNew())
}

// settleSelection takes a selection: the branch found, said where it was
// chosen for the port; the one to start, started, or for an update left
// for it to start; or the choices, asked about once, a new branch among
// them where the work may start one (the split's 7d row 7: the two
// questions, "start another?" and "which one?", as one), and refused in a
// script. A new branch chosen is the one Select starts for --new. say is
// a command with --new where it wasn't given, which says the branch it
// starts, and that --new starts another.
func SettleSelection(ctx context.Context, e *engine.Engine, streams term.Streams, selection engine.Selection, port string, work engine.Work, say bool) (ChosenBranch, error) {
	switch {
	case selection.ForVersion:
		fmt.Fprintf(streams.Err, "%s is in no open branch, so this starts one for it, named for the version it moves to.\n", port)
		return ChosenBranch{ForVersion: true}, nil
	case selection.Start != nil:
		if say {
			fmt.Fprintf(streams.Err, "%s is in no open branch, so this starts %s for it.\n", port, engine.BranchName(selection.Start.Name))
		}
		branch, err := e.Start(ctx, *selection.Start)
		return ChosenBranch{Branch: branch, Started: err == nil}, err
	case len(selection.Choices) > 0:
		chosen, start, err := choose(ctx, e, streams, port, selection.Choices, work != engine.WorkNone)
		if err != nil || !start {
			return ChosenBranch{Branch: chosen}, err
		}
		another, err := e.Select(ctx, engine.Selector{Port: port, New: true}, work)
		if err != nil {
			return ChosenBranch{}, err
		}
		return SettleSelection(ctx, e, streams, another, port, work, false)
	}
	if selection.Said != "" {
		more := ""
		if say && work != engine.WorkNone {
			more = "; --new starts another"
		}
		fmt.Fprintf(streams.Err, "Working in %s, %s%s.\n", selection.Branch.ShortName(), selection.Said, more)
	}
	return ChosenBranch{Branch: selection.Branch}, nil
}

// choose asks which of the open branches changing a port to work in, one
// question, a new branch among the answers where one may be started; a
// script is told them all, by exact name.
func choose(ctx context.Context, e *engine.Engine, streams term.Streams, port string, found []engine.PortBranch, mayStart bool) (model.Branch, bool, error) {
	if !streams.Terminal() {
		return model.Branch{}, false, &engine.AmbiguousError{Port: port, Branches: found}
	}
	fmt.Fprintf(streams.Err, "%s is changed in %d open branches:\n", port, len(found))
	for i, f := range found {
		fmt.Fprintf(streams.Err, "  %d. %s\n", i+1, branchPurpose(f))
	}
	question := fmt.Sprintf("? which one? [1-%d] ", len(found))
	if mayStart {
		question = fmt.Sprintf("? which one, or n for a new branch? [1-%d/n] ", len(found))
	}
	answer, err := streams.Ask(question)
	if err != nil {
		return model.Branch{}, false, err
	}
	if mayStart && strings.EqualFold(answer, "n") {
		return model.Branch{}, true, nil
	}
	n, err := strconv.Atoi(answer)
	if err != nil || n < 1 || n > len(found) {
		return model.Branch{}, false, errors.New("nothing chosen, so nothing changed")
	}
	return found[n-1].Branch, false, nil
}

// startFor starts the branch Select names for the work on port, as --new
// would.
func StartFor(ctx context.Context, e *engine.Engine, port string, work engine.Work) (model.Branch, bool, error) {
	selection, err := e.Select(ctx, engine.Selector{Port: port, New: true}, work)
	if err != nil {
		return model.Branch{}, false, err
	}
	branch, err := e.Start(ctx, *selection.Start)
	return branch, err == nil, err
}

// latestCheckHere is the newest check of the branch checked out here, for
// the run commands with none named; unnamed is what to say where no branch
// is checked out.
func latestCheckHere(ctx context.Context, e *engine.Engine, unnamed string) (model.Run, error) {
	branch, err := e.Current(ctx)
	if errors.Is(err, engine.ErrNoBranch) {
		return model.Run{}, errors.New(unnamed)
	}
	if err != nil {
		return model.Run{}, err
	}
	run, ok, err := e.LatestCheck(ctx, branch.ID)
	if err != nil {
		return model.Run{}, err
	}
	if !ok {
		return model.Run{}, fmt.Errorf("%s has no check yet: dockhand check", branch.ShortName())
	}
	return run, nil
}

// namedOrLatest is the check args name, or with none, the latest check of
// the branch -b, -p, or --pr names, else of the one checked out here: a
// branch has one check at a time, so it is the one queued or running, if
// any is (the second look's §3). unnamed says what to do elsewhere.
func NamedOrLatest(ctx context.Context, e *engine.Engine, streams term.Streams, where BranchFlags, args []string, unnamed string) (model.Run, error) {
	if len(args) == 1 {
		if where.Explicit() {
			return model.Run{}, errors.New("name a check or a branch, not both")
		}
		return e.RunNamed(ctx, args[0])
	}
	return LatestOf(ctx, e, streams, where, unnamed)
}

// latestOf is the latest check of the branch the flags name, else of the
// one checked out here.
func LatestOf(ctx context.Context, e *engine.Engine, streams term.Streams, where BranchFlags, unnamed string) (model.Run, error) {
	if !where.Explicit() {
		return latestCheckHere(ctx, e, unnamed)
	}
	branch, err := where.Resolve(ctx, e, streams)
	if err != nil {
		return model.Run{}, err
	}
	run, ok, err := e.LatestCheck(ctx, branch.ID)
	if err != nil {
		return model.Run{}, err
	}
	if !ok {
		return model.Run{}, fmt.Errorf("%s has no check yet: dockhand check -b %s", branch.ShortName(), branch.ShortName())
	}
	return run, nil
}

// Register adds -b and --new, one at a time, to an authoring command.
func (c *BranchChoice) Register(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&c.Branch, "branch", "b", "", "work in this existing branch, by its exact name (the dockhand/ prefix is optional); --new=<name> starts one")
	cmd.Flags().StringVar(&c.New, "new", "", "start a new branch for this, in its own worktree; --new=<name> names it")
	cmd.Flags().Lookup("new").NoOptDefVal = NewNamed
	cmd.MarkFlagsMutuallyExclusive("branch", "new")
}
