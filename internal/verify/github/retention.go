package github

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/ledger"
)

var _ verify.LogCachePruner = (*Provider)(nil)

// PruneLogCache relies on workflow's terminal-job eligibility decision and locks
// the request against downloads. Remote run identity and database evidence remain.
func (p *Provider) PruneLogCache(ctx context.Context, run record.ProviderRun, before time.Time, dry bool) (bool, error) {
	if run.Provider != verify.ProviderGitHub || run.RequestID == "" || p.State == nil || !filepath.IsAbs(p.Directory) {
		return false, state.ErrInvalid
	}
	pool, err := p.State.ProviderPool(ctx, verify.ProviderGitHub)
	if err != nil {
		return false, err
	}
	if pool.Directory != p.Directory {
		return false, state.ErrConflict
	}
	// Identity is checked before the lock: a run that is not the recorded
	// one is refused whether or not its cache is busy or already gone.
	if _, _, _, err := p.execution(ctx, run); err != nil {
		return false, err
	}
	// The request lock exists once a log was downloaded; without it there is
	// no cache to prune. Held, a download is in progress and the cache is
	// left for the next sweep, which the caller is told. A cache downloaded
	// before the ledger kept its locks under locks/ has its lock beside it,
	// and nothing takes the new one for a finished request.
	lock, err := filelock.TryExisting(ctx, ledger.LockPath(pool, run.RequestID), filelock.Exclusive)
	if errors.Is(err, os.ErrNotExist) {
		lock, err = filelock.TryExisting(ctx, filelock.Path(p.Directory, string(run.RequestID)), filelock.Exclusive)
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if errors.Is(err, filelock.ErrBusy) {
		return false, fmt.Errorf("%w: request %s", verify.ErrCacheBusy, run.RequestID)
	}
	if err != nil {
		return false, err
	}
	defer lock.Close()
	root, err := os.OpenRoot(p.Directory)
	if err != nil {
		return false, err
	}
	defer root.Close()
	names, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return false, err
	}
	prefix := record.Digest([]byte(run.RequestID)) + ".log"
	var remove []string
	for _, entry := range names {
		name := entry.Name()
		if name != prefix {
			suffix, ok := strings.CutPrefix(name, prefix+".job-")
			id, parseErr := strconv.ParseInt(suffix, 10, 64)
			if !ok || parseErr != nil || id <= 0 {
				continue
			}
		}
		info, err := entry.Info()
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return false, fmt.Errorf("github verification: unexpected log cache type: %s", name)
		}
		if info.ModTime().After(before) {
			return false, nil
		}
		remove = append(remove, name)
	}
	for _, name := range remove {
		if err := ctx.Err(); err != nil {
			return len(remove) > 0, err
		}
		if !dry {
			if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
				return true, err
			}
		}
	}
	return len(remove) > 0, nil
}
