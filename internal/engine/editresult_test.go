package engine

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// Each field of an update that its edit's result keeps becomes its own
// finding, left or fixed, and a dropped patch is no rejection (#194, R9).
func TestAnUpdatesFindingsAreMappedFieldByField(t *testing.T) {
	t.Parallel()
	base := Update{Port: "foo", Before: PortVersion{Version: "1.2"}, After: PortVersion{Version: "2.0"}}
	for _, test := range []struct {
		field string
		set   func(*Update)
		want  []model.Finding
	}{
		{"PatchProblems, with the hunks", func(u *Update) {
			u.patches = []patchcheck.Result{{Name: "fix.diff", Checked: true, Detail: "Hunk #1 FAILED at 12.\n1 out of 1 hunk FAILED\n"}}
		}, []model.Finding{{Kind: model.FindingPatchRejected, Subject: "fix.diff", Detail: []string{"Hunk #1 FAILED at 12.", "1 out of 1 hunk FAILED"}}}},
		{"PatchProblems, a dropped patch", func(u *Update) {
			u.patches = []patchcheck.Result{{Name: "fix.diff", Checked: true, Merged: true}}
			u.PatchesDropped = []string{"fix.diff"}
		}, []model.Finding{{Kind: model.FindingPatchDropped, Subject: "fix.diff", Fixed: true}}},
		{"PatchesUnchecked", func(u *Update) {
			u.patches = []patchcheck.Result{{Name: "a.diff", Detail: "no archive"}, {Name: "b.diff", Checked: true, Applies: true}}
		}, []model.Finding{{Kind: model.FindingPatchUnchecked, Subject: "a.diff", Detail: []string{"no archive"}}}},
		{"MissingWorksrcdir", func(u *Update) { u.MissingWorksrcdir = "foo-2.0" },
			[]model.Finding{{Kind: model.FindingWorksrcdirMissing, Subject: "foo-2.0"}}},
		{"Instructions", func(u *Update) { u.Instructions = []string{"devel/foo/Portfile:3: # keep at 1.x"} },
			[]model.Finding{{Kind: model.FindingInstruction, Detail: []string{"devel/foo/Portfile:3: # keep at 1.x"}}}},
		{"PlainHTTP", func(u *Update) {
			u.PlainHTTP = []PlainURL{{PlainURL: macports.PlainURL{Option: "homepage", URL: "http://foo.example/"}, HTTPS: "https://foo.example/"}}
		}, []model.Finding{{Kind: model.FindingPlainHTTP, Subject: "http://foo.example/", Detail: []string{"homepage", "https://foo.example/", "false"}}}},
		{"Others", func(u *Update) {
			u.Others = []forge.PullRequestSummary{{Number: 7, Title: "foo: update to 2.0", URL: "https://github.com/macports/macports-ports/pull/7"}}
		}, []model.Finding{{Kind: model.FindingOtherPullRequest, Subject: "https://github.com/macports/macports-ports/pull/7", Detail: []string{"7", "foo: update to 2.0"}}}},
		{"ClosedBefore", func(u *Update) { u.ClosedBefore = "an update to 2.0 was closed unmerged" },
			[]model.Finding{{Kind: model.FindingClosedBefore, Subject: "2.0", Detail: []string{"an update to 2.0 was closed unmerged"}}}},
		{"CrossesMajor", func(u *Update) { u.CrossesMajor = true },
			[]model.Finding{{Kind: model.FindingCrossesMajor, Subject: "2.0", Detail: []string{"1.2"}}}},
		{"LeavesStable", func(u *Update) { u.LeavesStable = true },
			[]model.Finding{{Kind: model.FindingLeavesStable, Subject: "2.0", Detail: []string{"1.2"}}}},
		{"Renamed", func(u *Update) { u.Renamed = "bar/foo" },
			[]model.Finding{{Kind: model.FindingRenamed, Subject: "bar/foo"}}},
		{"Stealth", func(u *Update) {
			u.Stealth = &Stealth{Distfiles: []editprep.StealthDistfile{{Name: "foo-1.2.tar.gz"}}, Revbumped: true, DistSubdir: "foo/1.2_1"}
		}, []model.Finding{{Kind: model.FindingStealth, Subject: "foo", Detail: []string{"foo/1.2_1", "true", "", "", "foo-1.2.tar.gz"}}}},
		{"ChecksumsLeft", func(u *Update) {
			u.ChecksumsLeft = []ChecksumLeft{{Kind: "context", What: "macOS 10.13", Reason: "it doesn't evaluate"}}
		}, []model.Finding{{Kind: model.FindingChecksumLeft, Subject: "macOS 10.13", Detail: []string{"context", "it doesn't evaluate"}}}},
		{"Differing", func(u *Update) { u.Differing = []string{"foo-1.2.tar.gz"} },
			[]model.Finding{{Kind: model.FindingChecksumDiffers, Subject: "foo-1.2.tar.gz"}}},
		{"PatchesRefreshed", func(u *Update) { u.PatchesRefreshed = []string{"build.diff: offsets taken up"} },
			[]model.Finding{{Kind: model.FindingPatchRefreshed, Subject: "build.diff", Fixed: true, Detail: []string{"offsets taken up"}}}},
		{"Followed", func(u *Update) { u.Followed = "bar/foo" },
			[]model.Finding{{Kind: model.FindingFollowed, Subject: "bar/foo", Fixed: true}}},
		{"Regenerated", func(u *Update) {
			u.Regenerated = []editprep.Regenerated{{Option: "cargo.crates", Count: 120, Changed: 4}}
		},
			[]model.Finding{{Kind: model.FindingRegenerated, Subject: "cargo.crates", Fixed: true, Detail: []string{"120", "4", ""}}}},
		{"Fixed", func(u *Update) { u.Fixed = []string{"whitespace: trailing spaces"} },
			[]model.Finding{{Kind: model.FindingLintFixed, Fixed: true, Detail: []string{"whitespace: trailing spaces"}}}},
		{"DistSubdirRemoved", func(u *Update) { u.DistSubdirRemoved = true },
			[]model.Finding{{Kind: model.FindingDistSubdirRemoved, Subject: "foo", Fixed: true}}},
		{"the rest is not stored", func(u *Update) {
			u.Diff, u.Started, u.Applied, u.Current, u.Setup, u.Distfiles, u.Compared, u.HandEdit = "diff", true, true, true, "github.setup", 3, 2, "refused"
		}, nil},
	} {
		update := base
		test.set(&update)
		require.Equal(t, test.want, update.findings(), test.field)
	}
}

// A finding is said from its data, and one of a kind this dockhand
// doesn't know by its name, subject and detail, quoted text as captured
// (#194, R8).
func TestAnUnknownFindingIsSaidByItsNameAndDetail(t *testing.T) {
	t.Parallel()
	require.Equal(t, "patch fix.diff doesn't apply", FindingWords(model.Finding{Kind: model.FindingPatchRejected, Subject: "fix.diff", Detail: []string{"Hunk #1 FAILED"}}))
	require.Equal(t, "the Portfile says: devel/foo/Portfile:3: # keep at 1.x  ", FindingWords(model.Finding{Kind: model.FindingInstruction, Detail: []string{"devel/foo/Portfile:3: # keep at 1.x  "}}))
	require.Equal(t, "sdk-pinned: MacOSX14.sdk (read from the build; kept)", FindingWords(model.Finding{Kind: "sdk-pinned", Subject: "MacOSX14.sdk", Detail: []string{"read from the build", "kept"}}))
	require.Equal(t, "sdk-pinned", FindingWords(model.Finding{Kind: "sdk-pinned"}))
}
