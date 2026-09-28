package engine

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
	"github.com/herbygillot/dockhand/internal/store"
)

// UpdateRequest asks to edit one port's files in a branch's worktree.
type UpdateRequest struct {
	Branch model.Branch
	// Action is model.EditUpdate, a new version; model.EditChecksums, the
	// checksums of the version the Portfile names; or model.EditRevbump, a
	// new revision. The edit it records is of the same kind.
	Action model.EditKind
	Port   string
	// Version is the release a bump moves to; the newest when empty.
	Version string
	// KeepOldChecksums refreshes a legacy checksum group's values in place
	// instead of rewriting it as rmd160, sha256, and size.
	KeepOldChecksums bool
	// SharedRelease moves every subport that shares the port's release.
	SharedRelease bool
	// Subject is the reason for a revision bump, the commit subject after
	// the port's name: "rebuild for poppler 25.09.0".
	Subject string
	// Plan prepares the edit and changes nothing.
	Plan bool
	// FromMaster plans against master as fetched now, with no branch: a
	// look before starting one. It goes only with Plan.
	FromMaster bool
	// Start, for a version update, prepares it against master as fetched
	// now and starts this branch from that master only once there is an
	// edit to make: a port already current starts nothing. An edit
	// dockhand can't make by itself starts it too, for the person to make
	// by hand. It takes the place of Branch.
	Start *StartRequest
	// KeepRevision leaves the revision of a stealth update as it is, for a
	// change that needs no rebuild.
	KeepRevision bool
	// CompareUpstream fetches the current version's archives beside the
	// new ones and compares them, for a version update (Design v3 §6.12).
	CompareUpstream bool
}

// PortVersion is a port's version and revision.
type PortVersion struct {
	Version  string
	Revision int
}

func (v PortVersion) String() string {
	if v.Revision == 0 {
		return v.Version
	}
	return fmt.Sprintf("%s_%d", v.Version, v.Revision)
}

// ErrUnsupported is an edit dockhand can't make by itself, such as a
// version it can't find in the Portfile; the error says why, after the
// sentinel's own words.
var ErrUnsupported = preparation.ErrUnsupported

// Update reports an update or checksum refresh.
type Update struct {
	Branch model.Branch
	// Base is the commit the update was prepared on: the branch's base, or
	// master as fetched for a plan from master.
	Base model.ObjectID
	// Port is the name the Portfile evaluates to.
	Port          string
	Before, After PortVersion
	// Release is where a bump found its version.
	Release *model.Release
	// Files are the paths the edit changes, sorted.
	Files []string
	// Diff is the edit as a patch.
	Diff string
	// Subject is the commit subject the edit would be committed with.
	Subject string
	// Distfiles counts the archives whose checksums were written.
	Distfiles int
	// PatchProblems name the port's patches that no longer apply, and
	// PatchesUnchecked those no check reached before the build.
	PatchProblems    []string
	PatchesUnchecked []string
	// Current is true when there was nothing to change.
	Current bool
	// Started is true when the update started its branch
	// (UpdateRequest.Start).
	Started bool
	// Applied is true when the working files were written.
	Applied bool
	// Upstream is what comparing the old and new upstream archives found,
	// when the update compared them.
	Upstream *model.UpstreamComparison
	// Stealth is a checksum refresh's stealth update, when it found one.
	Stealth *Stealth
	// DistSubdirRemoved is true when a version update removed the
	// dist_subdir an earlier stealth update set.
	DistSubdirRemoved bool
}

