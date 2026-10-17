package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
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
	// new is --new's name, or newNamed where it named none; empty where
	// --new wasn't given.
	new string
}

// newNamed is --new given without a name: the branch is named for the
// port, or the version an update moves to.
const newNamed = "\x00"

// isNew is whether --new was given; newName is the name it gave, if any.
func (c branchChoice) isNew() bool { return c.new != "" }
func (c branchChoice) newName() string {
	if c.new == newNamed {
		return ""
	}
	return c.new
}

func (c *branchChoice) flags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&c.branch, "branch", "b", "", "work in this existing branch, by its exact name (the dockhand/ prefix is optional); --new=<name> starts one")
	cmd.Flags().StringVar(&c.new, "new", "", "start a new branch for this, in its own worktree; --new=<name> names it")
	cmd.Flags().Lookup("new").NoOptDefVal = newNamed
	cmd.MarkFlagsMutuallyExclusive("branch", "new")
}

func updateCommand(s *settings, streams Streams) *cobra.Command {
	var where branchChoice
	var plan, mine, yes bool
	var version versionUpdate
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

--mine updates every port you maintain that has a newer release, as
outdated --mine finds them: one branch each, from fresh master, nothing
committed. It shows the branches before starting any, and asks once; --yes
starts them without asking. dockhand ship --mine carries them on, to their
pull requests, and ship takes a port's update as far as --to says.`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mine {
				if len(args) > 0 || where.branch != "" || where.isNew() {
					return errors.New("--mine starts a branch for each of your ports; it takes no port, --branch, or --new")
				}
				return updateMine(cmd.Context(), s, streams, version, plan, yes)
			}
			if yes {
				return errors.New("--yes goes with --mine")
			}
			if len(args) == 0 {
				return errors.New("name the port to update, or update your outdated ports with --mine")
			}
			request, err := version.request(args, plan)
			if err != nil {
				return err
			}
			_, _, err = author(cmd.Context(), s, streams, where, "update", request, version.linked)
			return err
		},
	}
	where.flags(cmd)
	version.flags(cmd)
	cmd.Flags().BoolVar(&plan, "plan", false, "show the edit and change nothing")
	cmd.Flags().BoolVar(&mine, "mine", false, "update each of your ports with a newer release, in a branch of its own (config maintainer)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "with --mine, start the branches without asking")
	return cmd
}

// updateMine is update --mine: ship's edit stage over your outdated ports,
// each in a branch of its own, nothing committed (decision 7, 2026-10-13).
func updateMine(ctx context.Context, s *settings, streams Streams, version versionUpdate, plan, yes bool) error {
	whole := &batchJSON{}
	streams.linkSteps(whole)
	e, err := s.open(ctx)
	if err != nil {
		return err
	}
	outdated, err := outdatedBatch(ctx, s, e, streams)
	e.Close()
	if err != nil {
		return err
	}
	out := streams.Out
	if len(outdated.Updates) == 0 {
		fmt.Fprintln(out, "Nothing to update: no newer releases remain outside your existing branches.")
		return nil
	}
	fmt.Fprintf(out, "Will start %s, one per port:\n", prose.Plural(len(outdated.Updates), "branch"))
	rows := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, update := range outdated.Updates {
		row := fmt.Sprintf("  %s\t%s %s → %s", engine.BranchName(update.Name), update.Port.Port, update.Port.Current, update.Port.Newest)
		if update.CrossesMajor() {
			row += ", a new major version"
		}
		fmt.Fprintln(rows, row)
	}
	_ = rows.Flush()
	if plan {
		fmt.Fprintln(out, "Nothing was started (--plan).")
		return nil
	}
	ok, _, err := streams.decide(yes, decision{question: "? Start them? [y/N] ", undone: "nothing was started", byYes: "--yes starts what is shown"})
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "Nothing was started.")
		return nil
	}
	var failed []string
	for _, update := range outdated.Updates {
		whole.next()
		fmt.Fprintln(out)
		request, err := version.request([]string{update.Port.Port}, false)
		if err == nil {
			request.Version, request.Release = update.Port.Newest, update.Port.Release
			_, _, err = author(ctx, s, streams, branchChoice{new: update.Name}, "update", request, version.linked)
		}
		if err != nil {
			fmt.Fprintf(streams.Err, "%s: %v\n", update.Port.Port, err)
			failed = append(failed, update.Port.Port)
		}
	}
	return batchExit(failed, len(outdated.Updates), "updated")
}

