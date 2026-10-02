package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
)

// testPreparer, when set, stands in for MacPorts in every engine a
// command opens.
var testPreparer func(*engine.Engine) engine.Preparer

// gitHubCLI is the GitHub CLI every engine a command opens may mark a
// draft ready with, where an organization refuses dockhand's app (D8).
// Tests stand in for it, so no test runs the one installed.
var gitHubCLI engine.GitHubCLI = github.CLI{}

// testForge, when set, stands in for GitHub in every engine a command
// opens.
var testForge func(*engine.Engine) engine.Forge

// branchChoice is the branch an authoring command was pointed at.
type branchChoice struct {
	branch string
	new    bool
}

func (c *branchChoice) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.branch, "branch", "", "work in this tracked branch (the dockhand/ prefix is optional)")
	cmd.Flags().BoolVar(&c.new, "new", false, "start a new branch for this, in its own worktree")
	cmd.MarkFlagsMutuallyExclusive("branch", "new")
}

func updateCommand(s *settings, streams Streams) *cobra.Command {
	var where branchChoice
	var plan bool
	var version versionUpdate
	linked := &version.linked
	var batch outdatedOptions
	cmd := &cobra.Command{
		Use:   "update <port> [version]",
		Short: "Update a port to a newer release",
		Long: `Moves a port to the newest release upstream, or the version named, and fills
in its checksums, in the branch's working files. Nothing is committed.

The branch is --branch, else the one checked out here; --new starts one,
once there is an edit to make, so a port already current starts nothing.
--plan shows the edit and changes nothing: with no branch to plan in, it
plans on master, as --new --plan does, and it never starts one.

--revbump-dependents also bumps the revision of every port that links the
updated one directly, its library dependents in the port index at the
branch's base, so users rebuild them; tidy commits each as "<port>: rebuild
for <updated> <version>". --except leaves a dependent out. --plan lists them
first.

The old and new versions' archives are compared, and a changed license
file, a changed build file, or a new declared dependency is reported: what a
reviewer would ask about, and what a passing build can't catch.

--outdated updates every named port, or with --mine every port you
maintain, that has a newer release: one branch each, from fresh master,
each update committed as one commit. It shows how it splits the work before
starting anything; --check also queues a check of each.

--submit goes on to tidy the edit, check it, and submit exactly that
commit once the check passes: tidy, then submit --check, each previewed.
With --json, the result is the update's, with tidy's, check's, and submit's
inside it, as far as it went. dockhand bump is the same, asking nothing.
Without a terminal, the tidy applies only when it is made of dockhand's own
edits alone; --yes applies such a tidy on a terminal too, without asking.
--on says where to check, and --tested-binaries and --tested-variants tick
the pull request's checkboxes, as they do for submit. Where to check is
settled before anything is edited.`,
		// --outdated takes any number of ports; one port's update takes
		// the port and, optionally, its version.
		Args: func(cmd *cobra.Command, args []string) error {
			if batch.outdated {
				return nil
			}
			return cobra.RangeArgs(0, 2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if linked.submit && (batch.outdated || plan) {
				return errors.New("--submit goes with one port's update, not --plan or --outdated")
			}
			if !linked.submit && (len(linked.on) > 0 || linked.testedBinaries || linked.testedVariants) {
				return errors.New("--on, --tested-binaries, and --tested-variants go with --submit")
			}
			if batch.outdated {
				batch.plan = plan
				return updateOutdated(cmd.Context(), s, streams, args, batch)
			}
			if batch.mine || batch.check {
				return errors.New("--mine and --check go with --outdated")
			}
			if batch.yes && !linked.submit {
				return errors.New("--yes goes with --outdated or --submit")
			}
			if len(args) == 0 {
				return errors.New("name the port to update, or update your outdated ports with --outdated --mine")
			}
			request, err := version.request(args, plan)
			if err != nil {
				return err
			}
			linked.yes = batch.yes
			return version.run(cmd.Context(), s, streams, where, request)
		},
	}
	where.flags(cmd)
	version.flags(cmd, "with --submit, ")
	cmd.Flags().BoolVar(&plan, "plan", false, "show the edit and change nothing")
	cmd.Flags().BoolVar(&linked.submit, "submit", false, "then tidy it, check it, and submit it once the check passes")
	cmd.Flags().BoolVar(&batch.outdated, "outdated", false, "update every named port, or with --mine yours, that has a newer release")
	cmd.Flags().BoolVar(&batch.mine, "mine", false, "with --outdated, the ports whose maintainers line names you (config maintainer)")
	cmd.Flags().BoolVar(&batch.check, "check", false, "with --outdated, also queue a check of each")
	cmd.Flags().BoolVarP(&batch.yes, "yes", "y", false, "with --outdated, start without asking; with --submit, apply a tidy of dockhand's own edits without asking")
	return cmd
}

func checksumsCommand(s *settings, streams Streams) *cobra.Command {
	var where branchChoice
	var plan, keepOld, keepRevision bool
	cmd := &cobra.Command{
		Use:   "checksums <port>",
		Short: "Refresh a port's checksums for the version it names",
		Long: `Fetches the port's distfiles for the version its Portfile names and writes
their checksums, in the branch's working files: what to run after editing
the version by hand. Nothing is committed.

A distfile that changed upstream under the same name, in a Portfile the
branch has not changed, is a stealth update: it says so, shows the checksums
before and after, bumps the revision, since the source changed, and sets
dist_subdir ${name}/${version}_${revision}, so mirrors keep both archives.
--no-revbump leaves the revision, for a change that needs no rebuild, and
numbers the directory instead: ${name}/${version}_1, the MacPorts guide's
recipe. A later version update removes either.

The branch is --branch, else the one checked out here; --new starts one.
--plan shows the edit and changes nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			request := engine.UpdateRequest{Action: model.EditChecksums, Port: args[0], KeepOldChecksums: keepOld, KeepRevision: keepRevision, Plan: plan}
			_, _, err := author(cmd.Context(), s, streams, where, "checksums", request, linkedOptions{})
			return err
		},
	}
	where.flags(cmd)
	cmd.Flags().BoolVar(&plan, "plan", false, "show the edit and change nothing")
	cmd.Flags().BoolVar(&keepOld, "keep-old-checksums", false, "refresh legacy md5 or sha1 checksums in place rather than rewriting them as rmd160, sha256, and size")
	cmd.Flags().BoolVar(&keepRevision, "no-revbump", false, "for a stealth update, leave the revision as it is")
	return cmd
}

// versionUpdate is what update and bump share: how the edit is made, and
// what going on to the pull request takes.
type versionUpdate struct {
	keepOld, shared, obsolete bool
	linked                    linkedOptions
}

// flags adds the flags update and bump share. goesOn is what the
// submission's flags go with, such as "with --submit, "; bump's always go
// on.
func (v *versionUpdate) flags(cmd *cobra.Command, goesOn string) {
	cmd.Flags().StringArrayVar(&v.linked.on, "on", nil, goesOn+"where to check (default check.on)")
	cmd.Flags().BoolVar(&v.linked.testedBinaries, "tested-binaries", false, goesOn+"state that you tested the basic functionality of all binary files")
	cmd.Flags().BoolVar(&v.linked.testedVariants, "tested-variants", false, goesOn+"state that you checked the most important variants")
	cmd.Flags().BoolVar(&v.keepOld, "keep-old-checksums", false, "refresh legacy md5 or sha1 checksums in place rather than rewriting them as rmd160, sha256, and size")
	cmd.Flags().BoolVar(&v.shared, "shared-release", false, "move every subport that shares the port's release")
	cmd.Flags().BoolVar(&v.obsolete, "with-obsolete", false, "also move the Portfile's obsolete stub for the port, one replaced_by it, to the same version")
	cmd.Flags().BoolVar(&v.linked.revbump, "revbump-dependents", false, "also bump the revision of the ports that link it directly")
	cmd.Flags().StringSliceVar(&v.linked.except, "except", nil, "leave this dependent out of --revbump-dependents")
}

// request is the update of the port args name, to the version they name
// or the newest.
func (v versionUpdate) request(args []string, plan bool) (engine.UpdateRequest, error) {
	if len(v.linked.except) > 0 && !v.linked.revbump {
		return engine.UpdateRequest{}, errors.New("--except takes a port out of --revbump-dependents; add --revbump-dependents")
	}
	request := engine.UpdateRequest{Action: model.EditUpdate, Port: args[0], KeepOldChecksums: v.keepOld, SharedRelease: v.shared, WithObsolete: v.obsolete, Plan: plan, CompareUpstream: true, LookForOthers: true}
	if len(args) == 2 {
		request.Version = args[1]
	}
	return request, nil
}

// run makes the update and, when it goes on to the pull request, tidies,
// checks, and submits it: update's path, and bump's. With --json, the
// result is the update's, with each later step's inside it.
func (v versionUpdate) run(ctx context.Context, s *settings, streams Streams, where branchChoice, request engine.UpdateRequest) error {
	linked := v.linked
	if linked.submit {
		if err := submitReady(ctx, s, request.Port, linked); err != nil {
			return err
		}
		streams.linkSteps(&updateJSON{})
	}
	// bump's other open pull requests are looked for before the edit, since
	// one would hold its submission after the check.
	request.Unattended = linked.unattended
	branch, update, err := author(ctx, s, streams, where, "update", request, linked)
	if err != nil || !linked.submit || !update.Applied {
		return err
	}
	return tidyAndSubmit(ctx, s, streams, branch, linked)
}

// submitReady settles, before the edit, what would stop an update going on
// to its pull request, so stopping there changes nothing: where to check,
// and for bump, which starts no second branch for a port, an open branch
// already changing it.
func submitReady(ctx context.Context, s *settings, port string, linked linkedOptions) error {
	e, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	if linked.unattended {
		changing, err := e.BranchesChanging(ctx, port)
		if err != nil {
			return err
		}
		if len(changing) > 0 {
			var names []string
			for _, branch := range changing {
				names = append(names, branch.ShortName())
			}
			return fmt.Errorf("%s is already changed in %s, so nothing was changed; dockhand status %s says what it needs", port, strings.Join(names, ", "), names[0])
		}
	}
	if _, err := e.Environments(ctx, firstNonEmpty(linked.on, s.file.Check.On)); err != nil {
		return fmt.Errorf("%w; nothing was changed", err)
	}
	return nil
}

// tidyAndSubmit is the rest of update --submit: tidy the branch, then
// submit --check, each previewed as its own command previews it.
func tidyAndSubmit(ctx context.Context, s *settings, streams Streams, branch model.Branch, linked linkedOptions) error {
	e, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer e.Close()
	out := streams.Out
	proposal, err := e.PlanTidy(ctx, engine.TidyRequest{Branch: branch})
	if err != nil {
		return err
	}
	if !proposal.Keep {
		fmt.Fprintf(out, "\n%s · tidying %s\n", branch.ShortName(), describeWork(proposal))
		streams.emit(tidyView(proposal))
		writeTidyPlan(out, proposal)
		applied, err := decideTidy(ctx, e, streams, proposal, false, linked.yes, "")
		if err != nil {
			return fmt.Errorf("%w; nothing was checked or submitted", err)
		}
		if !applied {
			fmt.Fprintln(out, "Nothing was checked or submitted.")
			return nil
		}
	}
	if branch, err = e.Resolve(ctx, branch.ShortName()); err != nil {
		return err
	}
	fmt.Fprintln(out)
	request := engine.SubmitRequest{Branch: branch, TestedBinaries: linked.testedBinaries, TestedVariants: linked.testedVariants}
	return submitChecked(ctx, s, e, streams, request, linked.on, linked.unattended)
}

// linkedOptions are update's --revbump-dependents and --except, and
// --submit, which goes on from the edit, with what it passes on: submit
// --check's --on, --tested-binaries, and --tested-variants, and --yes, which
// applies an unambiguous tidy without asking. unattended is bump's: nobody
// looks before the submission, so it holds what serve's would.
type linkedOptions struct {
	revbump                        bool
	except                         []string
	submit                         bool
	on                             []string
	testedBinaries, testedVariants bool
	yes                            bool
	unattended                     bool
}

// author finds the branch, makes the edit, and reports it.
func author(ctx context.Context, s *settings, streams Streams, where branchChoice, purpose string, request engine.UpdateRequest, linked linkedOptions) (branch model.Branch, update engine.Update, err error) {
	// A version update's plan with --new is a look before starting a
	// branch, against master as fetched now; anything else needs one.
	fromMaster := where.new && request.Plan
	if fromMaster && request.Action != model.EditUpdate {
		return model.Branch{}, engine.Update{}, fmt.Errorf("--plan changes nothing, so it starts no branch; plan in an existing one with --branch, or drop --plan")
	}
	e, err := s.open(ctx)
	if err != nil {
		return branch, update, err
	}
	defer e.Close()
	// So is one with no branch to plan in, since a plan starts nothing.
	var beside string
	if request.Plan && request.Action == model.EditUpdate && !fromMaster {
		if fromMaster, beside, err = unbranchedPlan(ctx, e, where, request.Port); err != nil {
			return branch, update, err
		}
	}
	var started bool
	out := streams.Out
	// A version update with --new starts its branch once there is an edit
	// to make, so a port already current starts nothing.
	deferred := where.new && !request.Plan && request.Action == model.EditUpdate
	switch {
	case deferred:
		name, err := e.FreeName(ctx, request.Port)
		if err != nil {
			return branch, update, err
		}
		request.Start = &engine.StartRequest{Name: name}
	case !fromMaster:
		// A plan asks nothing that would start a branch.
		choosing := streams
		if request.Plan {
			choosing = streams.unattended()
		}
		if branch, started, err = chooseBranch(ctx, e, choosing, where, request.Port, purpose); err != nil {
			return branch, update, err
		}
		announce(out, branch, started)
	}
	request.Branch, request.FromMaster = branch, fromMaster
	update, err = e.Update(ctx, request)
	if update.Started {
		branch, started = update.Branch, true
		announce(out, branch, started)
	}
	if err != nil && started {
		// The branch it started stays, for the edit by hand.
		streams.emit(updateView(branch, started, update, request.Plan))
	}
	if fromMaster && err == nil {
		why := ""
		if beside != "" {
			why = fmt.Sprintf(", since %s doesn't change %s", beside, request.Port)
		}
		fmt.Fprintf(out, "Planned on master %s (fetched just now)%s; --new without --plan starts the branch\n", engine.Short(update.Base), why)
		// The dependents are the index's at the master planned on.
		branch.Base = update.Base
	}
	// One whose evaluation isn't the change intended is refused as one it
	// can't make: rust's came bare, with no way on (the rust and cargo run).
	if errors.Is(err, engine.ErrUnsupported) || errors.Is(err, engine.ErrFidelity) {
		return branch, update, byHand(err, request, branch, started)
	}
	if uncertain := new(engine.UncertainRelease); errors.As(err, &uncertain) {
		return branch, update, uncertainUpdate(request.Port, uncertain.SetAside, linked, branch, started)
	}
	if held := new(engine.HeldBeforeEdit); errors.As(err, &held) {
		return branch, update, heldBeforeEdit(request, held.Held)
	}
	if err != nil {
		if started {
			return branch, update, fmt.Errorf("%w\nKept: %s, with nothing changed", err, branch.Name)
		}
		return branch, update, err
	}
	result := updateView(branch, started, update, request.Plan)
	streams.emit(result)
	if update.Current {
		switch {
		case deferred:
			fmt.Fprintf(out, "%s is already at %s; nothing to change, so no branch was started.\n", update.Port, update.After)
		case request.Action == model.EditUpdate:
			fmt.Fprintf(out, "%s is already at %s; nothing to change.\n", update.Port, update.After)
		default:
			fmt.Fprintf(out, "%s %s's checksums are current; nothing to change.\n", update.Port, update.After)
		}
		writePlainHTTP(out, update.PlainHTTP)
		return branch, update, nil
	}
	if request.Action == model.EditUpdate {
		fmt.Fprintf(out, "%s: %s → %s%s\n", update.Port, update.Before, update.After, releaseLabel(update.Release))
		// A plain bump read the same for semgrep's 0.14.0 to 1.179.0
		// (field testing, 2026-10-02).
		if update.CrossesMajor {
			fmt.Fprintf(out, "A new major version: what depends on %s may need to follow.\n", update.Port)
		}
		switch stub := update.Obsolete; {
		case stub == nil:
		case stub.Moved:
			fmt.Fprintf(out, "Its obsolete %s moves to %s too, in the same commit.\n", stub.Port, update.After.Version)
		case stub.Version != update.After.Version:
			fmt.Fprintf(out, "%s (obsolete, replaced_by %s) stays at %s; --with-obsolete moves it to %s.\n", stub.Port, update.Port, stub.Version, update.After.Version)
		}
		if update.Renamed != "" {
			fmt.Fprintf(out, "Upstream moved: GitHub answers %s as %s, by a redirect; the Portfile's github.setup may follow.\n", update.Release.Repository, update.Renamed)
		}
	} else if update.Stealth != nil {
		fmt.Fprintf(out, "%s %s · the distfile changed upstream without a new name (stealth update)\n", update.Port, update.Before)
		writeStealth(out, update.Stealth)
	} else {
		fmt.Fprintf(out, "%s %s\n", update.Port, update.After)
	}
	if request.Plan {
		fmt.Fprintf(out, "Plan, nothing changed:\n\n%s", update.Diff)
		if !strings.HasSuffix(update.Diff, "\n") {
			fmt.Fprintln(out)
		}
		if update.DistSubdirRemoved {
			fmt.Fprintln(out, "Removes "+distSubdirRemoved)
		}
		// What a reviewer would ask about is part of the look before.
		writeUpstream(out, update.Upstream)
		writePatches(out, update)
		writeOthers(out, update)
		writePlainHTTP(out, update.PlainHTTP)
		if linked.revbump {
			result.Revbumped, err = revbumpLinked(ctx, e, out, branch, update, linked.except, true)
			streams.emit(result)
			return branch, update, err
		}
		return branch, update, nil
	}
	what := "Updated version and checksums"
	switch {
	case request.Action == model.EditChecksums:
		what = "Updated checksums"
	case update.Distfiles == 0:
		// A port cloned with Git, or one that fetches nothing, has no
		// checksums to update (the semgrep run's finding 5).
		what = "Updated version"
	}
	if update.Distfiles > 0 {
		what += fmt.Sprintf(" (%s)", plural(update.Distfiles, "distfile"))
	}
	// What the dependency blocks hold, which the diff shows line by line:
	// hk's said "1 distfile" of 280 lines of crates (the gh, usql, hk, and
	// pgdog run's finding 4).
	for _, block := range update.Regenerated {
		// A block with nothing in it, before or after, says nothing:
		// "and 0 Git crates (0 changed)" (the txt run's finding 6).
		if block.Count == 0 && block.Changed == 0 {
			continue
		}
		entry := map[string]string{"go.vendors": "Go module", "cargo.crates": "crate", "cargo.crates_github": "Git crate"}[block.Option]
		if entry == "" {
			entry = block.Option + " entry"
		}
		what += fmt.Sprintf(" and %s (%d changed)", plural(block.Count, entry), block.Changed)
	}
	if update.Before.Revision != 0 && update.After.Revision == 0 {
		what += "; revision reset to 0"
	}
	if stealth := update.Stealth; stealth != nil {
		var also []string
		if stealth.Revbumped {
			also = append(also, fmt.Sprintf("revision %d → %d", update.Before.Revision, update.After.Revision))
		}
		if stealth.DistSubdir != "" {
			also = append(also, "dist_subdir "+stealth.DistSubdir+", so mirrors keep both archives")
		}
		switch len(also) {
		case 1:
			what += " and " + also[0]
		case 2:
			what += ", " + also[0] + ", and " + also[1]
		}
	}
	fmt.Fprintf(out, "%s.\nChanged: %s\n", what, strings.Join(update.Files, ", "))
	if stealth := update.Stealth; stealth != nil {
		if stealth.RevbumpProblem != "" {
			fmt.Fprintf(out, "! the revision was not bumped: %s. Bump it yourself if the change needs a rebuild.\n", stealth.RevbumpProblem)
		}
		if stealth.Problem != "" {
			fmt.Fprintf(out, "! dist_subdir was not set: %s. Set it yourself, so mirrors keep both archives: dist_subdir ${name}/${version}_${revision} with a revision bump, else ${name}/${version}_1\n", stealth.Problem)
		}
		if stealth.Revbumped {
			fmt.Fprintln(out, "The source changed, so the revision is bumped; --no-revbump leaves it, for a change that needs no rebuild.")
		} else if stealth.RevbumpProblem == "" {
			fmt.Fprintln(out, "Inspect the source change before deciding whether it needs a revision bump.")
		}
		fmt.Fprintln(out, "dockhand diff --archive shows what changed inside the archive.")
	}
	if update.DistSubdirRemoved {
		fmt.Fprintln(out, "Removed "+distSubdirRemoved)
	}
	writeUpstream(out, update.Upstream)
	writePatches(out, update)
	writeOthers(out, update)
	writePlainHTTP(out, update.PlainHTTP)
	if linked.revbump {
		if result.Revbumped, err = revbumpLinked(ctx, e, out, branch, update, linked.except, false); err != nil {
			return branch, update, err
		}
		streams.emit(result)
	}
	if !linked.submit {
		fmt.Fprintln(out, "Next: "+nextAfterEdit(ctx, e, branch))
	}
	return branch, update, nil
}

// distSubdirRemoved is why a version update removes a stealth update's
// dist_subdir, which the update says, and its plan before it (the hugo
// exercise's yq run, finding 2).
const distSubdirRemoved = "dist_subdir: a stealth update set it for the old version, and every archive of the new version has a name of its own."

// nextAfterEdit is what follows an edit (Design v3 §6.2): a check of the
// working files, whose own Next is the tidy that commits them. A branch
// other than the one checked out here is gone to first, since a check
// from elsewhere takes the branch's committed head.
func nextAfterEdit(ctx context.Context, e *engine.Engine, branch model.Branch) string {
	if current, err := e.Current(ctx); err == nil && current.ID == branch.ID {
		return "dockhand check"
	}
	return fmt.Sprintf(`cd "$(dockhand path %s)", then dockhand check`, branch.ShortName())
}

// announce says which branch an edit is in, and whether it was just
// started.
func announce(out io.Writer, branch model.Branch, started bool) {
	if started {
		fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", branch.Name, engine.Short(branch.Base))
	}
	fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), tilde(branch.Worktree))
}

// byHand says that dockhand can't make the edit by itself, why, and how
// to make it by hand (Design v3 §6.3), with advice only where it can work:
// checksums dockhand can't find in the Portfile to edit, it can't refresh
// either, and prints instead. A plan changes nothing, so it keeps nothing.
func byHand(err error, request engine.UpdateRequest, branch model.Branch, started bool) error {
	reason := strings.TrimPrefix(strings.TrimPrefix(err.Error(), engine.ErrUnsupported.Error()+": "), engine.ErrFidelity.Error()+": ")
	var unlocated *engine.Unlocated
	if errors.As(err, &unlocated) {
		reason = unlocated.Error()
	}
	kept := ""
	switch {
	case request.Plan:
	case started:
		kept = "\nKept: " + branch.Name + ", with nothing changed."
	default:
		kept = "\nKept: the branch, unchanged."
	}
	port := request.Port
	var toWrite *engine.ChecksumsToWrite
	switch {
	case request.Action == model.EditChecksums && errors.As(err, &toWrite):
		block := strings.ReplaceAll(portfile.ChecksumsBlock(toWrite.Checksums), "\n", "\n    ")
		return fmt.Errorf("can't refresh %s's checksums by itself: %s%s\nWrite them yourself; its archives have these now:\n    %s\n  dockhand edit %s", port, reason, kept, block, port)
	case request.Action == model.EditChecksums:
		return fmt.Errorf("can't refresh %s's checksums by itself: %s%s\nWrite them yourself, as port checksum %s reports them:\n  dockhand edit %s", port, reason, kept, port, port)
	case unlocated != nil:
		return fmt.Errorf("can't update %s by itself: %s%s\nEdit the version yourself; dockhand checksums %s then prints the checksums to write:\n  dockhand edit %s", port, reason, kept, port, port)
	}
	return fmt.Errorf("can't update %s by itself: %s%s\nEdit the version yourself; dockhand checksums %s then fills in the rest:\n  dockhand edit %s", port, reason, kept, port, port)
}

// uncertainUpdate is an update to the newest release that found none to
// choose: what compares newest was set aside, since its tag's commit is
// older than the port's own, and nothing newer is beyond it. Whether it's
// a release is a person's call, so it needs attention, and names the
// update that takes it.
func uncertainUpdate(port string, aside []engine.SetAside, linked linkedOptions, branch model.Branch, started bool) error {
	verb := "update"
	if linked.unattended {
		verb = "bump"
	}
	kept := ""
	if started {
		kept = "\nKept: " + branch.Name + ", with nothing changed."
	}
	return exitf(3, "can't tell whether %s is current, so nothing was changed: %s\nIf %s is a release: dockhand %s %s %s%s", port, setAsideWords(aside), aside[0].Tag, verb, port, aside[0].Source, kept)
}

// heldBeforeEdit is bump held before its edit for what would hold its
// submission after the check, said as a held submission is. After a look,
// update --submit goes ahead, since a person watches it.
func heldBeforeEdit(request engine.UpdateRequest, held []string) error {
	next := "dockhand update " + request.Port
	if request.Version != "" {
		next += " " + request.Version
	}
	return exitf(3, "%s waits for your look, so nothing was changed: %s\nOnce it's fine: %s --new --submit", request.Port, strings.Join(held, "; "), next)
}

// writeStealth shows each changed archive's checksums, before and after.
func writeStealth(out io.Writer, stealth *engine.Stealth) {
	for _, distfile := range stealth.Distfiles {
		if len(stealth.Distfiles) > 1 {
			fmt.Fprintf(out, "  %s\n", distfile.Name)
		}
		fmt.Fprintf(out, "  was   %s\n  now   %s\n", checksumWords(distfile.Was), checksumWords(distfile.Now))
	}
}

// checksumWords is a checksum as a person compares it: sha256, shortened,
// and the size, else rmd160.
func checksumWords(sum portfile.Checksum) string {
	short := func(hash string) string {
		if len(hash) <= 10 {
			return hash
		}
		return hash[:4] + "…" + hash[len(hash)-4:]
	}
	var words []string
	switch {
	case sum.SHA256 != "":
		words = append(words, "sha256 "+short(sum.SHA256))
	case sum.RMD160 != "":
		words = append(words, "rmd160 "+short(sum.RMD160))
	}
	if sum.Size != 0 {
		words = append(words, "size "+thousands(sum.Size))
	}
	return strings.Join(words, "   ")
}

// thousands writes a number with commas: 7,114,508.
func thousands(n int64) string {
	digits := strconv.FormatInt(n, 10)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}

// revbumpLinked has the engine bump the revision of the ports that link
// an updated one directly, or with plan list them, and says what it did.
func revbumpLinked(ctx context.Context, e *engine.Engine, out io.Writer, branch model.Branch, update engine.Update, except []string, plan bool) ([]string, error) {
	done, err := e.RevbumpLinked(ctx, branch, update, except, plan)
	if err != nil && len(done.Bump)+len(done.Changed)+len(done.Excepted) == 0 {
		return nil, err
	}
	// Those that link it only under a variant, which the index doesn't
	// record, are listed apart, and bumped as the rest are.
	var names, indexed, under []string
	for _, dependent := range done.Bump {
		names = append(names, dependent.Name)
		if len(dependent.Variants) > 0 {
			under = append(under, fmt.Sprintf("%s (+%s)", dependent.Name, strings.Join(dependent.Variants, " or +")))
		} else {
			indexed = append(indexed, dependent.Name)
		}
	}
	fmt.Fprintf(out, "Direct library dependents, from the index at %s:\n  %s\n", engine.Short(done.Base), orNone(strings.Join(indexed, "  ")))
	if len(under) > 0 {
		fmt.Fprintf(out, "  · under a variant, found in the Portfile, which the index doesn't record: %s\n", strings.Join(under, ", "))
	}
	for _, dependent := range done.Changed {
		fmt.Fprintf(out, "  · %s: the branch already changes it, so it is left as it is\n", dependent.Name)
	}
	if len(done.Excepted) > 0 {
		fmt.Fprintf(out, "  · left out with --except: %s\n", strings.Join(done.Excepted, ", "))
	}
	if err != nil {
		return nil, err
	}
	if plan || len(done.Bump) == 0 {
		return nonNil(names), nil
	}
	fmt.Fprintf(out, "Revision bumped %s; subject \"<port>: %s\" recorded for tidy.\n", plural(len(done.Bumped), "port"), done.Subject)
	return names, nil
}

func releaseLabel(release *model.Release) string {
	switch {
	case release == nil:
		return ""
	case release.Tag != "":
		forge := release.Forge
		switch forge {
		case "github":
			forge = "GitHub"
		case "gitlab":
			forge = "GitLab"
		}
		return fmt.Sprintf("   (%s tag %s)", strings.TrimSpace(forge), release.Tag)
	case release.Archive:
		return "   (from its distfiles)"
	}
	return ""
}

// releaseWords says where an update's version came from, for status: "jq
// 1.8.1, GitHub tag jq-1.8.1 of jqlang/jq at 1a2b3c4".
func releaseWords(port string, release model.Release) string {
	words := port + " " + release.Version
	switch {
	case release.Tag != "":
		words += ", " + strings.TrimSuffix(strings.TrimPrefix(releaseLabel(&release), "   ("), ")")
		if release.Repository != "" {
			words += " of " + release.Repository
		}
		if release.Commit != "" {
			words += " at " + engine.Short(model.ObjectID(release.Commit))
		}
	case release.Archive:
		words += ", from its distfiles"
	}
	return words
}

// unbranchedPlan reports whether a version update's plan has no branch to
// be planned in: none named, nothing tracked checked out here, and no open
// branch changing the port. It is planned on master, as --new --plan is,
// and beside is the untracked branch checked out here, if one is. What's
// checked out here and changes the port is refused instead, since a plan
// on master would leave it out (D7).
func unbranchedPlan(ctx context.Context, e *engine.Engine, where branchChoice, port string) (fromMaster bool, beside string, err error) {
	if where.branch != "" {
		return false, "", nil
	}
	if _, err := e.Current(ctx); !errors.Is(err, engine.ErrNoBranch) {
		return false, "", nil
	}
	beside = untrackedHere(ctx, e)
	changes, err := e.ChangesHere(ctx, port)
	switch {
	case err != nil:
		return false, "", err
	case changes && beside != "":
		return false, "", fmt.Errorf("%w: %s changes %s, which a plan on master would leave out; dockhand adopt tracks it, so the plan reads its changes, or --new --plan plans on master without them", engine.ErrNoBranch, beside, port)
	case changes:
		return false, "", fmt.Errorf("what's checked out here changes %s, which a plan on master would leave out; --new --plan plans on master without it", port)
	}
	changing, err := e.BranchesChanging(ctx, port)
	return len(changing) == 0, beside, err
}

// untrackedHere is the branch checked out here when it's one a person made
// and dockhand doesn't track; empty for master, or no branch at all.
func untrackedHere(ctx context.Context, e *engine.Engine) string {
	if current, err := e.Repo.CurrentBranch(ctx); err == nil && current != "master" && current != "main" {
		return current
	}
	return ""
}

// chooseBranch is the branch an authoring command works in: --branch, a
// new one with --new, or the one checked out here. With none of those, a
// terminal is asked, and a script is told the choices.
func chooseBranch(ctx context.Context, e *engine.Engine, streams Streams, where branchChoice, port, purpose string) (model.Branch, bool, error) {
	if where.branch != "" {
		branch, err := e.Resolve(ctx, where.branch)
		return branch, false, err
	}
	if where.new {
		return startFor(ctx, e, port)
	}
	branch, err := e.Current(ctx)
	if err == nil || !errors.Is(err, engine.ErrNoBranch) {
		return branch, false, err
	}
	// A branch someone made and dockhand does not track is theirs to
	// adopt where it changes the port; otherwise it's no context, as
	// master isn't, or no branch at all (D7).
	beside := untrackedHere(ctx, e)
	if beside != "" {
		changes, err := e.ChangesHere(ctx, port)
		if err != nil {
			return model.Branch{}, false, err
		}
		if changes {
			return model.Branch{}, false, fmt.Errorf("%w: %s changes %s and isn't tracked; dockhand adopt tracks it, so the %s is made there, or --new starts a branch from master without its changes", engine.ErrNoBranch, beside, port, purpose)
		}
	}

	changing, err := e.BranchesChanging(ctx, port)
	if err != nil {
		return model.Branch{}, false, err
	}
	var names []string
	for _, branch := range changing {
		names = append(names, branch.ShortName())
	}
	if !streams.terminal() {
		if len(names) == 0 {
			here := "this checkout is on none"
			if beside != "" {
				here = beside + ", checked out here, doesn't change it"
			}
			return model.Branch{}, false, fmt.Errorf("%s is in no open branch, and %s; start one with --new, or name one with --branch <name>", port, here)
		}
		return model.Branch{}, false, fmt.Errorf("%s is changed in %s; name it with --branch %s, or start another with --new", port, strings.Join(names, ", "), names[0])
	}
	name, err := e.FreeName(ctx, port)
	if err != nil {
		return model.Branch{}, false, err
	}
	switch len(changing) {
	case 0:
		fmt.Fprintf(streams.Err, "%s is in no open branch.\n", port)
		answer, err := ask(streams, fmt.Sprintf("? start %s for it? [Y/n] ", engine.BranchName(name)))
		if err != nil {
			return model.Branch{}, false, err
		}
		if answer != "" && !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			return model.Branch{}, false, errors.New("nothing changed")
		}
		return startNamed(ctx, e, name)
	case 1:
		fmt.Fprintf(streams.Err, "%s is changed in 1 open branch: %s\n", port, changing[0].Name)
		answer, err := ask(streams, fmt.Sprintf("? %s %s there, or start a new branch? [t]here / [n]ew / [q]uit ", verb(purpose), port))
		if err != nil {
			return model.Branch{}, false, err
		}
		switch strings.ToLower(answer) {
		case "t", "there":
			return changing[0], false, nil
		case "n", "new":
			return startNamed(ctx, e, name)
		}
		return model.Branch{}, false, errors.New("nothing changed")
	}
	fmt.Fprintf(streams.Err, "%s is changed in %d open branches: %s\n", port, len(changing), strings.Join(names, ", "))
	answer, err := ask(streams, fmt.Sprintf("? start %s instead? (--branch <name> picks one of them) [y/N] ", engine.BranchName(name)))
	if err != nil {
		return model.Branch{}, false, err
	}
	if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
		return model.Branch{}, false, errors.New("nothing changed")
	}
	return startNamed(ctx, e, name)
}

func verb(purpose string) string {
	if purpose == "checksums" {
		return "refresh"
	}
	return purpose
}

func startFor(ctx context.Context, e *engine.Engine, port string) (model.Branch, bool, error) {
	name, err := e.FreeName(ctx, port)
	if err != nil {
		return model.Branch{}, false, err
	}
	return startNamed(ctx, e, name)
}

func startNamed(ctx context.Context, e *engine.Engine, name string) (model.Branch, bool, error) {
	branch, err := e.Start(ctx, engine.StartRequest{Name: name})
	return branch, err == nil, err
}

// writePatches reports the patches an update found apply, those that no
// longer do, and those it couldn't check: that they apply is said too, so
// a check that found nothing isn't taken for none (the fluent-bit run).
func writePatches(out io.Writer, update engine.Update) {
	if update.PatchesApplied > 0 {
		source := "the new source"
		if update.Release != nil && update.Release.Version != "" {
			source = update.Release.Version + "'s source"
		}
		fmt.Fprintf(out, "· %s to %s\n", appliesWords(update.PatchesApplied, len(update.PatchProblems)), source)
	}
	for _, problem := range update.PatchProblems {
		fmt.Fprintf(out, "! patch %s\n", problem)
	}
	for _, unchecked := range update.PatchesUnchecked {
		fmt.Fprintf(out, "· patch %s\n", unchecked)
	}
}

// appliesWords says how many patches apply, "6 patches apply", or of how
// many, where others don't.
func appliesWords(applied, rejected int) string {
	switch {
	case rejected > 0:
		return fmt.Sprintf("%d of %d patches apply", applied, applied+rejected)
	case applied == 1:
		return "1 patch applies"
	}
	return fmt.Sprintf("%d patches apply", applied)
}

// writeUpstream reports what comparing the upstream archives found.
func writeUpstream(out io.Writer, comparison *model.UpstreamComparison) {
	switch {
	case comparison == nil:
	case comparison.Problem != "":
		// What couldn't be checked holds as a finding does (D4), so it is
		// marked as one.
		fmt.Fprintf(out, "! Upstream archives not compared: %s\n", comparison.Problem)
		fmt.Fprintln(out, holdLegend)
	case engine.NothingCompared(*comparison):
		fmt.Fprintf(out, "Upstream not compared: %s.\n", engine.CoverageWords(*comparison))
	case len(comparison.Changes) == 0:
		fmt.Fprintln(out, "Upstream source compared: no license, build file, or dependency changes.")
		if words := engine.CoverageWords(*comparison); words != "" {
			fmt.Fprintf(out, "  · %s\n", words)
		}
	default:
		fmt.Fprintln(out, "Upstream changes:")
		for _, change := range comparison.Changes {
			fmt.Fprintf(out, "  %s\n", upstreamWords(underUpstream(change)))
		}
		if words := engine.CoverageWords(*comparison); words != "" {
			fmt.Fprintf(out, "  · %s\n", words)
		}
		if comparison.Held() {
			fmt.Fprintln(out, "  "+holdLegend)
		}
	}
}

// writeOthers names the port's other open pull requests the update found,
// or why it couldn't look. They stop nothing: submit shows them again, and
// bump and serve hold on one.
// writePlainHTTP says the port's plain-HTTP URLs, which MacPorts would
// have over HTTPS, with whether the https form answers. The Portfile is
// left as it is: changing them is the maintainer's call.
func writePlainHTTP(out io.Writer, plain []engine.PlainURL) {
	if len(plain) == 0 {
		return
	}
	fmt.Fprintln(out, "MacPorts prefers HTTPS; over plain HTTP:")
	for _, url := range plain {
		answer := url.HTTPS + " answers"
		if !url.Answers {
			answer = "https doesn't answer there"
		}
		fmt.Fprintf(out, "  %s %s: %s\n", url.Option, url.URL, answer)
	}
}

func writeOthers(out io.Writer, update engine.Update) {
	if update.OthersProblem != "" {
		fmt.Fprintf(out, "Couldn't look for other open pull requests for %s: %s\n", update.Port, update.OthersProblem)
		return
	}
	for _, pr := range update.Others {
		fmt.Fprintf(out, "Also open for %s: #%d %s\n", update.Port, pr.Number, pr.Title)
	}
}

// holdLegend says what the marks of an upstream finding mean, beside any
// list with one that holds (the chezmoi run).
const holdLegend = "(! holds bump's and serve's submission for your look; · holds nothing)"

// underUpstream is a change as words under an Upstream heading, which
// already says whose it is: "upstream's LICENSE changed" is "LICENSE
// changed" there. Elsewhere, as in what holds a submission, the words
// stand alone.
func underUpstream(change model.UpstreamChange) model.UpstreamChange {
	for _, prefix := range []string{"upstream's ", "upstream: "} {
		if rest, ok := strings.CutPrefix(change.Message, prefix); ok {
			change.Message = rest
			break
		}
	}
	return change
}

func upstreamWords(change model.UpstreamChange) string {
	if change.Hold {
		return "! " + change.Message
	}
	return "· " + change.Message
}
