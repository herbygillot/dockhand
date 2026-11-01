package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/model"
)

// A moved source is said in update's Fixed, and in the commit's body, in
// place of a Portfile comment (the roadmap's D3).
func TestAMovedSourceIsSaidInTheFixedLineAndTheCommitBody(t *testing.T) {
	t.Parallel()
	moved := model.SourceMove{From: "git", To: "releases", Release: "2.5.0", Asset: "mindforger_2.5.0_tarball.tgz", Submodules: []string{"deps/cmark-gfm"},
		Lines: []string{"github.tarball_from releases", "distname ${name}_${version}_tarball", "extract.suffix .tgz", "worksrcdir ci_${name}_${version}"}}
	require.Equal(t, "fetch.type git is now release 2.5.0's mindforger_2.5.0_tarball.tgz, which carries its Git submodules (deps/cmark-gfm), with the checksums the update made; it writes distname ${name}_${version}_tarball, extract.suffix .tgz, worksrcdir ci_${name}_${version}",
		sourceMovedWords(moved))
	require.Equal(t, "github.tarball_from tarball is now releases, release v1.0's tool-1.0.tar.gz, with the checksums the update made",
		sourceMovedWords(model.SourceMove{From: "tarball", To: "releases", Release: "v1.0", Asset: "tool-1.0.tar.gz", Lines: []string{"github.tarball_from releases"}}))
	require.Equal(t, "github.tarball_from tarball is now archive, GitHub's archive of the tag, with the checksums the update made", sourceMovedWords(model.SourceMove{From: "tarball", To: "archive"}))

	edits := []model.Edit{{Directory: "editors/mindforger", Kind: model.EditUpdate, Release: &model.Release{ReleaseSelection: model.ReleaseSelection{CurrentVersion: "2.0.0"}, Version: "2.5.0"}, Source: &moved}}
	require.Equal(t, "Updates from 2.0.0.\nFetches release 2.5.0's mindforger_2.5.0_tarball.tgz, which carries the repository's Git submodules (deps/cmark-gfm), instead of a Git checkout.", chainBody(edits, "editors/mindforger"))
	require.Equal(t, "fixture", sourceMoves(append(edits, model.Edit{Directory: "devel/fixture", Port: "fixture", Source: &model.SourceMove{From: "tarball", To: "archive"}}))[1].Port)
}

// Where the source could move and didn't, the update's notice takes the
// place of the comparison's own, a notice holding nothing.
func TestASourceNoticeReplacesTheTarballNotice(t *testing.T) {
	t.Parallel()
	changes := []model.UpstreamChange{{Kind: "source", Rule: assess.SourceChoice, Message: "the port fetches GitHub's tarball"}, {Kind: "license", Rule: assess.LicenseChanged}}
	got := withSourceNotice(changes, "release 2.5.0 carries mindforger-2.5.0.tar.gz, which lacks deps/cmark-gfm, so the port stays on its clone")
	require.Len(t, got, 2)
	require.Equal(t, "release 2.5.0 carries mindforger-2.5.0.tar.gz, which lacks deps/cmark-gfm, so the port stays on its clone", got[0].Message)
	require.False(t, got[0].Hold)
	require.Len(t, withSourceNotice(nil, "x"), 1)
}