// Update edits a port's files in the branch's worktree, as the worktree
// stands, committed or not, and commits nothing: `dockhand check` takes it
// from there. The edit is prepared from a capture of the tracked files, and
// written only if none of the files it touches changed in the meantime.
func (e *Engine) Update(ctx context.Context, request UpdateRequest) (Update, error) {
	switch request.Action {
	case model.EditUpdate, model.EditChecksums:
	case model.EditRevbump:
		if strings.TrimSpace(request.Subject) == "" {
			return Update{}, errors.New("a revision bump needs its reason as the subject, such as --subject \"rebuild for poppler 25.09.0\"")
		}
	default:
		return Update{}, fmt.Errorf("engine: %s is not an update", request.Action)
	}
	if !macports.ValidName(request.Port) {
		return Update{}, fmt.Errorf("%q is not a port name", request.Port)
	}
	branch := request.Branch
	worktree, captured, base, err := e.updateSource(ctx, request)
	if err != nil {
		return Update{}, err
	}
	// start starts the branch a Start request asks for, from the master
	// the update was prepared on.
	start := func() error {
		if request.Start == nil {
			return nil
		}
		started := *request.Start
		started.Base = base
		if branch, err = e.Start(ctx, started); err != nil {
			return err
		}
		worktree, err = e.worktree(ctx, branch)
		return err
	}
	// byHand keeps, for an edit dockhand can't make, the branch the
	// person will make it in.
	byHand := func(err error) (Update, error) {
		if request.Start != nil && errors.Is(err, ErrUnsupported) {
			if startErr := start(); startErr != nil {
				return Update{}, errors.Join(err, startErr)
			}
			return Update{Branch: branch, Base: base, Port: request.Port, Started: true}, err
		}
		return Update{}, err
	}
	preparer, err := e.preparer()
	if err != nil {
		return Update{}, err
	}
	input := preparation.Request{
		EditIntent: model.EditIntent{SharedRelease: request.SharedRelease, KeepOldChecksums: request.KeepOldChecksums},
		Action:     request.Action,
		Source:     model.Source{Tree: model.ObjectID(captured), Base: model.ObjectID(base)},
		Selection:  macports.Selection{Selector: request.Port},
		Version:    request.Version,
		Subject:    request.Subject,
	}
	if request.Action == model.EditChecksums {
		// A Portfile the branch has changed since its base was edited by
		// hand first, as for a new version, and its refresh is no stealth
		// update. Which files those are is the branch's to say; the editor
		// makes the stealth update of the rest.
		changed, err := changedSinceBase(ctx, worktree, captured, base)
		if err != nil {
			return Update{}, err
		}
		input.Stealth = &preparation.StealthRequest{Changed: changed, KeepRevision: request.KeepRevision}
	}
	if request.Action == model.EditUpdate {
		release, err := preparer.ResolveRelease(ctx, input)
		if err != nil {
			return byHand(err)
		}
		input.Release = &release
	}
	compare := request.CompareUpstream && request.Action == model.EditUpdate
	if compare {
		directory, err := scratch.Dir("upstream-")
		if err != nil {
			return Update{}, err
		}
		defer os.RemoveAll(directory)
		input.KeepArchives = directory
	}
	result, err := preparer.Prepare(ctx, input)
	if err != nil {
		return byHand(err)
	}
	update := describe(branch, request.Port, result)
	update.Base = base
	update.Stealth, update.DistSubdirRemoved = result.Stealth, result.DistSubdirRemoved
	if result.Stealth != nil {
		update.Subject = update.Port + ": update checksums after a stealth update"
	}
	if compare && len(result.Files) > 0 {
		update.Upstream = compareUpstream(ctx, result)
	}
	if change, ok := toolchainChange(result.GoToolchain); ok && len(result.Files) > 0 {
		if update.Upstream == nil {
			update.Upstream = &model.UpstreamComparison{Changes: []model.UpstreamChange{}}
		}
		update.Upstream.Changes = append(update.Upstream.Changes, change)
	}
	if len(result.Files) == 0 {
		update.Current = true
		return update, nil
	}
	diff, err := worktree.DiffTrees(ctx, captured, string(result.PreparedTree))
	if err != nil {
		return Update{}, err
	}
	update.Diff = string(diff)
	if request.Plan {
		return update, nil
	}
	if request.Start != nil {
		if err := start(); err != nil {
			return update, err
		}
		update.Branch, update.Started = branch, true
	}

	if err := expandFor(ctx, worktree, update.Files); err != nil {
		return Update{}, err
	}
	if err := worktree.ApplyToWorkingFiles(ctx, result.Files); err != nil {
		if errors.Is(err, git.ErrWorkingFile) {
			return Update{}, fmt.Errorf("%w; nothing was written, so run it again", err)
		}
		return Update{}, err
	}
	update.Applied = true
	edit, err := e.editRecord(ctx, worktree, branch, request, update, result)
	if err != nil {
		return update, err
	}
	change := "refreshed checksums"
	switch request.Action {
	case model.EditUpdate:
		change = fmt.Sprintf("%s → %s", update.Before, update.After)
	case model.EditRevbump:
		change = fmt.Sprintf("revision %d → %d", update.Before.Revision, update.After.Revision)
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddEdit(edit); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: edit.At, Branch: branch.ID, Kind: "branch.edit", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: %s (%s)", update.Port, change, listPaths(update.Files))})
		return err
	})
	return update, err
}

