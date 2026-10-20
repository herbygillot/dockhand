package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// CaptureMode says which files a check captures (Design v3 §7).
type CaptureMode string

const (
	// CaptureWorking captures the tracked files as they are on disk,
	// staged or not.
	CaptureWorking CaptureMode = "working"
	// CaptureStaged captures the index.
	CaptureStaged CaptureMode = "staged"
	// CaptureHead captures the committed tip.
	CaptureHead CaptureMode = "head"
	// CaptureAuto captures what a command asking for no files in
	// particular means (autoMode): ship's, and check's without --head,
	// --staged, or --working-tree.
	CaptureAuto CaptureMode = "auto"
)

// CaptureRequest asks for a revision of a branch to check.
type CaptureRequest struct {
	Branch model.Branch
	Mode   CaptureMode
	// Include adds untracked files to the capture without staging them.
	Include []string
	// Plan records nothing, for check --plan: an earlier revision with the
	// same files is still found, and new files are a snapshot numbered
	// only when a check records it.
	Plan bool
	// Named is a branch a flag named, rather than the one checked out
	// where the command runs, which CaptureAuto reads.
	Named bool
}

// autoMode is what CaptureAuto captures: the working files of the branch
// checked out here, or of one a flag named that has no commits of its own,
// whose working files are all there is of it (the txt run's finding 2);
// the committed tip of a named branch whose worktree has no edits; and a
// refusal where it has both, since neither is plainly meant. Files to
// include are working files, which a committed tip can't take: check -b
// net-snmp --include, as the untracked patch's refusal says, was refused
// for the head (field testing, check-469).
func (e *Engine) autoMode(ctx context.Context, branch model.Branch, named, including bool) (CaptureMode, error) {
	if !named {
		return CaptureWorking, nil
	}
	if here, err := e.Current(ctx); err == nil && here.ID == branch.ID {
		return CaptureWorking, nil
	}
	edits, err := e.Edited(ctx, branch)
	if err != nil {
		return "", err
	}
	if len(edits) == 0 && !including {
		return CaptureHead, nil
	}
	if len(edits) == 0 {
		return CaptureWorking, nil
	}
	commits, err := e.CommitsAhead(ctx, branch)
	if err != nil {
		return "", err
	}
	if commits == 0 {
		return CaptureWorking, nil
	}
	return "", fmt.Errorf("%s's worktree has edits (%s); choose --head for the committed tip or --working-tree for the files", branch.ShortName(), strings.Join(edits, ", "))
}

// Capture is the revision a check covers.
type Capture struct {
	Revision model.Revision
	// Reused is true when an earlier revision already held these files.
	Reused bool
	// Untracked lists the files left out because Git does not track them.
	Untracked []string
	// NewPorts are the directories among them whose Portfile is left out:
	// a port written by hand, which the worktree's sparse checkout keeps
	// git add from taking (the Vx port's field testing, 2026-10-03).
	NewPorts []string
}

// Describe names the revision for people: "snapshot 3" or "commit 7e3f1a2".
func Describe(revision model.Revision) string {
	if revision.Kind == model.RevisionSnapshot {
		if revision.Snapshot == 0 {
			// A plan's capture, which only a check records and numbers.
			return "a new snapshot"
		}
		return fmt.Sprintf("snapshot %d", revision.Snapshot)
	}
	return "commit " + revision.Source.Commit.Short()
}

