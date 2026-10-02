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

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/project"
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
	// Release is that release where it was found already, as outdated
	// finds one: the update takes it rather than asking upstream again,
	// and checks it against the Portfile as any. Its version is Version's.
	Release *model.Release
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
	// LookForOthers names the port's other open pull requests, for a
	// version update, as submit's preview does: someone starting an update
	// should hear of one in flight. It holds nothing, and stops nothing.
	LookForOthers bool
	// Unattended is a version update no person looks over before it is
	// submitted, bump's. It looks for the port's other open pull requests
	// once its release is found and before it prepares the edit, and is
	// held there on one, or on not knowing (HeldBeforeEdit), since either
	// would hold the submission after the check: nothing is downloaded or
	// built for it. Submit looks again before publishing.
	Unattended bool
	// answered are the HTTPS answers the command making the update has had
	// already, create's for the homepage it wrote, which aren't asked
	// again (plainHTTP).
	answered map[string]bool
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

// ErrFidelity is an edit whose evaluation isn't the change intended, which
// update refuses as it refuses an edit it can't make, with how to make it
// by hand.
var ErrFidelity = preparation.ErrFidelity

// Unlocated is a checksum declaration dockhand can't find in the Portfile
// to edit, an unsupported edit that names the archive and why.
type Unlocated = preparation.Unlocated

