package author

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

// RenameCommand renames a port as MacPorts does, an obsolete stub left at
// its old name (the person's "I agree we add a `rename` action to
// Dockhand.", 2026-10-29).
func RenameCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchChoice
	var category string
	var plan bool
	cmd := &cobra.Command{
		Use:   "rename <port> <new name>",
		Short: "Rename a port, leaving an obsolete stub at its old name",
		Long: `Moves a port's Portfile and files/ to the new name's directory, keeping
its category unless --category names another. Sets the name explicitly,
adding a name line after its setup, such as github.setup or go.setup, if
needed to preserve the source. The old Portfile becomes an obsolete stub
with replaced_by set to the new name, its version unchanged, and its
revision increased. Tidy commits the pair as "<new>: rename from <old>";
check and submit treat it as one change.

Reports remaining references to the old name, uses of ${name}, and ports
that depend on it. Review these and make any changes needed to module
paths, binary names, notes, or other Portfile logic. To update the renamed
port's version afterward, run dockhand update <new name>.

The branch is --branch, else the one checked out here; --new starts one.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return renamePort(cmd, s, streams, where, engine.PortRenameRequest{Port: args[0], Name: args[1], Category: category, Plan: plan})
		},
	}
	where.Register(cmd)
	cmd.Flags().StringVar(&category, "category", "", "category for the renamed port (default: its current category)")
	cmd.Flags().BoolVar(&plan, "plan", false, "show what the rename would write, and change nothing")
	settings.SupportsJSON(cmd)
	return cmd
}

// MoveCommand moves a port to another category's directory, keeping its
// name: field testing ran git mv and edited categories by hand, which
// tidy took as a hand edit (2026-10-29).
func MoveCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchChoice
	var plan bool
	cmd := &cobra.Command{
		Use:   "move <port> <category>",
		Short: "Move a port to another category",
		Long: `Moves a port's Portfile and files/ to the category's directory, keeping its
name, and names the category first on its categories line where the old one
was. It's one edit, so tidy commits it as "<port>: move to <category>", and
check and submit take it as a move.

The branch is --branch, else the one checked out here; --new starts one.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return renamePort(cmd, s, streams, where, engine.PortRenameRequest{Port: args[0], Category: args[1], Plan: plan})
		},
	}
	where.Register(cmd)
	cmd.Flags().BoolVar(&plan, "plan", false, "show what the move would write, and change nothing")
	settings.SupportsJSON(cmd)
	return cmd
}

// renameJSON is a rename's or move's result.
type renameJSON struct {
	Branch            view.BranchRefJSON `json:"branch"`
	Started           bool               `json:"started"`
	Port              string             `json:"port"`
	Name              string             `json:"name"`
	From              string             `json:"from"`
	To                string             `json:"to"`
	Stub              string             `json:"stub,omitempty"`
	Left              []string           `json:"left"`
	UsesName          []string           `json:"uses_name"`
	Dependents        []string           `json:"dependents"`
	DependentsProblem string             `json:"dependents_problem,omitempty"`
	Applied           bool               `json:"applied"`
}

func renamePort(cmd *cobra.Command, s *settings.Settings, streams term.Streams, where branchflags.BranchChoice, request engine.PortRenameRequest) error {
	ctx := cmd.Context()
	e, err := s.Open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	// A plan with no branch to plan in, or with --new, is planned on
	// master and starts none, as update's is: a plan started the branch
	// --new named (Codex's workflow review of 1caa6c3d, finding 3).
	out := streams.Prose()
	fromMaster := request.Plan && where.IsNew()
	var beside string
	if request.Plan && !fromMaster {
		if fromMaster, beside, err = unbranchedPlan(ctx, e, where, request.Port); err != nil {
			return err
		}
	}
	var chosen branchflags.ChosenBranch
	if !fromMaster {
		if chosen, err = branchflags.ChooseBranch(ctx, e, streams, where, request.Port, engine.WorkEdit); err != nil {
			return err
		}
	}
	request.Branch, request.FromMaster = chosen.Branch, fromMaster
	if chosen.Started {
		fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", chosen.Branch.Name, chosen.Branch.Base.Short())
	}
	renamed, err := e.RenamePort(ctx, request)
	if err != nil {
		return err
	}
	if fromMaster {
		why := ""
		if beside != "" {
			why = fmt.Sprintf(", since %s doesn't change %s", beside, request.Port)
		}
		fmt.Fprintf(out, "Planned on master (fetched just now)%s; without --plan it starts a branch\n", why)
	}
	result := renameJSON{Branch: view.BranchRef(chosen.Branch), Started: chosen.Started, Port: renamed.Port, Name: renamed.Name, From: renamed.From, To: renamed.To,
		Left: view.NonNil(renamed.Left), UsesName: view.NonNil(renamed.UsesName), Dependents: view.NonNil(renamed.Dependents), DependentsProblem: renamed.DependentsProblem, Applied: renamed.Applied}
	verb := map[bool]string{true: "would rename", false: "renamed"}[request.Plan]
	what := fmt.Sprintf("%s %s to %s: %s → %s", verb, renamed.Port, renamed.Name, renamed.From, renamed.To)
	if renamed.Stub == nil {
		verb = map[bool]string{true: "would move", false: "moved"}[request.Plan]
		what = fmt.Sprintf("%s %s: %s → %s", verb, renamed.Port, renamed.From, renamed.To)
	} else {
		result.Stub = fmt.Sprintf("%s_%d", renamed.Stub.Version, renamed.Stub.Revision)
		what += fmt.Sprintf(", with an obsolete stub at %s, replaced_by %s, version %s", renamed.From, renamed.Name, result.Stub)
	}
	label := chosen.Branch.ShortName()
	if fromMaster {
		label = "master"
	}
	fmt.Fprintf(out, "%s · %s\n", label, what)
	if renamed.Category {
		fmt.Fprintf(out, "    Its categories line names %s first.\n", strings.SplitN(renamed.To, "/", 2)[0])
	}
	if len(renamed.Left) > 0 {
		fmt.Fprintf(out, "    Still naming %s in %s/Portfile, yours to change:\n", renamed.Port, renamed.To)
		for _, line := range renamed.Left {
			fmt.Fprintf(out, "      %s\n", line)
		}
	}
	if len(renamed.UsesName) > 0 {
		fmt.Fprintf(out, "    Uses ${name}, which now reads %s; yours to check:\n", renamed.Name)
		for _, line := range renamed.UsesName {
			fmt.Fprintf(out, "      %s\n", line)
		}
	}
	switch {
	case len(renamed.Dependents) > 0:
		fmt.Fprintf(out, "    %s on %s, by the port index; their lines are yours: %s\n", map[bool]string{true: "1 port depends", false: prose.Plural(len(renamed.Dependents), "port") + " depend"}[len(renamed.Dependents) == 1], renamed.Port, strings.Join(renamed.Dependents, ", "))
	case renamed.DependentsProblem != "":
		fmt.Fprintf(out, "    The ports depending on %s couldn't be read: %s\n", renamed.Port, renamed.DependentsProblem)
	}
	streams.Emit(result)
	if request.Plan {
		fmt.Fprintln(out, "Plan, nothing changed.")
		return nil
	}
	next := []string{"dockhand check"}
	if renamed.Stub != nil {
		next = []string{"dockhand update " + renamed.Name + " (for a new release)", "dockhand check"}
	}
	fmt.Fprintln(out, "Next: "+branchflags.NextIn(ctx, e, chosen.Branch, next...))
	return nil
}
