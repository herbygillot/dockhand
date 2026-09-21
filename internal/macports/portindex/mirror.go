package portindex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/progress"
)

// Mirror enables the MacPorts mirror's PortIndex as a bootstrap seed for a
// cache with no usable generation, in place of a full pass of several
// minutes. The mirror's index carries no commit stamp, so it is bracketed:
// its Last-Modified less a margin names a master commit the snapshot cannot
// predate, and every path changed from that commit to the target tree is
// re-indexed through the incremental path, which makes the generation
// current for the target tree. The target commit must itself be no older
// than the snapshot, or entries changed after it would survive. The result
// is recorded with its provenance and never strict: the mirror's indexer and
// Base are not the local ones, so the executable identity cannot vouch for it.
type Mirror struct {
	HTTP *http.Client
	// URL overrides the platform's default mirror index.
	URL string
	// Margin is subtracted from the mirror's Last-Modified to bracket the
	// master commit; two hours when unset, since master gains tens of ports
	// an hour and the mirror's index is minutes behind its build.
	Margin time.Duration
}

const defaultMirrorMargin = 2 * time.Hour

// mirrorProvenance records what a mirror-seeded generation was built from.
type mirrorProvenance struct {
	URL          string    `json:"url"`
	LastModified time.Time `json:"last_modified"`
	// Commit is the master commit the bracket chose; the paths changed from
	// it to the tree were re-indexed.
	Commit string `json:"commit"`
	Margin string `json:"margin"`
}

// mirrorSeed downloads the mirror's index and brackets it below the target
// commit. It returns the seed directory, the paths to re-index, and the
// provenance, or an empty seed when the mirror cannot seed this tree: no
// Last-Modified, no bracketing commit in the target's history, a target
// older than the snapshot, or shared resources changed since the bracket.
func (c *cache) mirrorSeed(ctx context.Context, repo *git.Repository, commit, tree string) (string, []string, *mirrorProvenance, error) {
	mirror := c.config.Mirror
	address := mirror.URL
	if address == "" {
		var err error
		if address, err = DefaultMirrorURL(c.platform); err != nil {
			progress.VerboseReport(ctx, "Mirror index unavailable for bootstrap: %v", err)
			return "", nil, nil, nil
		}
	}
	margin := mirror.Margin
	if margin <= 0 {
		margin = defaultMirrorMargin
	}
	committed, err := repo.CommitTime(ctx, commit)
	if err != nil {
		return "", nil, nil, err
	}
	progress.Report(ctx, "Fetching the mirror's PortIndex to seed the cache")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", nil, nil, err
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	request.Header.Set("Accept-Encoding", "identity")
	response, err := fetch.Open(mirror.HTTP, request, maxPortIndexBytes)
	if err != nil {
		return "", nil, nil, fmt.Errorf("portindex: fetching the mirror index: %w", err)
	}
	defer response.Body.Close()
	lastModified, err := http.ParseTime(response.Header.Get("Last-Modified"))
	if err != nil {
		progress.VerboseReport(ctx, "Mirror index has no Last-Modified to bracket; indexing in full")
		return "", nil, nil, nil
	}
	if committed.Before(lastModified) {
		progress.VerboseReport(ctx, "Source commit %s predates the mirror index of %s; indexing in full", commit[:12], lastModified.UTC().Format(time.RFC3339))
		return "", nil, nil, nil
	}
	bracket, err := repo.CommitBefore(ctx, commit, lastModified.Add(-margin))
	if err != nil {
		return "", nil, nil, err
	}
	if bracket == "" {
		progress.VerboseReport(ctx, "No commit below the mirror index's bracket; indexing in full")
		return "", nil, nil, nil
	}
	trees, err := repo.CommitTrees(ctx, []string{bracket})
	if err != nil {
		return "", nil, nil, err
	}
	changed, err := repo.ChangedPaths(ctx, trees[bracket], tree)
	if err != nil {
		return "", nil, nil, err
	}
	if requiresFullIndex(changed) {
		progress.VerboseReport(ctx, "Shared resources changed since the mirror index's bracket %s; indexing in full", bracket[:12])
		return "", nil, nil, nil
	}
	seed, err := os.MkdirTemp(c.directory, ".mirror-seed-")
	if err != nil {
		return "", nil, nil, err
	}
	file, err := os.OpenFile(filepath.Join(seed, portIndexName), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		os.RemoveAll(seed)
		return "", nil, nil, err
	}
	size, err := io.Copy(file, response.Body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil && size == 0 {
		err = fmt.Errorf("portindex: the mirror index is empty")
	}
	if err != nil {
		os.RemoveAll(seed)
		return "", nil, nil, err
	}
	progress.VerboseReport(ctx, "Seeding the PortIndex from the mirror index of %s, re-indexing %d paths changed since master %s", lastModified.UTC().Format(time.RFC3339), len(changed), bracket[:12])
	return seed, changed, &mirrorProvenance{URL: address, LastModified: lastModified.UTC(), Commit: bracket, Margin: margin.String()}, nil
}
