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

// versions lists the MacPorts versions a shim set covers, oldest first
// by MacPorts' own ordering.
func versions(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("shim: %w", err)
	}
	var have []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".tcl"); ok {
			have = append(have, name)
		}
	}
	if len(have) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoShims, dir)
	}
	sort.Slice(have, func(i, j int) bool { return macports.VerCmp(have[i], have[j]) < 0 })
	return have, nil
}

// Newest is the highest MacPorts version this shim set was written and
// verified against.
//
// It is derived from the shims rather than declared beside them,
// because a constant saying the same thing would be a second place to
// update and would start lying the day someone added a shim without
// remembering it. Adding a shim is what raises this number; there is
// nothing else to keep in step.
//
// It answers "the newest MacPorts dockhand knows how to talk to", which
// is not "the newest MacPorts released". An installation newer than
// this still works — Select falls back rather than failing — but it is
// being driven by a shim written for an older one, and that is worth
// saying out loud.
func Newest(fsys fs.FS, dir string) (string, error) {
	have, err := versions(fsys, dir)
	if err != nil {
		return "", err
	}
	return have[len(have)-1], nil
}

// Select returns the shim from dir that fits the given MacPorts
// version, and the version it was named for. An empty version means
// undetermined, which takes the newest shim.
func Select(fsys fs.FS, dir, version string) (script string, named string, err error) {
	have, err := versions(fsys, dir)
	if err != nil {
		return "", "", err
	}

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