// ChecksumsToWrite is a checksum refresh dockhand couldn't write, with the
// archives' checksums for a person to write.
type ChecksumsToWrite = preparation.ChecksumsToWrite

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
	// Distfiles counts the port's archives whose checksums were written,
	// and Regenerated the dependency blocks written again, a Git crate's
	// archive among them, each with its entries.
	Distfiles   int
	Regenerated []preparation.Regenerated
	// PatchProblems name the port's patches that no longer apply, and
	// PatchesUnchecked those no check reached before the build;
	// PatchesApplied counts those checked that apply.
	PatchProblems    []string
	PatchesUnchecked []string
	PatchesApplied   int
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
	// Others are the port's other open pull requests, where the update
	// looked for them (UpdateRequest.LookForOthers), and OthersProblem why
	// they couldn't be looked for.
	Others        []forge.PullRequestSummary
	OthersProblem string
	// PlainHTTP are the port's URLs over plain HTTP, homepage and
	// master_sites, each with whether its https form answers, since
	// MacPorts prefers HTTPS. They're said, never changed: that's the
	// maintainer's call, and no part of the edit.
	PlainHTTP []PlainURL
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
	if request.Release != nil && (request.Action != model.EditUpdate || request.Release.Version != request.Version) {
		return Update{}, fmt.Errorf("engine: the release found is %s's, for a version update to %q", request.Release.Version, request.Version)
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
		// Which files the branch changed since its base is the branch's to
		// say, and where its Portfile is among them, the port as the base
		// evaluates it: a version edited by hand since makes the refresh no
		// stealth update, while a comment or a homepage edited leaves it
		// one. The editor decides, and makes the stealth update.
		changed, err := changedSinceBase(ctx, worktree, captured, base)
		if err != nil {
			return Update{}, err
		}
		input.Stealth = &preparation.StealthRequest{Changed: changed, KeepRevision: request.KeepRevision,
			Base: e.basePort(ctx, model.Source{Tree: model.ObjectID(captured), Base: base}, base, request.Port, changed)}
	}
	// own is the branch's own pull request, which is no other.
	own := 0
	if branch.PullRequest != nil {
		own = branch.PullRequest.Number
	}
	if request.Action == model.EditUpdate {
		input.Release = request.Release
		release, err := preparer.ResolveRelease(ctx, input)
		if err != nil {
			return byHand(err)
		}
		input.Release = &release
		if request.Unattended && !release.NoUpdate {
			if err := e.heldBeforeEdit(ctx, request.Port, own); err != nil {
				return Update{}, err
			}
		}
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
	// A version update or a checksum refresh is where the port's URLs are
	// looked at, so its plain-HTTP ones are said there, beside the edit. A
	// port with nothing to change is said to be current without waiting on
	// its hosts.
	if (request.Action == model.EditUpdate || request.Action == model.EditChecksums) && len(result.Files) > 0 {
		if info, ok := result.PortAfter(update.Port); ok {
			update.PlainHTTP = e.plainHTTP(ctx, info, request.answered)
		}
	}
	update.Stealth, update.DistSubdirRemoved = result.Stealth, result.DistSubdirRemoved
	if result.Stealth != nil {
		update.Subject = update.Port + ": update checksums after a stealth update"
	}
	if len(result.Files) > 0 && (compare || result.GoToolchain != nil) {
		trees := [2]model.Source{{Tree: model.ObjectID(captured), Base: model.ObjectID(base)}, {Tree: result.PreparedTree, Base: model.ObjectID(base)}}
		update.Upstream = e.assessUpstream(ctx, result, sourcecompare.Versions{Old: update.Before.Version, New: update.After.Version}, trees, compare)
	}
	if len(result.Files) == 0 {
		update.Current = true
		return update, nil
	}
	// An unattended update looked before its edit, and found none.
	if request.LookForOthers && request.Action == model.EditUpdate && !request.Unattended {
		update.Others, update.OthersProblem = e.openPullRequests(ctx, []string{update.Port}, own)
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
	// Where the branch was as its base before the edit, as a new one is,
	// the update's own comparison is the assessment of the files it
	// leaves: its before is the base and its after the revision, and it
	// compared them in every context that fetches them (the assessment
	// design, D), or couldn't, which it says. A Git-fetched port's update
	// compared no archives, and records none, so its assessment is made
	// when it's collected.
	var primed *model.Assessment
	if after, _ := result.PortAfter(update.Port); compare && update.Upstream != nil && !after.GitFetched() {
		trees, err := worktree.CommitTrees(ctx, []string{string(base)})
		if err != nil {
			return update, err
		}
		if trees[string(base)] == captured {
			_, applied, err := worktree.WorkingTree(ctx)
			if err != nil {
				return update, err
			}
			primed = &model.Assessment{Branch: branch.ID, Tree: model.ObjectID(applied), Base: base, Port: update.Port, Directory: edit.Directory,
				Comparison: *update.Upstream, Policy: assess.Policy, At: edit.At}
		}
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddEdit(edit); err != nil {
			return err
		}
		if primed != nil {
			if err := tx.RecordAssessment(*primed); err != nil {
				return err
			}
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

// basePort is a port as the branch's base evaluates it, where the branch
// changed its Portfile since; nil where it didn't, where the base has no
// such port, or where it couldn't be evaluated, which a stealth update
// takes as a new version.
func (e *Engine) basePort(ctx context.Context, source model.Source, base model.ObjectID, name string, changed []string) *macports.PortInfo {
	reader, err := e.portReader()
	if err != nil {
		return nil
	}
	directory, err := reader.Directory(ctx, source, name)
	if err != nil || !slices.Contains(changed, directory+"/Portfile") {
		return nil
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return nil
	}
	ports, err := reader.Ports(ctx, model.Source{Commit: base, Tree: model.ObjectID(trees[string(base)]), Base: base}, directory, model.Environment{}, nil)
	if err != nil {
		return nil
	}
	for _, port := range ports {
		if port.Name == name {
			return &port
		}
	}
	return nil
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
	update := Update{Branch: branch, Port: result.Target.Name, Release: result.Release, PatchProblems: result.PatchProblems(), PatchesUnchecked: result.UncheckedPatches(), PatchesApplied: result.PatchesApplied(),
		Distfiles: len(result.Downloads), Regenerated: result.Regenerated}
	if update.Port == "" {
		update.Port = selector
	}
	if before, ok := result.PortBefore(update.Port); ok {
		update.Before = PortVersion{Version: before.Version, Revision: before.Revision}
	}
	if after, ok := result.PortAfter(update.Port); ok {
		update.After = PortVersion{Version: after.Version, Revision: after.Revision}
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
// out, checking it out again where clean removed it, for work on the
// branch.
func (e *Engine) worktree(ctx context.Context, branch model.Branch) (*git.Repository, error) {
	if branch.Worktree == "" {
		var err error
		if branch, err = e.placeWorktree(ctx, branch); err != nil {
			return nil, err
		}
	}
	if err := e.checkOutAgain(ctx, branch); err != nil {
		return nil, err
	}
	return e.openWorktree(ctx, branch)
}

// openWorktree opens the branch's checkout as it is, which must have the
// branch checked out. One that's gone isn't made again, as worktree makes
// it, so what only reads a branch, as status does, changes nothing.
func (e *Engine) openWorktree(ctx context.Context, branch model.Branch) (*git.Repository, error) {
	if branch.Worktree == "" || !exists(branch.Worktree) {
		return nil, fmt.Errorf("%s has no worktree here", branch.Name)
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

// ChangesHere reports whether what's checked out where the engine was
// opened changes a port: in the commits since it left master, as fetched
// now, or in files edited or added and not committed. Work planned on
// master would leave those out. It's for a checkout dockhand doesn't
// track, a branch a person made with Git or master itself; a tracked
// branch's changes are BranchesChanging's.
func (e *Engine) ChangesHere(ctx context.Context, port string) (bool, error) {
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return false, err
	}
	head, err := e.Repo.Resolve(ctx, "HEAD")
	if err != nil {
		return false, err
	}
	left, err := e.Repo.MergeBase(ctx, head, string(master))
	if err != nil {
		return false, err
	}
	paths, err := e.Repo.ChangedPaths(ctx, left, head)
	if err != nil {
		return false, err
	}
	edited, err := e.Repo.TrackedChanges(ctx)
	if err != nil {
		return false, err
	}
	added, err := e.Repo.Untracked(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(ScopeOf(slices.Concat(paths, edited, added)).PortNames(), port), nil
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

// assessUpstream assesses what upstream's change means for the port an
// update prepared (the assessment design, C): each archive the update
// replaced beside the one that replaces it, in every context that fetches
// them, as gh's source tarball and the prebuilt zip older macOS fetches
// each have their own, read where the port builds there; what the new
// version's Python requirements ask of the ports that provide them, whose
// versions are observed in the tree before the edit and after, as the
// assessment asks; and what its go.mod asks of go.toolchain_min, whether
// or not archives are compared. Not being able to compare is reported,
// never a reason to refuse the update. Nil where nothing was compared or
// said.
func (e *Engine) assessUpstream(ctx context.Context, result preparation.Result, versions sourcecompare.Versions, trees [2]model.Source, compare bool) *model.UpstreamComparison {
	input := assess.Input{Versions: versions}
	input.Base, _ = result.PortBefore(result.Target.Name)
	input.Port, _ = result.PortAfter(result.Target.Name)
	if file, data, err := e.Repo.File(ctx, string(trees[1].Tree), result.Target.Portfile); err == nil && file.Exists {
		input.Portfile = data
	}
	if t := result.GoToolchain; t != nil {
		input.Toolchain = &assess.Toolchain{Required: t.Required, Declared: t.Declared, Outcome: toolchainOutcomes[t.Outcome]}
	}
	problem := ""
	switch {
	case !compare:
	case result.PreviousProblem != "":
		problem = "the current version's archives could not be fetched: " + result.PreviousProblem
	case len(result.Downloads) == 0:
		// A port fetched with git has no archives, so nothing to compare.
		compare = false
	default:
		for _, download := range result.Downloads {
			if !slices.ContainsFunc(result.Pairs, func(pair preparation.ArchivePair) bool { return pair.Next.Name == download.Name }) {
				problem = download.Name + " replaces no archive dockhand could find, so it wasn't compared"
				break
			}
		}
	}
	if compare && problem == "" {
		pairs, err := e.readPairs(ctx, result.Pairs, input.Base, input.Port)
		if err != nil {
			problem = err.Error()
		}
		input.Pairs = pairs
	}
	if problem == "" && len(input.Pairs) > 0 {
		input.Observed = e.observeProviders(ctx, input, trees)
	}
	comparison := assess.Assess(input)
	comparison.Problem = problem
	if !compare && len(comparison.Changes) == 0 {
		return nil
	}
	return &comparison
}

// readings is where readings of upstream's archives are kept; none are
// where the engine wasn't given a place.
func (e *Engine) readings() project.Cache {
	return project.Cache{Directory: e.options.Readings}
}

// toolchainOutcomes are what the editor did about go.toolchain_min, as the
// assessment words them.
var toolchainOutcomes = map[preparation.GoToolchainOutcome]string{
	preparation.GoToolchainCovered: assess.ToolchainCovered, preparation.GoToolchainRaised: assess.ToolchainRaised,
	preparation.GoToolchainUndeclared: assess.ToolchainUndeclared, preparation.GoToolchainByHand: assess.ToolchainByHand,
}

// readPairs reads each pair of archives, each version where its port
// builds in the context that fetches it (the assessment design's step 1):
// a monorepo's Python bindings in bindings/python. A pair a context didn't
// name the port for is read with the update's own. What's read is kept,
// by the archive's content, for an assessment made again.
func (e *Engine) readPairs(ctx context.Context, pairs []preparation.ArchivePair, base, port macports.PortInfo) ([]assess.Pair, error) {
	var read []assess.Pair
	for _, pair := range pairs {
		if pair.Previous.Path == "" || pair.Next.Path == "" {
			return nil, errors.New("the archives were not kept to compare")
		}
		ports := [2]macports.PortInfo{pair.Base, pair.Port}
		for i, fallback := range []macports.PortInfo{base, port} {
			if ports[i].Name == "" {
				ports[i] = fallback
			}
		}
		var readings [2]project.Reading
		for i, archive := range []archives.Download{pair.Previous, pair.Next} {
			reading, err := e.readings().Read(ctx, archive.Path, archive.SHA256, project.Spec{Subdirectory: macports.SourceSubdirectory(ports[i].Options["worksrcdir"])})
			if err != nil {
				return nil, fmt.Errorf("reading %s: %v", path.Base(archive.Path), err)
			}
			readings[i] = reading
		}
		read = append(read, assess.Pair{Archive: pair.Next.Name, Before: readings[0], After: readings[1], Port: pair.Port})
	}
	return read, nil
}

// observeProviders observes what an assessment wants, until it wants
// nothing more: the version of each port that provides a Python
// requirement in question, evaluated in the tree before the edit or after.
// What can't be observed is said as such, which the assessment weighs as
// not knowing.
func (e *Engine) observeProviders(ctx context.Context, input assess.Input, trees [2]model.Source) map[assess.Provider]assess.Observation {
	observed := map[assess.Provider]assess.Observation{}
	input.Observed = observed
	reader, unreadable := e.portReader()
	for {
		wanted := assess.Wanted(input)
		if len(wanted) == 0 {
			return observed
		}
		for _, provider := range wanted {
			if unreadable != nil {
				observed[provider] = assess.Observation{Problem: "couldn't read the ports: " + unreadable.Error()}
				continue
			}
			tree := trees[1]
			if provider.Base {
				tree = trees[0]
			}
			version, directory, err := portVersion(ctx, reader, tree, provider.Port)
			switch {
			case errors.Is(err, ErrNoPort) || errors.Is(err, portindex.ErrNotIndexed):
				observed[provider] = assess.Observation{Absent: true}
			case err != nil:
				observed[provider] = assess.Observation{Problem: err.Error()}
			default:
				observed[provider] = assess.Observation{Version: version, Directory: directory}
			}
		}
	}
}

// portVersion is a port's version in a tree, as MacPorts evaluates it on
// this Mac, and its directory; ErrNoPort, or the index's ErrNotIndexed,
// where the tree has no such port.
func portVersion(ctx context.Context, reader PortReader, source model.Source, name string) (string, string, error) {
	directory, err := reader.Directory(ctx, source, name)
	if err != nil {
		return "", "", err
	}
	ports, err := reader.Ports(ctx, source, directory, model.Environment{}, nil)
	if err != nil {
		return "", "", err
	}
	for _, port := range ports {
		if port.Name == name {
			return port.Version, directory, nil
		}
	}
	return "", "", fmt.Errorf("%w: %s defines no port %s", ErrNoPort, directory, name)
}
