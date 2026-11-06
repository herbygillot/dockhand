package assess

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/project"
)

// A Go port's -X target its package no longer declares holds, naming the
// argument, as mp4ff moved its version variable to another package; a
// declared variable says nothing; and a package the archive doesn't have,
// or that doesn't parse, is coverage, holding nothing (the docs dig's I18).
func TestAGoLinkTargetMustStillBeDeclared(t *testing.T) {
	port := macports.PortInfo{Name: "mp4ff", Options: map[string]string{"build.args": `-ldflags "-X github.com/Eyevinn/mp4ff/mp4.version=0.50.0 -s -w"`}}
	spec := project.Spec{GoPackages: port.GoLinkPackages()}
	require.Equal(t, []macports.GoLinkTarget{{Argument: "-X github.com/Eyevinn/mp4ff/mp4.version", Package: "github.com/Eyevinn/mp4ff/mp4", Name: "version"}}, port.GoLinkTargets())
	version := func(top string, files map[string]string) project.Reading {
		files["go.mod"] = "module github.com/Eyevinn/mp4ff\n\ngo 1.22\n"
		return read(t, top, files, spec)
	}
	findings := func(after project.Reading) (held []string, covered []string) {
		comparison := Assess(Input{Port: port, Base: port, Pairs: []Pair{{Archive: "mp4ff.tar.gz", Before: version("mp4ff-0.49.0", map[string]string{"mp4/version.go": "package mp4\n\nvar version = \"dev\"\n"}), After: after}}})
		for _, change := range comparison.Changes {
			if change.Rule == GoLinkTargetRule && change.Hold {
				held = append(held, change.Message)
			}
		}
		for _, coverage := range comparison.Coverage {
			if coverage.Policy == GoLinkTargetRule {
				covered = append(covered, coverage.Reason)
			}
		}
		return held, covered
	}

	held, _ := findings(version("mp4ff-0.50.0", map[string]string{"mp4/box.go": "package mp4\n\ntype Box struct{}\n", "internal/version.go": "package internal\n\nvar version = \"dev\"\n"}))
	require.Equal(t, []string{"upstream: package github.com/Eyevinn/mp4ff/mp4 declares no variable version, so the build's -X github.com/Eyevinn/mp4ff/mp4.version sets nothing, and the version it reports may be wrong"}, held)

	held, covered := findings(version("mp4ff-0.50.0", map[string]string{"mp4/version.go": "package mp4\n\nvar (\n\tcommit string\n\tversion = \"dev\"\n)\n"}))
	require.Empty(t, held, "declared")
	require.Empty(t, covered)

	held, covered = findings(version("mp4ff-0.50.0", map[string]string{"cmd/main.go": "package main\n"}))
	require.Empty(t, held)
	require.Equal(t, []string{"the package github.com/Eyevinn/mp4ff/mp4 for -X github.com/Eyevinn/mp4ff/mp4.version wasn't found in the archive"}, covered)

	held, covered = findings(version("mp4ff-0.50.0", map[string]string{"mp4/version.go": "package mp4\n\nvar version = \n"}))
	require.Empty(t, held)
	require.Len(t, covered, 1)
	require.Contains(t, covered[0], "couldn't be parsed")
}

// A file of a -X target's package changing, or new, holds nothing while
// the package still declares the variable: rclone 1.76.0 was held on
// twelve of package fs's files for its fs.Version, which fs/version.go
// still declared (field testing, macports-ports#36436).
func TestAGoLinkTargetsPackageIsntABuildFile(t *testing.T) {
	port := macports.PortInfo{Name: "rclone", Options: map[string]string{"build.args": `-ldflags "-X github.com/rclone/rclone/fs.Version=1.76.0"`}}
	spec := project.Spec{GoPackages: port.GoLinkPackages()}
	version := func(top string, files map[string]string) project.Reading {
		files["go.mod"] = "module github.com/rclone/rclone\n\ngo 1.22\n"
		return read(t, top, files, spec)
	}
	comparison := Assess(Input{Port: port, Base: port, Pairs: []Pair{{Archive: "rclone-1.76.0.tar.gz",
		Before: version("rclone-1.75.0", map[string]string{"fs/version.go": "package fs\n\nvar Version = \"dev\"\n", "fs/config.go": "package fs\n\nvar A = 1\n"}),
		After:  version("rclone-1.76.0", map[string]string{"fs/version.go": "package fs\n\nvar Version = \"dev\"\n", "fs/config.go": "package fs\n\nvar A = 2\n", "fs/log.go": "package fs\n"})}}})
	for _, change := range comparison.Changes {
		require.False(t, change.Hold, change.Message)
		require.NotContains(t, change.Path, "fs/", change.Message)
	}
}
