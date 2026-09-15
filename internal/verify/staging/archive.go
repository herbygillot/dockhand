// Package staging prepares immutable indexed source archives for verification providers.
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

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

type Request struct {
	Source   record.Source
	Target   record.Target
	Platform record.Platform
	Index    portindex.Config
}

// Archive stages the frozen tree and index, then atomically installs the archive.
// Payload contains provider-owned top-level files outside the reserved ports tree.
func Archive(ctx context.Context, repo *git.Repository, request Request, destination string, payload map[string][]byte, client *http.Client) (err error) {
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
	progress.Report(ctx, "Materializing committed source for verification")
	snapshot, err := repo.Materialize(ctx, string(request.Source.Tree))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, snapshot.Close()) }()
	if err = portindex.Stage(ctx, repo, request.Source, request.Platform, request.Index, snapshot.Root, client); err != nil {
		return err
	}
	if err = requireIndexedTarget(snapshot.Root, request.Target); err != nil {
		return err
	}
	progress.Report(ctx, "Packing source and verification inputs")
	temp, err := os.CreateTemp(filepath.Dir(destination), ".input-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	output := tar.NewWriter(temp)
	err = filepath.WalkDir(snapshot.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(snapshot.Root, path)
		if err != nil {
			return err
		}
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
		header.Name = filepath.ToSlash(filepath.Join("ports", rel))
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
	})
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
		err = temp.Sync()
	}
	closeErr = temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temp.Name(), destination)

}

func requireIndexedTarget(root string, target record.Target) error {
	index, err := portindex.Open(root)
	if err != nil {
		return err
	}
	entry, err := index.Lookup(target.Name)
	if err != nil {
		return fmt.Errorf("staging: selected target %s is not indexed: %w", target.Name, err)
	}
	if entry.Portdir != filepath.ToSlash(filepath.Dir(target.Portfile)) {
		return fmt.Errorf("staging: indexed target %s belongs to %s, expected %s", target.Name, entry.Portdir, target.Portfile)
	}
	return nil
}