// updateSource is what an update is prepared from: the branch's working
// files on its base, or for a plan from master, master's tree as fetched
// now, read through the clone, since a plan only reads.
func (e *Engine) updateSource(ctx context.Context, request UpdateRequest) (*git.Repository, string, model.ObjectID, error) {
	if request.FromMaster || request.Start != nil {
		if request.FromMaster && !request.Plan || request.Action != model.EditUpdate {
			return nil, "", "", errors.New("engine: only a version update is prepared from master; start a branch for anything else")
		}
		master, err := e.fetchMaster(ctx)
		if err != nil {
			return nil, "", "", err
		}
		trees, err := e.Repo.CommitTrees(ctx, []string{string(master)})
		if err != nil {
			return nil, "", "", err
		}
		return e.Repo, trees[string(master)], master, nil
	}
	worktree, err := e.worktree(ctx, request.Branch)
	if err != nil {
		return nil, "", "", err
	}
	_, captured, err := worktree.WorkingTree(ctx)
	if err != nil {
		return nil, "", "", fmt.Errorf("reading %s's working files: %w", request.Branch.Worktree, err)
	}
	return worktree, captured, request.Branch.Base, nil
}

// editRecord is what tidy later reads: each file's blob before and after,
// and the subject the edit carries.
func (e *Engine) editRecord(ctx context.Context, worktree *git.Repository, branch model.Branch, request UpdateRequest, update Update, result preparation.Result) (model.Edit, error) {
	edit := model.Edit{ID: model.EditID(store.NewID("ed")), Branch: branch.ID, Kind: request.Action, Port: update.Port, Subject: update.Subject, At: e.now(),
		Upstream: update.Upstream, Release: update.Release}
	edit.Directory = portDirectory(result.Files[0].Path)
	if result.Target.Portfile != "" {
		edit.Directory = path.Dir(result.Target.Portfile)
	}
	if edit.Subject == "" {
		switch edit.Kind {
		case model.EditChecksums:
			edit.Subject = update.Port + ": update checksums"
		case model.EditRevbump:
			edit.Subject = update.Port + ": " + strings.TrimSpace(request.Subject)
		default:
			edit.Subject = update.Port + ": update to " + update.After.Version
		}
	}
	for _, file := range result.Files {
		recorded := model.EditedFile{Path: file.Path, Before: model.ObjectID(file.Before.Blob)}
		if !file.Delete {
			blob, err := worktree.BlobID(ctx, file.After)
			if err != nil {
				return model.Edit{}, err
			}
			recorded.After = model.ObjectID(blob)
		}
		edit.Files = append(edit.Files, recorded)
	}
	return edit, nil
}

// portDirectory is the <category>/<port> a path is under, or its directory
// when it is not in one.
func portDirectory(file string) string {
	if directory, ok := macports.PortDirectoryOf(file); ok {
		return directory
	}
	return path.Dir(file)
}

