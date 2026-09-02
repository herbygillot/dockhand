package cmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The release flags. Every verb that names a macOS release spells it
// the same way — a name, a version, and where a set makes sense, "all"
// — and each used to read it on its own. This file is that vocabulary
// in one place: one release, the one release a verifying verb submits
// on, a set resolved against what is provisioned, and the modern span
// a fresh machine defaults to.

// parseRelease reads one --on or --macos value into a release. A value
// the table does not name is the invocation's fault, so the error
// lands in the usage band. Nothing else is interpreted here: "" and
// "all" are refused like any other unknown name, because the verbs
// that treat either specially decide so before parsing, and not all
// of them agree on what "all" means.
func parseRelease(s string) (platform.Release, error) {
	r, err := platform.Parse(s)
	if err != nil {
		return platform.Release{}, &UsageError{Err: err}
	}
	return r, nil
}

// releaseFlag parses --on for the verbs that verify: one release, the
// empty flag meaning the provider default (the newest provisioned
// base). A matrix is refused with directions — the verdict note tracks
// one job per commit, so breadth comes from repeated runs or from
// exec, whose probes are built for it.
func releaseFlag(on string) (platform.Release, error) {
	if on == "" {
		return platform.Release{}, nil
	}
	if strings.EqualFold(on, "all") || strings.Contains(on, ",") {
		return platform.Release{}, usagef("this verb submits one platform; run the matrix afterwards with `dockhand verify <branch> --on <list|all>`")
	}
	return parseRelease(on)
}

// resolveReleaseSet resolves a list flag against the provisioned bases:
// nothing means the newest, "all" means every base, and otherwise each
// value in the order given, duplicates included.
//
// With requireBase, each named release must actually have a base — a
// verdict cannot be promised on an environment that does not exist.
// The check runs per element, interleaved with parsing, so `--on
// <unprovisioned>,<unknown>` reports the missing base and `--on
// <unknown>,<unprovisioned>` the unknown name: whichever the user
// wrote first is what they hear about. Without requireBase the names
// are taken as given, which is exec's contract — its probes answer
// for whatever base they are pointed at, and a missing one fails
// there, by name.
//
// The newest of no bases is not a release, so an empty set with
// nothing asked for is refused rather than sliced.
func resolveReleaseSet(on []string, provisioned []platform.Release, requireBase bool) ([]platform.Release, error) {
	if len(provisioned) == 0 && (requireBase || len(on) == 0) {
		return nil, fmt.Errorf("%w: no base images", verify.ErrNoEnvironment)
	}
	if len(on) == 0 {
		return provisioned[:1], nil
	}
	var out []platform.Release
	for _, v := range on {
		if strings.EqualFold(v, "all") {
			return provisioned, nil
		}
		r, err := parseRelease(v)
		if err != nil {
			return nil, err
		}
		if requireBase && !slices.Contains(provisioned, r) {
			return nil, fmt.Errorf("%w: no base image for %s; `dockhand provision tart --macos %s` builds one",
				verify.ErrNoEnvironment, r.Name, strings.ToLower(r.CompactName()))
		}
		out = append(out, r)
	}
	return out, nil
}

// modernReleases is the span a fresh machine provisions and the Xcode
// guide explains when no base narrows it: Monterey (Darwin 21) onward,
// in table order, oldest first. It is read from the table rather than
// from a provider's Capabilities on purpose — a provider reports its
// bases newest first, and the provision sweep's progress lines and the
// Xcode needs table are printed in this order.
func modernReleases() []platform.Release {
	var out []platform.Release
	for _, r := range platform.Releases {
		if r.Darwin >= 21 {
			out = append(out, r)
		}
	}
	return out
}