// Capture records what a check will build: the committed tip when the
// files are exactly it, otherwise a numbered snapshot of the files. A
// capture whose files an earlier revision already holds reuses it, so
// checking twice without an edit checks the same snapshot.
func (e *Engine) Capture(ctx context.Context, request CaptureRequest) (Capture, error) {
	branch := request.Branch
	if request.Mode == CaptureAuto {
		mode, err := e.autoMode(ctx, branch, request.Named, len(request.Include) > 0)
		if err != nil {
			return Capture{}, err
		}
		request.Mode = mode
	}
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return Capture{}, err
	}
	conflicts, err := worktree.Conflicts(ctx)
	if err != nil {
		return Capture{}, err
	}
	if len(conflicts) > 0 {
		return Capture{}, fmt.Errorf("%s has unresolved conflicts in %s; resolve them before checking", branch.Name, listPaths(conflicts))
	}
	head, err := worktree.Resolve(ctx, "HEAD^{commit}")
	if err != nil {
		return Capture{}, err
	}
	trees, err := worktree.CommitTrees(ctx, []string{head})
	if err != nil {
		return Capture{}, err
	}
	var capture Capture
	if capture.Untracked, err = worktree.Untracked(ctx); err != nil {
		return Capture{}, err
	}
	for _, path := range request.Include {
		if slices.Contains(capture.Untracked, path) {
			continue
		}
		// One Git ignores is left out as git add leaves it, and said so,
		// with the command that names the rule (the person, 2026-10-04:
		// what gathers files abides by .gitignore as Git does).
		if ignored, err := worktree.Ignored(ctx, path); err == nil && len(ignored) > 0 {
			return Capture{}, fmt.Errorf("--include %s: Git ignores it, so a check leaves it out as git add would; git check-ignore -v %s names the rule", path, path)
		}
		return Capture{}, fmt.Errorf("--include %s: it is not an untracked file here", path)
	}
	// An editor's swap or backup file isn't one a person leaves out of a
	// check: it isn't listed as left out (field testing's py-mlx-vlm,
	// 2026-10-04: office/tasksh/.Portfile.swp), though --include could
	// still take it. A .gitignore that ignores it keeps it out of git add.
	capture.Untracked = slices.DeleteFunc(capture.Untracked, func(p string) bool { return slices.Contains(request.Include, p) || editorFile(p) })
	for _, path := range capture.Untracked {
		if directory, ok := macports.PortDirectoryOf(path); ok && path == directory+"/Portfile" {
			capture.NewPorts = append(capture.NewPorts, directory)
		}
	}

	var tree string
	switch request.Mode {
	case CaptureHead:
		if len(request.Include) > 0 {
			return Capture{}, errors.New("--include adds files to captured working files, not to the committed head")
		}
		tree = trees[head]
	case CaptureStaged:
		tree, err = worktree.IndexTree(ctx)
	default:
		_, tree, err = worktree.WorkingTree(ctx)
	}
	if err != nil {
		return Capture{}, err
	}
	if len(request.Include) > 0 {
		if tree, err = worktree.WithFiles(ctx, tree, request.Include); err != nil {
			return Capture{}, err
		}
	}
	// The capture stands only if the files did not move while it read them:
	// read again, the files --include adds too, the trees must agree.
	if request.Mode == CaptureWorking || request.Mode == "" {
		if e.betweenReads != nil {
			e.betweenReads()
		}
		_, again, err := worktree.WorkingTree(ctx)
		if err == nil && len(request.Include) > 0 {
			again, err = worktree.WithFiles(ctx, again, request.Include)
		}
		if err != nil {
			return Capture{}, err
		}
		if again != tree {
			return Capture{}, errors.New("the files changed while they were read; check again once they settle")
		}
	}

	revision := model.Revision{Branch: branch.ID, Source: model.Source{Tree: model.ObjectID(tree), Base: branch.Base}, Head: model.ObjectID(head), CreatedAt: e.now()}
	if tree == trees[head] {
		revision.Kind, revision.Source.Commit = model.RevisionCommit, model.ObjectID(head)
	} else {
		revision.Kind = model.RevisionSnapshot
	}
	found := func(existing []model.Revision) bool {
		for _, earlier := range slices.Backward(existing) {
			if earlier.SameTree(revision) && earlier.Kind == revision.Kind && earlier.Source.Commit == revision.Source.Commit {
				capture.Revision, capture.Reused = earlier, true
				return true
			}
		}
		return false
	}
	if request.Plan {
		err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
			existing, err := r.Revisions(branch.ID)
			if err == nil && !found(existing) {
				// An ID of its own, as the plan made of it has, though
				// neither is recorded.
				revision.ID = model.RevisionID(store.NewID("rev"))
				capture.Revision = revision
			}
			return err
		})
		return capture, err
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		existing, err := tx.Revisions(branch.ID)
		if err != nil {
			return err
		}
		if found(existing) {
			return nil
		}
		revision.ID = model.RevisionID(store.NewID("rev"))
		if revision.Kind == model.RevisionSnapshot {
			if revision.Snapshot, err = tx.NextSnapshot(branch.ID); err != nil {
				return err
			}
		}
		capture.Revision = revision
		return tx.AddRevision(revision)
	})
	return capture, err
}

// CommitsAhead is how many commits a branch has beyond its base; none for
// a branch whose work is all in its working files.
func (e *Engine) CommitsAhead(ctx context.Context, branch model.Branch) (int, error) {
	head, _, err := e.Repo.Branch(ctx, branch.Name)
	if err != nil {
		return 0, err
	}
	return e.Repo.CountCommits(ctx, string(branch.Base), head)
}

// Edited lists the tracked files a branch's worktree has changed and not
// committed.
func (e *Engine) Edited(ctx context.Context, branch model.Branch) ([]string, error) {
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return nil, err
	}
	return worktree.TrackedChanges(ctx)
}