// batchExit is a batch's exit (the command-line UX review's §10): 1 where
// none of total went through, 3 where some stopped and need a look.
func batchExit(failed []string, total int, done string) error {
	switch {
	case len(failed) > 0 && len(failed) == total:
		return &ExitError{Code: ExitFailure, Message: "nothing was " + done + ": " + strings.Join(failed, ", ") + " stopped"}
	case len(failed) > 0:
		return &ExitError{Code: ExitAttention, Message: strings.Join(failed, ", ") + " stopped; the rest went on"}
	}
	return nil
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

It reads the Portfile as it stands, on every platform it models, with no
version to find and none of update's holds: a context it can't read, or a
declaration none it reads fetches, is left as it was and said.

The branch is --branch, else the one checked out here; --new starts one.
--plan shows the edit and changes nothing, and is the check: it says
whether every distfile matches its checksums, starting no branch where
none is open, and exits 3 where any doesn't.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			request := engine.UpdateRequest{Action: model.EditChecksums, Port: args[0], EditIntent: model.EditIntent{KeepOldChecksums: keepOld}, KeepRevision: keepRevision, Plan: plan}
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
	keepOld, shared, obsolete, followMoves bool
	linked                                 linkedOptions
}

// flags adds the edit's flags, which update, ship, and bump share.
func (v *versionUpdate) flags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&v.keepOld, "keep-old-checksums", false, "refresh legacy md5 or sha1 checksums in place rather than rewriting them as rmd160, sha256, and size")
	cmd.Flags().BoolVar(&v.shared, "shared-release", false, "move every subport that shares the port's release")
	cmd.Flags().BoolVar(&v.followMoves, "follow-moves", false, "where GitHub answers the port's repository by another name, point github.setup at it")
	cmd.Flags().BoolVar(&v.obsolete, "with-obsolete", false, "also move the Portfile's obsolete stub for the port, one replaced_by it, to the same version")
	cmd.Flags().BoolVar(&v.linked.revbump, "revbump-dependents", false, "also bump the revision of the ports that link it directly")
	cmd.Flags().StringSliceVar(&v.linked.except, "except", nil, "leave this dependent out of --revbump-dependents")
}

// stageFlags adds the flags of the stages after the edit, defined once,
// which ship and bump share: where to check, and the pull request's.
func (v *versionUpdate) stageFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&v.linked.on, "on", nil, "where to check, explicit environments (default build.default's profile, or build.unattended's where nobody looks)")
	cmd.Flags().StringVar(&v.linked.build, "build", "", "where to check, by build profile (default build.default's, or build.unattended's where nobody looks)")
	cmd.Flags().StringVar(&v.linked.tests, "tests", "", "the check's tests: declared (advisory), required, or skip (default check.tests)")
	cmd.Flags().StringSliceVar(&v.linked.types, "type", nil, "the template's Type(s): bugfix, enhancement, security fix (default: enhancement for an update dockhand made)")
	cmd.Flags().BoolVar(&v.linked.draft, "draft", false, "open the pull request as a draft, even where its check fails")
	cmd.Flags().BoolVar(&v.linked.testedBinaries, "tested-binaries", false, "state that you tested the basic functionality of all binary files")
	cmd.Flags().BoolVar(&v.linked.testedVariants, "tested-variants", false, "state that you checked the most important variants")
	cmd.Flags().StringVar(&v.linked.title, "title", "", "the pull request's title (default: the commit subject)")
	cmd.Flags().StringVar(&v.linked.note, "note", "", "your own note in the pull request's description")
	cmd.Flags().BoolVar(&v.linked.skipNotification, "skip-notification", false, "add [skip notification], so maintainers are not mentioned")
	cmd.MarkFlagsMutuallyExclusive("build", "on")
}