// changedSinceBase are the files a branch's captured tree has changed since
// its base.
func changedSinceBase(ctx context.Context, worktree *git.Repository, captured string, base model.ObjectID) ([]string, error) {
	trees, err := worktree.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return nil, err
	}
	return worktree.ChangedPaths(ctx, trees[string(base)], captured)
}

// describe reads what the preparation found.
func describe(branch model.Branch, selector string, result preparation.Result) Update {
	update := Update{Branch: branch, Port: result.Target.Name, Release: result.Release, PatchProblems: result.PatchProblems(), PatchesUnchecked: result.UncheckedPatches(),
		Distfiles: len(result.Downloads) + len(result.Crates)}
	if update.Port == "" {
		update.Port = selector
	}
	if len(result.Fidelity) > 0 {
		before := result.Fidelity[0].Before.Ports[update.Port]
		after := result.Fidelity[len(result.Fidelity)-1].After.Ports[update.Port]
		update.Before = PortVersion{Version: before.Version, Revision: before.Revision}
		update.After = PortVersion{Version: after.Version, Revision: after.Revision}
	} else if port := result.Unchanged; port != nil {
		// Nothing was edited, so the port is at what it was.
		update.Before = PortVersion{Version: port.Version, Revision: port.Revision}
		update.After = update.Before
	}
	for _, file := range result.Files {
		update.Files = append(update.Files, file.Path)
	}
	slices.Sort(update.Files)
	if len(result.Commits) > 0 {
		update.Subject = result.Commits[0].Subject
	}
	return update
}

// worktree opens the branch's checkout, which must have the branch checked
// out.
func (e *Engine) worktree(ctx context.Context, branch model.Branch) (*git.Repository, error) {
	if branch.Worktree == "" {
		return nil, fmt.Errorf("%s is not checked out anywhere; check it out with git switch %s", branch.Name, branch.Name)
	}
	if err := e.checkOutAgain(ctx, branch); err != nil {
		return nil, err
	}
	worktree, err := git.Open(ctx, branch.Worktree, e.options.Git)
	if err != nil {
		return nil, err
	}
	current, err := worktree.CurrentBranch(ctx)
	if err != nil || current != branch.Name {
		return nil, fmt.Errorf("%s is not checked out in %s any more; switch back with git switch %s", branch.Name, branch.Worktree, branch.Name)
	}
	return worktree, nil
}

// expandFor adds the directories of the ports the files belong to to a
// sparse worktree's cone, so the edited files are there to write.
func expandFor(ctx context.Context, worktree *git.Repository, files []string) error {
	cone, err := worktree.SparseCone(ctx)
	if err != nil || len(cone) == 0 {
		return err
	}
	var missing []string
	for _, file := range files {
		directory := portDirectory(file)
		if directory == "." || slices.Contains(missing, directory) {
			continue
		}
		covered := slices.ContainsFunc(cone, func(c string) bool { return directory == c || strings.HasPrefix(directory, c+"/") })
		if !covered {
			missing = append(missing, directory)
		}
	}
	return worktree.ExpandSparse(ctx, missing...)
}

// BranchesChanging lists the open branches whose commits or working files
// change a port, by its directory's name: update commits nothing, so a
// branch it edited changes the port before tidy commits it.
func (e *Engine) BranchesChanging(ctx context.Context, port string) ([]model.Branch, error) {
	var open []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		open, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		return err
	}); err != nil {
		return nil, err
	}
	var changing []model.Branch
	for _, branch := range open {
		head, _, err := e.Repo.Branch(ctx, branch.Name)
		if errors.Is(err, git.ErrBranchMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		paths, err := e.Repo.ChangedPaths(ctx, string(branch.Base), head)
		if err != nil {
			return nil, err
		}
		edited, err := e.workingEdits(ctx, branch)
		if err != nil {
			return nil, err
		}
		if slices.Contains(ScopeOf(append(paths, edited...)).PortNames(), port) {
			changing = append(changing, branch)
		}
	}
	return changing, nil
}

