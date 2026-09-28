package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// ArchiveDirectory is where the checkout's kept archives are (decision
// 28), beside the database: each file named by the hex of its sha256. Each
// checkout the database serves keeps its own, so cleaning one up never
// weighs another's.
func (e *Engine) ArchiveDirectory() string {
	return filepath.Join(filepath.Dir(e.options.Database), "archives", string(e.Repository))
}

// archivePath is where a kept archive's file is.
func (e *Engine) archivePath(digest string) string {
	return filepath.Join(e.ArchiveDirectory(), strings.TrimPrefix(digest, "sha256:"))
}

// keptArchive is an archive kept by its digest, and whether it is: its
// record and its whole file both there.
func (e *Engine) keptArchive(ctx context.Context, digest string) (model.Archive, bool, error) {
	var archive model.Archive
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		archive, err = r.Archive(digest)
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		return archive, false, nil
	}
	if err != nil {
		return archive, false, err
	}
	info, err := os.Stat(e.archivePath(digest))
	return archive, err == nil && info.Size() == archive.Size, nil
}

// keepArchive keeps an archive its build reported by digest: fetch writes
// it into the store, where it is checked against the digest, made durable,
// and only then recorded, which makes it ready for dependents (decision
// 44). One kept already isn't fetched again.
func (e *Engine) keepArchive(ctx context.Context, archive model.Archive, fetch func(path string) error) error {
	if !strings.HasPrefix(archive.Digest, "sha256:") || !model.ValidArchiveName(archive.Name) {
		return fmt.Errorf("an archive named %q with digest %q can't be kept", archive.Name, archive.Digest)
	}
	if _, kept, err := e.keptArchive(ctx, archive.Digest); err != nil || kept {
		return err
	}
	directory := e.ArchiveDirectory()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	incoming, err := os.MkdirTemp(directory, ".incoming-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(incoming)
	fetched := filepath.Join(incoming, archive.Name)
	if err := fetch(fetched); err != nil {
		return fmt.Errorf("fetching %s: %w", archive.Name, err)
	}
	size, digest, err := sha256File(fetched)
	if err != nil {
		return err
	}
	if digest != archive.Digest {
		return fmt.Errorf("%s arrived as %s, not the %s its build reported", archive.Name, digest, archive.Digest)
	}
	if err := atomicfile.Place(fetched, e.archivePath(archive.Digest)); err != nil {
		return err
	}
	archive.Size, archive.KeptAt = size, e.now()
	return e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.KeepArchive(archive) })
}

// pruneArchives forgets the archives kept before a time that no live
// result names (store.Tx.PruneArchives), and removes their files, then
// what no record names that is older than it: a fetch that didn't
// finish, or a file whose record never followed. It returns what it
// forgot, and what stays kept.
func (e *Engine) pruneArchives(ctx context.Context, before time.Time) (removed, kept []model.Archive, err error) {
	if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		var err error
		if removed, err = tx.PruneArchives(before); err != nil {
			return err
		}
		kept, err = tx.Archives()
		return err
	}); err != nil {
		return nil, nil, err
	}
	var errs []error
	for _, archive := range removed {
		if err := os.Remove(e.archivePath(archive.Digest)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	directory := e.ArchiveDirectory()
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return removed, kept, errors.Join(errs...)
	}
	if err != nil {
		return removed, kept, errors.Join(append(errs, err)...)
	}
	named := map[string]bool{}
	for _, archive := range kept {
		named[strings.TrimPrefix(archive.Digest, "sha256:")] = true
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || named[entry.Name()] || !info.ModTime().Before(before) {
			continue
		}
		errs = append(errs, os.RemoveAll(filepath.Join(directory, entry.Name())))
	}
	return removed, kept, errors.Join(errs...)
}

// archiveBytes words the size of archives: 820 MB, 1.4 GB.
func archiveBytes(archives []model.Archive) string {
	var n int64
	for _, archive := range archives {
		n += archive.Size
	}
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%.0f MB", math.Ceil(float64(n)/(1<<20)))
}

// sha256File is a file's size and digest, sha256:<hex>.
func sha256File(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return size, "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// Keep keeps a target's archive, by the digest its passed result reported
// (buildenv.Build).
func (b *build) Keep(target model.TargetID, name string, fetch func(path string) error) error {
	result, ok := b.results[target]
	if !ok || result.Outcome != model.OutcomePassed || result.Archive == "" {
		return fmt.Errorf("%s has no passed result naming an archive to keep", target)
	}
	return b.d.e.keepArchive(b.ctx, model.Archive{Digest: result.Archive, Name: name}, fetch)
}