// stageReady refuses what the later stages' flags can't take before
// anything is done: a misspelled --tests.
func (v versionUpdate) stageReady() error {
	if v.linked.tests != "" && !model.TestPolicy(v.linked.tests).Valid() {
		return fmt.Errorf("--tests %q is not declared, required, or skip", v.linked.tests)
	}
	return nil
}

// request is the update of the port args name, to the version they name
// or the newest.
func (v versionUpdate) request(args []string, plan bool) (engine.UpdateRequest, error) {
	if len(v.linked.except) > 0 && !v.linked.revbump {
		return engine.UpdateRequest{}, errors.New("--except takes a port out of --revbump-dependents; add --revbump-dependents")
	}
	request := engine.UpdateRequest{Action: model.EditUpdate, Port: args[0], EditIntent: model.EditIntent{KeepOldChecksums: v.keepOld, SharedRelease: v.shared}, WithObsolete: v.obsolete, FollowMoves: v.followMoves, Plan: plan, CompareUpstream: true, LookForOthers: true}
	if len(args) == 2 {
		request.Version = args[1]
	}
	return request, nil
}

// tidyBranch commits a branch's edits as tidy would plan them, previewed
// as tidy previews them, where it has any, and returns the branch as it is
// after; applied is false where the person declined, which undone says.
// yes applies an unambiguous plan without asking (decideTidy).
func tidyBranch(ctx context.Context, e *engine.Engine, streams Streams, branch model.Branch, undone string) (model.Branch, bool, error) {
	out := streams.Out
	proposal, err := e.PlanTidy(ctx, engine.TidyRequest{Branch: branch})
	if err != nil {
		return branch, false, err
	}
	if !proposal.Keep {
		fmt.Fprintf(out, "\n%s · tidying %s\n", branch.ShortName(), describeWork(proposal))
		streams.emit(tidyView(proposal))
		writeTidyPlan(out, proposal, false)
		// A plan of dockhand's own edits is applied unasked; one with the
		// person's is reviewed on a terminal, and held where nobody looks,
		// --yes or no terminal: --yes skips ship's question, and approves no
		// plan it hasn't shown (Codex's review of 832f059e, high 1). The
		// hold names tidy, as serve's does, where tidy's own refusal
		// offered --yes, which ship's --yes isn't.
		if !streams.terminal() {
			if err := proposal.HeldForLook(model.AnsweredNobody); err != nil {
				return branch, false, fmt.Errorf("%w; %s", err, undone)
			}
		}
		applied, err := decideTidy(ctx, e, streams, proposal, false, proposal.Unambiguous(), "")
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

// linkedOptions are the edit's --revbump-dependents and --except, and what
// ship and bump pass on to the stages after it: where to check, the tests,
// and the pull request's words. unattended is bump's and ship's with --yes
// or no terminal: nobody
// looks before the submission, so it holds what serve's would.
type linkedOptions struct {
	revbump bool
	except  []string
	// goesOn is ship's: the edit goes on to its commit and on, so no
	// next step is said after it.
	goesOn                         bool
	on                             []string
	build                          string
	testedBinaries, testedVariants bool
	// tests is the check's --tests; types and draft the submission's
	// --type and --draft (the second look's §6: bump lacked them).
	tests      string
	types      []string
	draft      bool
	unattended bool
	// title, note, and skipNotification are submit's, passed on; noteSet
	// is whether --note was given, "" taking a note out. bump took none, so
	// a test's pull request couldn't be marked one (the rc6 full stage, B2).
	title            string
	note             string
	noteSet          bool
	skipNotification bool
}

// author finds the branch, makes the edit, and reports it.
func author(ctx context.Context, s *settings, streams Streams, where branchChoice, purpose string, request engine.UpdateRequest, linked linkedOptions) (branch model.Branch, update engine.Update, err error) {
	// A version update's plan with --new is a look before starting a
	// branch, against master as fetched now; anything else needs one.
	fromMaster := where.isNew() && request.Plan
	if fromMaster && request.Action != model.EditUpdate {
		return model.Branch{}, engine.Update{}, fmt.Errorf("--plan changes nothing, so it starts no branch; plan in an existing one with --branch, or drop --plan")
	}
	e, err := s.open(ctx)
	if err != nil {
		return branch, update, err
	}
	defer e.Close()
	// So is one with no branch to plan in, since a plan starts nothing: a
	// checksum refresh's too, whose plan started a branch, "this starts
	// dockhand/jq-checksums for it", for a plan that changes nothing.
	var beside string
	if request.Plan && (request.Action == model.EditUpdate || request.Action == model.EditChecksums) && !fromMaster {
		if fromMaster, beside, err = unbranchedPlan(ctx, e, where, request.Port); err != nil {
			return branch, update, err
		}
	}
	var started bool
	out := streams.Out
	// A version update with --new starts its branch once there is an edit
	// to make, so a port already current starts nothing.
	// It's named for the version it moves to, which the update finds.
	deferred := where.isNew() && !request.Plan && request.Action == model.EditUpdate
	switch {
	case deferred:
		request.Start = &engine.StartRequest{Name: where.newName()}
	case !fromMaster:
		// A plan asks nothing that would start a branch.
		choosing := streams
		if request.Plan {
			choosing = streams.unattended()
		}
		branch, started, err = chooseBranch(ctx, e, choosing, where, request.Port, purpose)
		if errors.Is(err, errStartForVersion) && !request.Plan && request.Action == model.EditUpdate {
			deferred, err = true, nil
			request.Start = &engine.StartRequest{}
		}
		if err != nil {
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
	if update.RenamedFrom != "" {
		branch = update.Branch
		fmt.Fprintf(out, "Renamed %s to %s to match the update's target version.\n", update.RenamedFrom, branch.ShortName())
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
		fmt.Fprintf(out, "Planned on master %s (fetched just now)%s; --new without --plan starts the branch\n", update.Base.Short(), why)
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
		return branch, update, heldBeforeEdit(request, engine.HoldLines(held.Held))
	}
	if err != nil {
		if started {
			// An update that failed after its edit, as an obsolete stub's
			// following did, says the edit it kept: "nothing changed" was
			// said of a worktree holding terraform-1.16's (field testing
			// at a3c43ef7).
			if len(update.Files) > 0 {
				return branch, update, fmt.Errorf("%w\nKept branch %s with uncommitted changes to %s.", err, branch.Name, strings.Join(update.Files, ", "))
			}
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
			fmt.Fprintf(out, "%s %s: %s; nothing to change.\n", update.Port, update.After, matchWords(update.Compared))
		}
		writeLeft(out, update.ChecksumsLeft)
		writePlainHTTP(out, update.PlainHTTP)
		return branch, update, nil
	}
	if request.Action == model.EditUpdate {
		var source string
		if update.Release != nil && engine.ReleaseSource(*update.Release) != "" {
			source = "   (" + engine.ReleaseSource(*update.Release) + ")"
		}
		fmt.Fprintf(out, "%s: %s → %s%s\n", update.Port, update.Before, update.After, source)
		// A plain bump read the same for semgrep's 0.14.0 to 1.179.0
		// (field testing, 2026-10-02).
		if update.CrossesMajor {
			fmt.Fprintf(out, "New major version: ports that depend on %s may also need updates.\n", update.Port)
		}
		switch stub := update.Obsolete; {
		case stub == nil:
		case stub.Moved:
			fmt.Fprintf(out, "Also updating obsolete port %s to %s; tidy will include both updates in one commit.\n", stub.Port, update.After.Version)
		case stub.Version != update.After.Version:
			fmt.Fprintf(out, "%s (obsolete, replaced_by %s) stays at %s; --with-obsolete moves it to %s.\n", stub.Port, update.Port, stub.Version, update.After.Version)
		}
		// Where upstream was compared, the move is one of its findings.
		if update.Followed != "" {
			fmt.Fprintf(out, "Updated github.setup to %s, the current GitHub location of %s.\n", update.Followed, update.Release.Repository)
		} else if update.Renamed != "" && update.Upstream == nil {
			fmt.Fprintf(out, "Upstream moved: GitHub redirects %s to %s. Use update --follow-moves to update github.setup.\n", update.Release.Repository, update.Renamed)
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
		// A checksum refresh's plan is its check: it says which archives
		// don't match, and exits 3 where any doesn't, for a script.
		if request.Action == model.EditChecksums {
			writeLeft(out, update.ChecksumsLeft)
			if len(update.Differing) > 0 {
				fmt.Fprintf(out, "%d of %s don't match their checksums: %s\n", len(update.Differing), prose.Plural(update.Compared, "distfile"), strings.Join(update.Differing, ", "))
				return branch, update, &ExitError{Code: ExitAttention}
			}
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
		what += fmt.Sprintf(" (%s)", prose.Plural(update.Distfiles, "distfile"))
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
		what += fmt.Sprintf(" and %s (%d changed)", prose.Plural(block.Count, entry), block.Changed)
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
	// A crate the Portfile pinned over its lock goes once the new lock
	// moves past it, and says so (termusic's, field testing, 2026-10-02).
	for _, block := range update.Regenerated {
		for _, pin := range block.Dropped {
			fmt.Fprintf(out, "Removed the Portfile pin for %s %s (lockfile version: %s); the new lockfile now specifies %s.\n", pin.Name, pin.Pinned, pin.Was, pin.Locked)
		}
		if block.Unchecked != "" {
			fmt.Fprintln(out, block.Unchecked)
		}
		for _, notice := range block.Notices {
			fmt.Fprintln(out, "Notice: "+notice)
		}
	}
	if stealth := update.Stealth; stealth != nil {
		if stealth.RevbumpProblem != "" {
			fmt.Fprintf(out, "! the revision was not bumped: %s. Bump it yourself if the change needs a rebuild.\n", stealth.RevbumpProblem)
		}
		if stealth.Problem != "" {
			fmt.Fprintf(out, "! dist_subdir was not set: %s. Set it yourself, so mirrors keep both archives: dist_subdir ${name}/${version}_${revision} with a revision bump, else ${name}/${version}_1\n", stealth.Problem)
		}
		if stealth.Revbumped {
			fmt.Fprintln(out, "Bumped the revision because the source changed. Use --no-revbump only if the change needs no rebuild.")
		} else if stealth.RevbumpProblem == "" {
			fmt.Fprintln(out, "Inspect the source change before deciding whether it needs a revision bump.")
		}
		fmt.Fprintln(out, "dockhand diff --source shows what changed inside the archive.")
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
	if !linked.goesOn {
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
	return nextIn(ctx, e, branch, "dockhand check")
}

// nextIn is a Next: line's steps, run in a branch: as they are where it's
// checked out, and after a cd into its worktree elsewhere, since a check
// from outside takes its committed head, not its edits. A Next: names
// only what would be accepted (the release bar's rule), so the branch's
// steps run where the branch is.
func nextIn(ctx context.Context, e *engine.Engine, branch model.Branch, steps ...string) string {
	line := strings.Join(steps, ", then ")
	if current, err := e.Current(ctx); err == nil && current.ID == branch.ID {
		return line
	}
	return fmt.Sprintf(`cd "$(dockhand path %s)", then %s`, branch.ShortName(), line)
}

// announce says which branch an edit is in, and whether it was just
// started.
func announce(out io.Writer, branch model.Branch, started bool) {
	if started {
		fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", branch.Name, branch.Base.Short())
	}
	fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), config.Tilde(branch.Worktree))
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
	case request.Start != nil && request.NoHandBranch:
		kept = "\nNo branch was started."
	default:
		kept = "\nKept: the branch, unchanged."
	}
	port := request.Port
	var toWrite *engine.ChecksumsToWrite
	switch {
	case request.Action == model.EditChecksums && errors.As(err, &toWrite):
		block := strings.ReplaceAll(portfile.ChecksumsBlock(toWrite.Checksums), "\n", "\n    ")
		return fmt.Errorf("Could not update the checksums for %s automatically: %s%s\nUse these checksums in the Portfile:\n    %s\nEdit with: dockhand edit %s", port, reason, kept, block, port)
	case request.Action == model.EditChecksums:
		return fmt.Errorf("Could not update the checksums for %s automatically: %s%s\nRun port checksum %s, then copy the reported checksums into the Portfile.\nEdit with: dockhand edit %s", port, reason, kept, port, port)
	case unlocated != nil:
		return fmt.Errorf("Could not update %s automatically: %s%s\nEdit the version, then run dockhand checksums %s to print the checksums to copy into the Portfile.\nEdit with: dockhand edit %s", port, reason, kept, port, port)
	}
	// checksums meets what stopped the update, evaluating the same
	// Portfile, so it isn't named: git's and qemu's refusals each sent
	// the person to a checksums that refused them again (the hugo
	// exercise, field testing). A Next: names only what would be
	// accepted (the release bar's rule).
	return fmt.Errorf("Could not update %s automatically: %s%s\nEdit the version, run port checksum %s, then copy the reported checksums into the Portfile.\nEdit with: dockhand edit %s", port, reason, kept, port, port)
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
	// One that isn't a release, as certgraph's tag on an older commit
	// isn't, is better left out by the port's own livecheck than updated
	// to "after a look" (field testing at c97bca63).
	return exitf(ExitAttention, "can't tell whether %s is current, so nothing was changed: %s\nIf %s is a release: dockhand %s %s %s\nIf it isn't, as an old tag spelled oddly: a livecheck.regex in the Portfile that leaves %s out keeps it out of later looks%s", port, setAsideWords(aside), aside[0].Tag, verb, port, aside[0].Source, aside[0].Tag, kept)
}

// heldBeforeEdit is an update nobody looks over, bump's or ship's with
// --yes, held before its edit for what would hold its submission after
// the check, said as a held submission is. After a look, ship on a
// terminal goes ahead, since a person answers it; it named update
// --submit, which v0.4.0 removed (Codex's review's tests, rc2).
func heldBeforeEdit(request engine.UpdateRequest, held []string) error {
	next := "dockhand ship " + request.Port + ", on a terminal"
	if request.Version != "" {
		next = fmt.Sprintf("dockhand update %s %s --new, then dockhand ship -p %s on a terminal", request.Port, request.Version, request.Port)
	}
	return exitf(ExitAttention, "%s waits for your look, so nothing was changed: %s\nOnce it's fine: %s", request.Port, strings.Join(held, "; "), next)
}

// writeStealth shows each changed archive's checksums, before and after.
func writeStealth(out io.Writer, stealth *engine.Stealth) {
	for _, distfile := range stealth.Distfiles {
		if len(stealth.Distfiles) > 1 {
			fmt.Fprintf(out, "  %s\n", distfile.Name)
		}
		fmt.Fprintf(out, "  was   %s\n  now   %s\n", distfile.Was.Words(), distfile.Now.Words())
	}
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
	fmt.Fprintf(out, "Direct library dependents, from the index at %s:\n  %s\n", done.Base.Short(), orNone(prose.Few(indexed, 12, "port")))
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
	fmt.Fprintf(out, "Revision bumped %s; subject \"<port>: %s\" recorded for tidy.\n", prose.Plural(len(done.Bumped), "port"), done.Subject)
	return names, nil
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

// chooseBranch is the branch an authoring command works in, as
// engine.Select finds it: -b's, an existing one; one --new starts; the one
// checked out here; or the port's own open branch, one started for it
// where none changes it, said first. Where several change it, a terminal
// is asked once which, or for a new one, and a script is told them.
func chooseBranch(ctx context.Context, e *engine.Engine, streams Streams, where branchChoice, port, purpose string) (model.Branch, bool, error) {
	what := whatFor(purpose)
	if purpose == "update" {
		what = "version"
	}
	selection, err := e.Select(ctx, engine.Selector{Branch: where.branch, New: where.isNew(), NewName: where.newName(), Port: port, Policy: engine.SelectOrStart}, what, purpose)
	if err != nil {
		return model.Branch{}, false, err
	}
	return settleSelection(ctx, e, streams, selection, port, purpose, !where.isNew())
}

// settleSelection takes a selection: the branch found, said where it was chosen
// for the port; the one to start, started; or the choices, asked about
// once, a new branch among them where an authoring command may start one
// (the split's 7d row 7: the two questions, "start another?" and "which
// one?", as one), and refused in a script.
func settleSelection(ctx context.Context, e *engine.Engine, streams Streams, selection engine.Selection, port, purpose string, say bool) (model.Branch, bool, error) {
	switch {
	case selection.Start != nil && selection.Start.Name == "":
		fmt.Fprintf(streams.Err, "%s is in no open branch, so this starts one for it, named for the version it moves to.\n", port)
		return model.Branch{}, false, errStartForVersion
	case selection.Start != nil:
		if say {
			fmt.Fprintf(streams.Err, "%s is in no open branch, so this starts %s for it.\n", port, engine.BranchName(selection.Start.Name))
		}
		branch, err := e.Start(ctx, *selection.Start)
		return branch, err == nil, err
	case len(selection.Choices) > 0:
		chosen, start, err := choose(ctx, e, streams, port, selection.Choices, purpose != "")
		if err != nil || !start {
			return chosen, false, err
		}
		what := whatFor(purpose)
		name, err := e.NameFor(ctx, port, what)
		if err != nil {
			return model.Branch{}, false, err
		}
		branch, err := e.Start(ctx, engine.StartRequest{Name: name, Ports: []string{port}})
		return branch, err == nil, err
	}
	if selection.Said != "" {
		more := ""
		if purpose != "" {
			more = "; --new starts another"
		}
		fmt.Fprintf(streams.Err, "Working in %s, %s%s.\n", selection.Branch.ShortName(), selection.Said, more)
	}
	return selection.Branch, false, nil
}

// choose asks which of the open branches changing a port to work in, one
// question, a new branch among the answers where one may be started; a
// script is told them all, by exact name.
func choose(ctx context.Context, e *engine.Engine, streams Streams, port string, found []engine.PortBranch, mayStart bool) (model.Branch, bool, error) {
	if !streams.terminal() {
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
	answer, err := ask(streams, question)
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

// errStartForVersion is an update's branch to start once the update knows
// the version it moves to, which names it.
var errStartForVersion = errors.New("start the branch for the version the update finds")

// whatFor is what a branch an authoring command starts is named for
// (engine.NameFor): an edit's says nothing more than its port.
func whatFor(purpose string) string {
	switch purpose {
	case "checksums":
		return "checksums"
	case "revbump":
		return "rebuild"
	}
	return ""
}

func startFor(ctx context.Context, e *engine.Engine, port, what string) (model.Branch, bool, error) {
	name, err := e.NameFor(ctx, port, what)
	if err != nil {
		return model.Branch{}, false, err
	}
	branch, err := e.Start(ctx, engine.StartRequest{Name: name, Ports: []string{port}})
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
		fmt.Fprintf(out, "· %s to %s\n", engine.PatchesApply(update.PatchesApplied, len(update.PatchProblems)), source)
	}
	for _, problem := range update.PatchProblems {
		fmt.Fprintf(out, "! patch %s\n", problem)
	}
	for _, dropped := range update.PatchesDropped {
		fmt.Fprintf(out, "Removed patch %s and its file; the new source already includes the fix.\n", dropped)
	}
	for _, unchecked := range update.PatchesUnchecked {
		fmt.Fprintf(out, "· patch %s\n", unchecked)
	}
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
// already says whose it is (engine.UnderUpstream).
func underUpstream(change model.UpstreamChange) model.UpstreamChange {
	return engine.UnderUpstream(change)
}

func upstreamWords(change model.UpstreamChange) string {
	if change.Hold {
		return "! " + change.Message
	}
	return "· " + change.Message
}

// matchWords says that every archive a checksum refresh compared matches
// the Portfile's checksums for it.
func matchWords(compared int) string {
	switch compared {
	case 0:
		return "its checksums are current"
	case 1:
		return "its distfile matches its checksums"
	}
	return fmt.Sprintf("all %d distfiles match their checksums", compared)
}

// writeLeft says what a checksum refresh left as it was, and why, a line
// each: a context it couldn't read, a declaration none it read fetches,
// dependency entries it couldn't write.
func writeLeft(out io.Writer, left []engine.ChecksumLeft) {
	for _, l := range left {
		fmt.Fprintf(out, "Left as it was: %s, %s\n", l.What, l.Reason)
	}
}