// workingEdits are the tracked files edited in a branch's worktree and not
// yet committed; none where it isn't checked out, which it leaves as it
// is.
func (e *Engine) workingEdits(ctx context.Context, branch model.Branch) ([]string, error) {
	if branch.Worktree == "" || !exists(branch.Worktree) {
		return nil, nil
	}
	worktree, err := git.Open(ctx, branch.Worktree, e.options.Git)
	if err != nil {
		return nil, err
	}
	if current, err := worktree.CurrentBranch(ctx); err != nil || current != branch.Name {
		return nil, nil
	}
	return worktree.TrackedChanges(ctx)
}

// FreeName is a name for a new branch for work on a port, per decision 37:
// <port>-<short ID>, such as jq-4k2p, which no branch, tracked or not, and
// no worktree directory already uses. It holds no verb or version, since
// the work may become more than it started as, and the name never changes.
func (e *Engine) FreeName(ctx context.Context, port string) (string, error) {
	for range 20 {
		name := port + "-" + shortID()
		if _, err := e.Resolve(ctx, name); err == nil {
			continue
		} else if !errors.Is(err, ErrNoBranch) {
			return "", err
		}
		if _, _, err := e.Repo.Branch(ctx, BranchName(name)); err == nil {
			continue
		} else if !errors.Is(err, git.ErrBranchMissing) {
			return "", err
		}
		if exists(e.worktreeDirectory(BranchName(name))) {
			continue
		}
		return name, nil
	}
	return "", fmt.Errorf("no free branch name for %s; name one with dockhand start <name>", port)
}

// shortID is four random lowercase letters and digits.
func shortID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	id := make([]byte, 4)
	for i := range id {
		id[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return string(id)
}

// toolchainChange is a Go release upstream's go.mod requires that the
// Portfile's go.toolchain_min doesn't, as a finding a passing build can't
// catch, since the builder's Go is new enough: it holds the update for a
// person's look, as a new declared dependency does.
func toolchainChange(toolchain *preparation.GoToolchain) (model.UpstreamChange, bool) {
	if toolchain == nil {
		return model.UpstreamChange{}, false
	}
	message := fmt.Sprintf("upstream: go.mod requires Go %s, and the Portfile declares no go.toolchain_min; declaring one gates the port on older Go, the maintainer's call", toolchain.Required)
	if toolchain.Declared != "" {
		message = fmt.Sprintf("upstream: go.mod requires Go %s, above go.toolchain_min %s, which isn't one literal declaration dockhand can raise; raise it by hand", toolchain.Required, toolchain.Declared)
	}
	return model.UpstreamChange{Kind: "toolchain", Path: "go.mod", Message: message, Hold: true}, true
}

// compareUpstream compares the current version's archives with the new
// version's, one distfile with the same one. Not being able to compare is
// reported, never a reason to refuse the update.
func compareUpstream(ctx context.Context, result preparation.Result) *model.UpstreamComparison {
	comparison := &model.UpstreamComparison{Changes: []model.UpstreamChange{}}
	switch {
	case result.PreviousProblem != "":
		comparison.Problem = "the current version's archives could not be fetched: " + result.PreviousProblem
		return comparison
	case len(result.Downloads) == 0:
		// A port fetched with git has no archives, so nothing to compare.
		return nil
	case len(result.Previous) != len(result.Downloads):
		comparison.Problem = fmt.Sprintf("the versions have %d and %d distfiles, so they can't be paired", len(result.Previous), len(result.Downloads))
		return comparison
	}
	for i, now := range result.Downloads {
		old := result.Previous[i]
		if old.Path == "" || now.Path == "" {
			comparison.Problem = "the archives were not kept to compare"
			return comparison
		}
		changes, err := sourcecompare.Compare(ctx, old.Path, now.Path)
		if err != nil {
			comparison.Problem = err.Error()
			return comparison
		}
		for _, change := range changes {
			comparison.Changes = append(comparison.Changes, model.UpstreamChange{Kind: change.Kind, Path: change.Path, Message: change.Message, Hold: change.Hold})
		}
	}
	return comparison
}
