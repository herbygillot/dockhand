package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tool"
)

func TestReportRendering(t *testing.T) {
	origVer := runVersion
	t.Cleanup(func() { runVersion = origVer })
	// The probe runs over a stated machine, not this one: a finder
	// whose PATH search answers for exactly two tools.
	tools := tool.NewFinder(func(name string) (string, error) {
		switch name {
		case "port-tclsh", "git":
			return "/opt/local/bin/" + name, nil
		}
		return "", errors.New("not found")
	})
	runVersion = func(_ context.Context, path string, args ...string) string {
		if strings.Contains(path, "git") {
			return "git version 2.4.0"
		}
		return ""
	}

	out := Probe(t.Context(), tools).String()
	require.Contains(t, out, "port-tclsh   /opt/local/bin/port-tclsh")
	require.Contains(t, out, "tclsh        missing")
	require.Contains(t, out, "below the 2.5 floor")
	require.Contains(t, out, "evaluation               available")
	require.Contains(t, out, "branch workflow          unavailable")
	require.Contains(t, out, "VM verification          unavailable (no tart)")
}

// Every probe that execs runs under the context the caller handed in.
// They used to run under context.Background(), which meant a run's
// interrupt was noticed only between probes and never reached the exec
// that was hanging — and doctor is precisely what someone runs when the
// machine is already misbehaving. A context cancelled before the probe
// starts stands in for one cancelled during it: what is asserted is
// which context arrived, not what the exec did with it.
func TestProbesRunUnderTheCallersContext(t *testing.T) {
	origVer, origProv, origRest := runVersion, provisioned, restorable
	t.Cleanup(func() { runVersion, provisioned, restorable = origVer, origProv, origRest })

	// port-tclsh is deliberately absent: its version probe runs a real
	// port client through prefix.Version, and this test states a
	// machine rather than asking this one.
	tools := tool.NewFinder(func(name string) (string, error) {
		switch name {
		case "git", "gh", "tart":
			return "/opt/local/bin/" + name, nil
		}
		return "", errors.New("not found")
	})

	var seen []error
	runVersion = func(ctx context.Context, path string, args ...string) string {
		seen = append(seen, ctx.Err())
		return ""
	}
	provisioned = func(ctx context.Context, _ *tool.Finder) ([]string, error) {
		seen = append(seen, ctx.Err())
		return nil, ctx.Err()
	}
	restorable = func(ctx context.Context, _ *tool.Finder) ([]string, error) {
		seen = append(seen, ctx.Err())
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	Probe(ctx, tools)

	require.Len(t, seen, 4,
		"git's version, gh's version, and the two image listings — bases and goldens — each exec")
	for _, err := range seen {
		assert.ErrorIs(t, err, context.Canceled)
	}
}

func TestVersionBelowIsNumeric(t *testing.T) {
	// 2.45 sorts below "2.5" lexically; the floor must compare numerically.
	require.False(t, versionBelow("2.45.0", 2, 5))
	require.False(t, versionBelow("2.10.1", 2, 5))
	require.False(t, versionBelow("2.5.0", 2, 5))
	require.False(t, versionBelow("3.0", 2, 5))
	require.True(t, versionBelow("2.4.0", 2, 5))
	require.True(t, versionBelow("1.9.5", 2, 5))
	// The enforced floor is 2.5 (worktree-aware plumbing).
	require.False(t, versionBelow("2.5.0", 2, 5))
	require.False(t, versionBelow("2.39.5", 2, 25))
	require.True(t, versionBelow("2.24.4", 2, 25))
	// Unparseable versions are not claimed to be below the floor.
	require.False(t, versionBelow("", 2, 5))
	require.False(t, versionBelow("unknown", 2, 5))
}

// A shim is written against one MacPorts and taken to hold for later
// ones until superseded. An installation past the newest shim still
// works, and says so rather than failing.
func TestShimNote(t *testing.T) {
	assert.Empty(t, shimNote("2.12.6", "2.12.6"), "the version it was written for is silent")
	assert.Empty(t, shimNote("2.11.0", "2.12.6"), "an older installation is silent; its own shim fits")
	assert.Contains(t, shimNote("2.13.0", "2.12.6"), "2.12.6")
	assert.Contains(t, shimNote("2.13.0", "2.12.6"), "newer than")
	// MacPorts' ordering, not lexical: 2.9 is older than 2.12.
	assert.Empty(t, shimNote("2.9.0", "2.12.6"))
}

// The tart binary being present says nothing about whether any
// verification environment exists — an installed tart with no base
// image fails on first use. The bases are the capability.
func TestVMVerificationRequiresABase(t *testing.T) {
	r := Report{Tools: []Tool{{Name: "tart", Found: true, Path: "/opt/local/bin/tart"}}}
	assert.Contains(t, r.String(), "no base images")
	assert.Contains(t, r.String(), "dockhand provision")

	r.VMBases = []string{"Sequoia", "Sonoma"}
	assert.Contains(t, r.String(), "available (Sequoia, Sonoma)")

	none := Report{Tools: []Tool{{Name: "tart", Found: false}}}
	assert.Contains(t, none.String(), "no tart")
}

// A GOLDEN WITH NO BASE IS A CAPABILITY ONE COMMAND AWAY, and doctor
// used to report only "no base images: run `dockhand provision tart`" —
// the full road, a fetch and a MacPorts install and a toolchain, beside
// a copy that would have taken seconds to clone.
func TestDoctorNamesTheCloneWhenAGoldenStandsAndNoBaseDoes(t *testing.T) {
	origProv, origRest := provisioned, restorable
	t.Cleanup(func() { provisioned, restorable = origProv, origRest })
	provisioned = func(context.Context, *tool.Finder) ([]string, error) { return nil, nil }
	restorable = func(context.Context, *tool.Finder) ([]string, error) { return []string{"Sequoia"}, nil }

	out := Probe(t.Context(), hasTart(t)).String()
	assert.Contains(t, out, "--restore", "the cheap remedy is named")
	assert.Contains(t, out, "Sequoia", "and so is what it can be restored for")
}

// Neither is the honest full-road case, and it must not offer a clone
// there is nothing to clone from.
func TestDoctorNamesTheFullRoadWhenThereIsNoGoldenEither(t *testing.T) {
	origProv, origRest := provisioned, restorable
	t.Cleanup(func() { provisioned, restorable = origProv, origRest })
	provisioned = func(context.Context, *tool.Finder) ([]string, error) { return nil, nil }
	restorable = func(context.Context, *tool.Finder) ([]string, error) { return nil, nil }

	out := Probe(t.Context(), hasTart(t)).String()
	assert.Contains(t, out, "no base images and no goldens")
	assert.NotContains(t, out, "--restore", "nothing to clone from, so nothing to offer")
}

// A release this machine could verify on after one clone is worth
// saying even when other releases are already available.
func TestDoctorReportsARestorableReleaseBesideTheAvailableOnes(t *testing.T) {
	origProv, origRest := provisioned, restorable
	t.Cleanup(func() { provisioned, restorable = origProv, origRest })
	provisioned = func(context.Context, *tool.Finder) ([]string, error) { return []string{"Sequoia"}, nil }
	restorable = func(context.Context, *tool.Finder) ([]string, error) { return []string{"Sequoia", "Sonoma"}, nil }

	out := Probe(t.Context(), hasTart(t)).String()
	assert.Contains(t, out, "available (Sequoia; restorable: Sonoma)",
		"the golden whose base is gone is named; the one that has a base is not repeated")
}

// hasTart is a finder that says tart is installed and nothing else is,
// so the VM line is what the report is about.
func hasTart(t *testing.T) *tool.Finder {
	t.Helper()
	return tool.NewFinder(func(name string) (string, error) {
		if name == "tart" {
			return "/opt/local/bin/tart", nil
		}
		return "", errors.New("not found")
	})
}

// A PLATFORM IS NOT AN IMAGE, and doctor now says which. "Tahoe" names
// a macOS and not a build of it, and the bases dockhand provisions come
// from a `:latest` tag that moves — so two machines could both report
// "available (Tahoe)" and be verifying on different disks.
func TestDoctorNamesTheImageBehindEachBase(t *testing.T) {
	const img = "ghcr.io/cirruslabs/macos-tahoe-vanilla@sha256:eeec54bf"
	op, or, ob := provisioned, restorable, baseImage
	provisioned = func(context.Context, *tool.Finder) ([]string, error) { return []string{"Tahoe"}, nil }
	restorable = func(context.Context, *tool.Finder) ([]string, error) { return nil, nil }
	baseImage = func(release string) string {
		if release == "Tahoe" {
			return img
		}
		return ""
	}
	t.Cleanup(func() { provisioned, restorable, baseImage = op, or, ob })

	tools := tool.NewFinder(func(name string) (string, error) {
		if name == "tart" {
			return "/opt/local/bin/tart", nil
		}
		return "", errors.New("not found")
	})
	out := Probe(t.Context(), tools).String()
	assert.Contains(t, out, "Tahoe: "+img)
}

// A BASE PROVISIONED BEFORE DOCKHAND RECORDED ONE SAYS SO BY SAYING
// NOTHING: the moment that could answer has passed, and `latest` has
// moved since, so there is no honest way to fill it in later.
func TestDoctorInventsNoImageForABaseThatRecordedNone(t *testing.T) {
	op, or, ob := provisioned, restorable, baseImage
	provisioned = func(context.Context, *tool.Finder) ([]string, error) { return []string{"Tahoe"}, nil }
	restorable = func(context.Context, *tool.Finder) ([]string, error) { return nil, nil }
	baseImage = func(string) string { return "" }
	t.Cleanup(func() { provisioned, restorable, baseImage = op, or, ob })

	tools := tool.NewFinder(func(name string) (string, error) {
		if name == "tart" {
			return "/opt/local/bin/tart", nil
		}
		return "", errors.New("not found")
	})
	out := Probe(t.Context(), tools).String()
	assert.Contains(t, out, "available (Tahoe)")
	assert.NotContains(t, out, "Tahoe: ", "no line at all, rather than an empty one")
}
