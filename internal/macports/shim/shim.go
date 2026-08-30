// Package shim selects the Tcl shim matching a MacPorts installation.
//
// A shim speaks to MacPorts' own internals — mportopen, ditem_key,
// portfetch's namespace, the ui hooks — and those are not a stable
// public API. What works against one MacPorts may not work against
// another, so shims are kept per version rather than as one script
// hoped to fit every installation.
//
// A shim file is named for the MacPorts version it was written and
// verified against, and is taken to hold for that version and later,
// until a newer shim supersedes it. Select therefore picks the newest
// shim that is not newer than the installation.
//
// Both ends fall back rather than fail, because a shim mismatch is a
// degraded case, not a broken one: an installation whose version could
// not be determined gets the newest shim, and one older than every
// shim gets the oldest. Selection never gates work it might merely
// have made worse.
package shim

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
)

// ErrNoShims reports a shim set with no scripts in it — a build
// problem, not a machine problem.
var ErrNoShims = errors.New("shim: no shims embedded")

// Select returns the shim from dir that fits the given MacPorts
// version, and the version it was named for. An empty version means
// undetermined, which takes the newest shim.
func Select(fsys fs.FS, dir, version string) (script string, named string, err error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return "", "", fmt.Errorf("shim: %w", err)
	}
	var have []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".tcl"); ok {
			have = append(have, name)
		}
	}
	if len(have) == 0 {
		return "", "", fmt.Errorf("%w: %s", ErrNoShims, dir)
	}
	// Oldest first, by MacPorts' own ordering.
	sort.Slice(have, func(i, j int) bool { return macports.VerCmp(have[i], have[j]) < 0 })

	chosen, why := have[len(have)-1], "newest: installation version undetermined"
	if version != "" {
		chosen, why = have[0], "oldest: installation predates every shim"
		for _, v := range have {
			if macports.VerCmp(v, version) <= 0 {
				chosen, why = v, "newest shim not newer than the installation"
			}
		}
	}
	slog.Debug("shim selected", "shim", chosen, "macports", version, "why", why)

	b, err := fs.ReadFile(fsys, path.Join(dir, chosen+".tcl"))
	if err != nil {
		return "", "", fmt.Errorf("shim: %w", err)
	}
	return string(b), chosen, nil
}