// editorFile reports an editor's swap, lock, or backup file: vim's
// .name.swp, .swo, and .swn, Emacs's name~ and .#name, and a merge's
// .orig.
func editorFile(name string) bool {
	base := path.Base(name)
	switch {
	case strings.HasSuffix(base, "~"), strings.HasPrefix(base, ".#"), strings.HasSuffix(base, ".orig"):
		return true
	case strings.HasPrefix(base, "."):
		for _, suffix := range []string{".swp", ".swo", ".swn"} {
			if strings.HasSuffix(base, suffix) {
				return true
			}
		}
	}
	return false
}

// UntrackedInChanged are the untracked files a capture left out that sit in
// a directory the plan changes: a new patch in a port's files/, which the
// check builds without, and fails or passes for the wrong reason (Codex's
// gcc15 group update, 2026-10-07). They're said before any VM boots, with
// what takes each in.
func UntrackedInChanged(capture Capture, plan model.Plan) []string {
	var directories []string
	for _, target := range plan.Targets {
		if target.Role == model.Changed && target.Directory != "" && !slices.Contains(directories, target.Directory) {
			directories = append(directories, target.Directory)
		}
	}
	var found []string
	for _, path := range capture.Untracked {
		if slices.ContainsFunc(directories, func(d string) bool { return strings.HasPrefix(path, d+"/") }) {
			found = append(found, path)
		}
	}
	return found
}

// UntrackedPatches are the untracked files a changed port's Portfile names
// in its patchfiles, read from tree, by port directory: the patch a
// person wrote and didn't git add, which the Portfile's commit needs, as
// net-snmp's patch-swinst_darwin-declaration.diff did, where tidy
// committed the patchfiles line without it and the check 404'd fetching it
// (field testing, check-469). untracked is the worktree's list; a name is
// read in the port's files/, where MacPorts looks for it.
func (e *Engine) UntrackedPatches(ctx context.Context, tree string, directories, untracked []string) (map[string][]string, error) {
	return untrackedPatches(ctx, e.Repo, tree, directories, untracked)
}

func untrackedPatches(ctx context.Context, repo *git.Repository, tree string, directories, untracked []string) (map[string][]string, error) {
	found := map[string][]string{}
	for _, directory := range directories {
		file, data, err := repo.File(ctx, tree, directory+"/Portfile")
		if err != nil {
			return nil, err
		}
		if !file.Exists {
			continue
		}
		for _, name := range portfile.PatchfileNames(data) {
			path := directory + "/files/" + name
			if slices.Contains(untracked, path) && !slices.Contains(found[directory], path) {
				found[directory] = append(found[directory], path)
			}
		}
	}
	return found, nil
}

// ignoredPatches are the files a changed port's Portfile names in its
// patchfiles, read from tree, that Git ignores, by port directory: on
// disk in the worktree, neither in tree nor untracked. git add leaves
// them out, and so do a commit and a check.
func ignoredPatches(ctx context.Context, worktree *git.Repository, tree string, directories, untracked []string) (map[string][]string, error) {
	var named []string
	for _, directory := range directories {
		file, data, err := worktree.File(ctx, tree, directory+"/Portfile")
		if err != nil {
			return nil, err
		}
		if !file.Exists {
			continue
		}
		for _, name := range portfile.PatchfileNames(data) {
			path := directory + "/files/" + name
			if slices.Contains(untracked, path) || slices.Contains(named, path) || !fs.ValidPath(path) {
				continue
			}
			if inTree, _, err := worktree.File(ctx, tree, path); err != nil {
				return nil, err
			} else if inTree.Exists {
				continue
			}
			if info, err := os.Lstat(filepath.Join(worktree.Root, filepath.FromSlash(path))); err == nil && info.Mode().IsRegular() {
				named = append(named, path)
			}
		}
	}
	ignored, err := worktree.Ignored(ctx, named...)
	if err != nil {
		return nil, err
	}
	found := map[string][]string{}
	for _, path := range ignored {
		directory, _ := macports.PortDirectoryOf(path)
		found[directory] = append(found[directory], path)
	}
	return found, nil
}

// UntrackedPatchError is a check refused before any VM starts, since a
// changed port's patchfiles names a file the capture doesn't hold: one
// Git doesn't track, or one it ignores, which --include can't take.
type UntrackedPatchError struct {
	Port string
	Path string
	// Ignored is true where Git ignores it.
	Ignored bool
}

func (u *UntrackedPatchError) Error() string {
	if u.Ignored {
		return fmt.Sprintf("%s's patchfiles names %s, which Git ignores, so neither a commit nor a check carries it; git check-ignore -v %s names the rule", u.Port, u.Path, u.Path)
	}
	return fmt.Sprintf("%s's patchfiles names %s, which isn't tracked: git add it, or check --include it", u.Port, u.Path)
}
