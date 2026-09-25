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
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/record"
)

// testPreparer, when set, stands in for MacPorts in every engine a
// command opens.
var testPreparer func(*engine.Engine) engine.Preparer

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
	var plan, keepOld, shared bool
	var linked linkedOptions
	var batch outdatedOptions
	cmd := &cobra.Command{
		Use:   "update <port> [version]",
		Short: "Update a port to a newer release",
		Long: `Moves a port to the newest release upstream, or the version named, and fills
in its checksums, in the branch's working files. Nothing is committed.

The branch is --branch, else the one checked out here; --new starts one.
--plan shows the edit and changes nothing.

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
Without a terminal, the tidy applies only when it is made of dockhand's own
edits alone.`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if linked.submit && (batch.outdated || plan) {
				return errors.New("--submit goes with one port's update, not --plan or --outdated")
			}
			if batch.outdated {
				return updateOutdated(cmd.Context(), s, streams, args, batch)
			}
			if batch.mine || batch.check {
				return errors.New("--mine and --check go with --outdated")
			}
			if len(args) == 0 {
				return errors.New("name the port to update, or update your outdated ports with --outdated --mine")
			}
			if len(linked.except) > 0 && !linked.revbump {
				return errors.New("--except takes a port out of --revbump-dependents; add --revbump-dependents")
			}
			request := engine.UpdateRequest{Action: record.Bump, Port: args[0], KeepOldChecksums: keepOld, SharedRelease: shared, Plan: plan, CompareUpstream: true}
			if len(args) == 2 {
				request.Version = args[1]
			}
			branch, update, err := author(cmd.Context(), s, streams, where, "update", request, linked)
			if err != nil || !linked.submit || !update.Applied {
				return err
			}
			return tidyAndSubmit(cmd.Context(), s, streams, branch)
		},
	}
	where.flags(cmd)
	cmd.Flags().BoolVar(&plan, "plan", false, "show the edit and change nothing")
	cmd.Flags().BoolVar(&linked.submit, "submit", false, "then tidy it, check it, and submit it once the check passes")
	cmd.Flags().BoolVar(&keepOld, "keep-old-checksums", false, "refresh legacy md5 or sha1 checksums in place rather than rewriting them as rmd160, sha256, and size")
	cmd.Flags().BoolVar(&shared, "shared-release", false, "move every subport that shares the port's release")
	cmd.Flags().BoolVar(&linked.revbump, "revbump-dependents", false, "also bump the revision of the ports that link it directly")
	cmd.Flags().StringSliceVar(&linked.except, "except", nil, "leave this dependent out of --revbump-dependents")
	cmd.Flags().BoolVar(&batch.outdated, "outdated", false, "update every named port, or with --mine yours, that has a newer release")
	cmd.Flags().BoolVar(&batch.mine, "mine", false, "with --outdated, the ports whose maintainers line names you (config maintainer)")
	cmd.Flags().BoolVar(&batch.check, "check", false, "with --outdated, also queue a check of each")
	cmd.Flags().BoolVarP(&batch.yes, "yes", "y", false, "with --outdated, start without asking")
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
			request := engine.UpdateRequest{Action: record.RefreshChecksums, Port: args[0], KeepOldChecksums: keepOld, KeepRevision: keepRevision, Plan: plan}
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

// tidyAndSubmit is the rest of update --submit: tidy the branch, then
// submit --check, each previewed as its own command previews it.
func tidyAndSubmit(ctx context.Context, s *settings, streams Streams, branch model.Branch) error {
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
		writeTidyPlan(out, proposal)
		applied, err := decideTidy(ctx, e, streams, proposal, false, false, "")
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
	return submitChecked(ctx, s, e, streams, engine.SubmitRequest{Branch: branch}, nil)
}

// linkedOptions are update's --revbump-dependents and --except, and
// --submit, which goes on from the edit.
type linkedOptions struct {
	revbump bool
	except  []string
	submit  bool
}

