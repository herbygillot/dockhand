package change

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// ErrNotAFile is Snapshot refusing something under a portdir that is not
// an ordinary file — a symlink, a socket, a device node. A commit records
// blobs, and a walk that read a symlink's target as content would commit
// the pointed-at bytes under the pointer's name, silently.
var ErrNotAFile = errors.New("change: a portdir holds something that is not an ordinary file")

// Snapshot commits a portdir's working tree as it sits — Prepared from
// the files, Identify over HEAD, CommitTree with HEAD as parent — and
// returns the sha and content. A PURE OBJECT WRITER like Commit, and for
// the same reason: the pin that keeps this commit alive is AdoptIn's
// line in the batch that records it, so a crash here leaves garbage
// (statestore.PruneExpire) and never a pin with no record.
//
// It refuses a portdir outside any repository: nothing here can record
// what it starts, and that is the band-40 refusal on `verify <portdir>`.
// `exec` OUTSIDE A REPOSITORY remains the untracked road for an ad-hoc
// guest — there is no store to record a lease in and no reconciler that
// could seize it. Inside a repository it records a lease like every
// other guest a dockhand verb holds (ruled 2026-09-07).
//
// IT SNAPSHOTS REMOVALS TOO, which is the half a walk of the filesystem
// alone cannot see. A person who deleted a stale patch and then asked
// `verify <portdir>` means the deletion; a snapshot built only from the
// files that are THERE would commit HEAD's copy of the deleted file
// back, and the guest would build a portdir the person does not have. So
// the portdir's tree at HEAD is read beside the walk and every path it
// holds that the working tree no longer does becomes a File with Delete
// set — which is exactly what File.Delete is for, and the one place the
// design has to express it.
func Snapshot(ctx context.Context, repo *git.Repo, id record.ChangeID, portdir string) (sha string, content record.ContentID, err error) {
	if repo == nil {
		return "", "", fmt.Errorf("%w: a snapshot needs a repository", git.ErrNotARepo)
	}
	if id == "" {
		return "", "", fmt.Errorf("%w: a snapshot needs the change id its pin will be named for", ErrIncomplete)
	}
	// RelPath is the refusal: a portdir outside this repository is a
	// portdir nothing here can record, and the error names the boundary
	// rather than this function's own opinion of it.
	rel, err := repo.RelPath(portdir)
	if err != nil {
		return "", "", err
	}
	head, err := repo.RevParse(ctx, "HEAD^{commit}")
	if err != nil {
		return "", "", err
	}
	files, err := walkPortdir(portdir)
	if err != nil {
		return "", "", err
	}
	gone, err := removedSince(ctx, repo, head, rel, files)
	if err != nil {
		return "", "", err
	}
	p := Prepared{Portdir: TreePath(rel), Files: append(files, gone...)}
	content, err = p.Identify(ctx, repo, head)
	if err != nil {
		return "", "", err
	}
	// The message is this function's own and not Message's: a snapshot has
	// no plan, no summary and no ticket, and what a reader of `git log`
	// needs from it is which portdir was captured and that a person's
	// working tree — not a plan — is what it holds.
	sha, err = repo.CommitTree(ctx, string(content), []string{head},
		fmt.Sprintf("%s: verify the working tree\n\nSnapshot of %s as it sits, taken so the verification has a commit to build.\n", rel, rel))
	if err != nil {
		return "", "", err
	}
	return sha, content, nil
}

// walkPortdir reads every ordinary file under a portdir as a File, at
// its portdir-relative slash-separated path, sorted so two walks of one
// directory produce one tree and one content id.
//
// Directories are descended and nothing else is: a symlink or any other
// irregular entry is ErrNotAFile rather than something read for its
// bytes. The mode is left at the zero value, which File.Mode documents
// as "whatever the tree already says, or 0644 for a new path" — the
// only answer the object writer can honestly give until git.File carries
// a mode.
func walkPortdir(dir string) ([]File, error) {
	var files []File
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%w: %s is %s", ErrNotAFile, path, d.Type())
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		files = append(files, File{Path: filepath.ToSlash(rel), Content: content})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// removedSince is the other half of a snapshot: the paths the portdir's
// tree at rev still carries that the working tree no longer has, as
// Files with Delete set.
//
// A portdir that does not exist at rev at all — a brand new port — has
// nothing to remove, and git.ErrNoObject is that answer rather than a
// failure.
func removedSince(ctx context.Context, repo *git.Repo, rev, portdir string, have []File) ([]File, error) {
	batch, err := repo.CatFile(ctx)
	if err != nil {
		return nil, err
	}
	// The read path's close: the session is being abandoned, and what it
	// would report is that the caller stopped reading.
	defer batch.Close() //nolint:errcheck // read-path close; nothing was written
	root, err := batch.Tree(rev + ":" + portdir)
	if errors.Is(err, git.ErrNoObject) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(have))
	for _, f := range have {
		present[f.Path] = true
	}
	var gone []File
	// A portdir is a Portfile and a files/ directory beside it, so the
	// walk is shallow in practice and iterative rather than recursive so
	// that a tree somebody nested deeply cannot recurse this process.
	type at struct {
		prefix  string
		entries []git.TreeEntry
	}
	for todo := []at{{entries: root}}; len(todo) > 0; {
		here := todo[0]
		todo = todo[1:]
		for _, e := range here.entries {
			path := e.Name
			if here.prefix != "" {
				path = here.prefix + "/" + e.Name
			}
			if e.Dir() {
				sub, terr := batch.Tree(e.OID)
				if terr != nil {
					return nil, terr
				}
				todo = append(todo, at{prefix: path, entries: sub})
				continue
			}
			if !present[path] {
				gone = append(gone, File{Path: path, Delete: true})
			}
		}
	}
	slices.SortFunc(gone, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return gone, nil
}
