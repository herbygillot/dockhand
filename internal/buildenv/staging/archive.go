package staging

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
)

type Request struct {
	AdditionalTargets []model.Target
	Source            model.Source
	Target            model.Target
	Platform          model.Platform
	// Index supplies the staged index of the tree the archive packs.
	Index portindex.Source
}

// Archive stages the frozen tree and index, then atomically installs the archive.
// Payload contains provider-owned top-level files outside the reserved ports tree.
func Archive(ctx context.Context, repo *git.Repository, workspaces *workspace.Registry, request Request, destination string, payload map[string][]byte, client *http.Client) (err error) {
	for name := range payload {
		if !fs.ValidPath(name) || name == "." || name == "ports" || filepath.Base(name) != name {
			return fmt.Errorf("staging: invalid provider payload name %q", name)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if repo == nil {
		return fmt.Errorf("staging: source repository is required")
	}
	if request.Source.Commit != "" {
		trees, err := repo.CommitTrees(ctx, []string{string(request.Source.Commit)})
		if err != nil {
			return err
		}
		if trees[string(request.Source.Commit)] != string(request.Source.Tree) {
			return fmt.Errorf("staging: source commit and tree disagree")
		}
	}
	progress.DebugReport(ctx, "Materializing committed source for verification")
	// The archive packs the whole tree, from the workspace dependent
	// discovery shares when it worked on the same prepared tree.
	files, release, err := workspaces.Acquire(ctx, repo, request.Source)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	if err = files.EnsureAll(ctx); err != nil {
		return err
	}
	root := files.Root()
	into, err := files.Tree(request.Platform)
	if err != nil {
		return err
	}
	if request.Index == nil {
		return fmt.Errorf("staging: an index source is required")
	}
	index, err := request.Index.Index(ctx, into)
	if err != nil {
		return err
	}
	for _, target := range append([]model.Target{request.Target}, request.AdditionalTargets...) {
		if err = requireIndexedTarget(index, target); err != nil {
			return err
		}
	}
	progress.DebugReport(ctx, "Packing source and verification inputs")
	return atomicfile.Create(destination, 0600, func(temp *os.File) error {
		return packSource(ctx, root, index.Files(), payload, temp)
	})
}

// packSource writes the snapshot, the index staged for its release, and
// the provider payload as one tar stream. The index is packed from where
// it was staged, not from the root, which another release staging the
// same revision at once shares, and installs its own index into.
func packSource(ctx context.Context, root string, index []string, payload map[string][]byte, temp *os.File) error {
	output := tar.NewWriter(temp)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.Dir(rel) == "." && portindex.IsIndexFile(rel) {
			return nil
		}
		return packFile(output, path, filepath.ToSlash(filepath.Join("ports", rel)), d)
	})
	for _, path := range index {
		if err != nil {
			break
		}
		var info fs.FileInfo
		if info, err = os.Stat(path); err == nil {
			err = packFile(output, path, "ports/"+filepath.Base(path), fs.FileInfoToDirEntry(info))
		}
	}
	if err == nil {
		for name, data := range payload {
			if err = output.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data))}); err != nil {
				break
			}
			if _, err = output.Write(data); err != nil {
				break
			}
		}
	}
	closeErr := output.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = ctx.Err()
	}
	return err

}

// packFile writes one file or directory of the snapshot under name.
func packFile(output *tar.Writer, path, name string, d fs.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return err
	}
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		link, err = os.Readlink(path)
		if err != nil {
			return err
		}
	}
	header, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	header.Name = name
	header.Uid = 0
	header.Gid = 0
	header.Uname = "root"
	header.Gname = "wheel"
	if info.IsDir() || info.Mode()&0111 != 0 {
		header.Mode = 0755
	} else {
		header.Mode = 0644
	}
	if err = output.WriteHeader(header); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func requireIndexedTarget(index *portindex.Index, target model.Target) error {
	entry, err := index.Lookup(target.Name)
	if err != nil {
		return fmt.Errorf("staging: selected target %s is not indexed: %w", target.Name, err)
	}
	if entry.Portdir != filepath.ToSlash(filepath.Dir(target.Portfile)) {
		return fmt.Errorf("staging: indexed target %s belongs to %s, expected %s", target.Name, entry.Portdir, target.Portfile)
	}
	return nil
}