// author finds the branch, makes the edit, and reports it.
func author(ctx context.Context, s *settings, streams Streams, where branchChoice, purpose string, request engine.UpdateRequest, linked linkedOptions) (branch model.Branch, update engine.Update, err error) {
	if where.new && request.Plan {
		return model.Branch{}, engine.Update{}, fmt.Errorf("--plan changes nothing, so it starts no branch; plan in an existing one with --branch, or drop --plan")
	}
	e, err := s.open(ctx)
	if err != nil {
		return branch, update, err
	}
	defer e.Close()
	var started bool
	branch, started, err = chooseBranch(ctx, e, streams, where, request.Port, purpose)
	if err != nil {
		return branch, update, err
	}
	out := streams.Out
	if started {
		fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", branch.Name, engine.Short(branch.Base))
	}
	fmt.Fprintf(out, "%s · %s\n", branch.ShortName(), tilde(branch.Worktree))
	request.Branch = branch
	update, err = e.Update(ctx, request)
	if errors.Is(err, preparation.ErrUnsupported) {
		return branch, update, byHand(err, request, branch, started)
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
		if request.Action == record.Bump {
			fmt.Fprintf(out, "%s is already at %s; nothing to change.\n", update.Port, update.After)
		} else {
			fmt.Fprintf(out, "%s %s's checksums are current; nothing to change.\n", update.Port, update.After)
		}
		return branch, update, nil
	}
	if request.Action == record.Bump {
		fmt.Fprintf(out, "%s: %s → %s%s\n", update.Port, update.Before, update.After, releaseLabel(update.Release))
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
		if linked.revbump {
			result.Revbumped, err = revbumpLinked(ctx, e, out, branch, update, linked.except, true)
			streams.emit(result)
			return branch, update, err
		}
		return branch, update, nil
	}
	what := "Updated version and checksums"
	if request.Action == record.RefreshChecksums {
		what = "Updated checksums"
	}
	if update.Distfiles > 0 {
		what += fmt.Sprintf(" (%s)", plural(update.Distfiles, "distfile"))
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
		fmt.Fprintln(out, "Removed dist_subdir: a stealth update set it for the old version, and the new version's archive has a name of its own.")
	}
	writeUpstream(out, update.Upstream)
	for _, problem := range update.PatchProblems {
		fmt.Fprintf(out, "! patch %s\n", problem)
	}
	if linked.revbump {
		if result.Revbumped, err = revbumpLinked(ctx, e, out, branch, update, linked.except, false); err != nil {
			return branch, update, err
		}
		streams.emit(result)
	}
	if !linked.submit {
		fmt.Fprintln(out, "Next: review it with git diff, then commit it")
	}
	return branch, update, nil
}

// byHand says that dockhand can't make the edit by itself, why, and how
// to make it by hand (Design v3 §6.3).
func byHand(err error, request engine.UpdateRequest, branch model.Branch, started bool) error {
	reason := strings.TrimPrefix(err.Error(), preparation.ErrUnsupported.Error()+": ")
	kept := "the branch, unchanged"
	if started {
		kept = branch.Name + ", with nothing changed"
	}
	switch request.Action {
	case record.RefreshChecksums:
		return fmt.Errorf("can't refresh %s's checksums by itself: %s\nKept: %s.\nWrite them yourself, as port checksum %s reports them:\n  dockhand edit %s",
			request.Port, reason, kept, request.Port, request.Port)
	}
	return fmt.Errorf("can't update %s by itself: %s\nKept: %s.\nEdit the version yourself; dockhand checksums %s then fills in the rest:\n  dockhand edit %s",
		request.Port, reason, kept, request.Port, request.Port)
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

// revbumpLinked bumps the revision of the ports that link an updated one
// directly, or with plan lists them.
func revbumpLinked(ctx context.Context, e *engine.Engine, out io.Writer, branch model.Branch, update engine.Update, except []string, plan bool) ([]string, error) {
	linked, err := e.LinkedPorts(ctx, branch, update.Port, except)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, dependent := range linked.Bump {
		names = append(names, dependent.Name)
	}
	fmt.Fprintf(out, "Direct library dependents, from the index at %s:\n  %s\n", engine.Short(linked.Base), orNone(strings.Join(names, "  ")))
	for _, dependent := range linked.Changed {
		fmt.Fprintf(out, "  · %s: the branch already changes it, so it is left as it is\n", dependent.Name)
	}
	if len(linked.Excepted) > 0 {
		fmt.Fprintf(out, "  · left out with --except: %s\n", strings.Join(linked.Excepted, ", "))
	}
	if plan || len(linked.Bump) == 0 {
		return nonNil(names), nil
	}
	subject := fmt.Sprintf("rebuild for %s %s", update.Port, update.After.Version)
	for _, dependent := range linked.Bump {
		if _, err := e.Update(ctx, engine.UpdateRequest{Branch: branch, Action: record.BumpRevision, Port: dependent.Name, Subject: subject}); err != nil {
			return nil, fmt.Errorf("revision-bumping %s: %w; the ports before it are bumped", dependent.Name, err)
		}
	}
	fmt.Fprintf(out, "Revision bumped %s; subject \"<port>: %s\" recorded for tidy.\n", plural(len(linked.Bump), "port"), subject)
	return names, nil
}

func releaseLabel(release *record.Release) string {
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
	// adopt; master, or no branch at all, is no context.
	if current, currentErr := e.Repo.CurrentBranch(ctx); currentErr == nil && current != "master" && current != "main" {
		return model.Branch{}, false, err
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
			return model.Branch{}, false, fmt.Errorf("%s is in no open branch, and this checkout is on none; start one with --new, or name one with --branch <name>", port)
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

// writeUpstream reports what comparing the upstream archives found.
func writeUpstream(out io.Writer, comparison *model.UpstreamComparison) {
	switch {
	case comparison == nil:
	case comparison.Problem != "":
		fmt.Fprintf(out, "Upstream archives not compared: %s\n", comparison.Problem)
	case len(comparison.Changes) == 0:
		fmt.Fprintln(out, "Upstream archives compared: no license, build file, or dependency changes.")
	default:
		fmt.Fprintln(out, "Upstream archives compared:")
		for _, change := range comparison.Changes {
			fmt.Fprintf(out, "  %s\n", upstreamWords(change))
		}
	}
}

func upstreamWords(change model.UpstreamChange) string {
	if change.Hold {
		return "! " + change.Message
	}
	return "· " + change.Message
}
