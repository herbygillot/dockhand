package verdict

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/verify"
)

func TestInstallNamesReadNamesAndNotPaths(t *testing.T) {
	// Nine files, three names. brotli's three libbrotlicommon files all
	// announce /opt/local/lib/libbrotlicommon.1.dylib, and a headline
	// whose names were taken from paths would ask its dependents about
	// six libraries that nothing records.
	assert.Equal(t, []string{
		"/opt/local/lib/libbrotlicommon.1.dylib",
		"/opt/local/lib/libbrotlidec.1.dylib",
		"/opt/local/lib/libbrotlienc.1.dylib",
	}, installNames(brotli()))

	assert.Nil(t, installNames(nil), "a build that installed nothing publishes nothing")
}

func TestLinkProofNamesTheFilesThatBind(t *testing.T) {
	// The dependent's own sweep, as the provider gathers it: every
	// install name the installation records, mapped to the files that
	// record it. libSystem is in there because every Mach-O links it.
	links := map[string][]string{
		"/opt/local/lib/libwidget.3.dylib": {
			"/opt/local/lib/libgdal.36.dylib",
			"/opt/local/bin/gdalinfo",
		},
		"/usr/lib/libSystem.B.dylib": {"/opt/local/lib/libgdal.36.dylib"},
	}

	b := LinkProof("gdal", installNames(widgetAfter()), links)

	require.True(t, b.Linked)
	assert.Equal(t, "gdal", b.Port)
	// Sorted, so a note written twice reads the same, and filtered to
	// the library this change is about — a proof naming libSystem would
	// be answering a question nobody asked.
	assert.Equal(t, []string{
		"/opt/local/bin/gdalinfo links against /opt/local/lib/libwidget.3.dylib",
		"/opt/local/lib/libgdal.36.dylib links against /opt/local/lib/libwidget.3.dylib",
	}, b.Lines)
}

func TestADependentThatLinksNothingIsBuildOnlyInFact(t *testing.T) {
	// It declared a dependency and it installed, and nothing it laid
	// down records the library. That is a measurement and not an
	// inference, and it is what the pull request says instead of
	// claiming the revbump was needed.
	b := LinkProof("py313-gdal-tools", installNames(widgetAfter()),
		map[string][]string{"/usr/lib/libSystem.B.dylib": {"/opt/local/bin/gdal-config"}})

	assert.False(t, b.Linked)
	// Empty is the answer, not the absence of one: the note writes Links
	// without omitempty so that "we looked and nothing links" and
	// "nobody looked" stay tellable apart.
	assert.Empty(t, b.Lines)
}

func TestAHeadlineThatPublishesNothingProvesNothing(t *testing.T) {
	b := LinkProof("gdal", nil, map[string][]string{
		"/opt/local/lib/libwidget.3.dylib": {"/opt/local/lib/libgdal.36.dylib"}})
	assert.False(t, b.Linked, "with no names to look for, the sweep has nothing to say")
}

func TestTheProofIsOverInstallNamesTheHeadlineActuallyPublishes(t *testing.T) {
	// A dependent bound to the OLD name after a rebuild is the thing
	// this proof exists to catch: it did not pick the change up.
	old := &verify.Manifest{Port: "libwidget", Dylibs: []verify.Dylib{{
		Path: "/opt/local/lib/libwidget.2.dylib", InstallName: "/opt/local/lib/libwidget.2.dylib"}}}

	stale := map[string][]string{"/opt/local/lib/libwidget.2.dylib": {"/opt/local/lib/libgdal.36.dylib"}}
	assert.False(t, LinkProof("gdal", installNames(widgetAfter()), stale).Linked,
		"it links the version being left, not the one this change published")
	assert.True(t, LinkProof("gdal", installNames(old), stale).Linked)
}

// The proof names what MOVED, and not everything the headline
// publishes.
//
// A port publishing several libraries is the ordinary case — brotli,
// openssl and xorg-libXaw all do — so a dependent that links only the
// one that stood still is common, and it used to come back Linked with
// a line naming that library. Under a heading saying these ports were
// revbumped because a library moved, that line is evidence for a claim
// the measurement does not support.
func TestTheProofIsTakenAgainstWhatMovedAndNotAgainstEverything(t *testing.T) {
	before := &verify.Manifest{Port: "libwidget", Version: "2.4.1_0", Platform: "Sequoia",
		Dylibs: []verify.Dylib{
			{Path: "/opt/local/lib/libwidget.2.dylib", InstallName: "/opt/local/lib/libwidget.2.dylib",
				CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
			{Path: "/opt/local/lib/libwidgetx.1.dylib", InstallName: "/opt/local/lib/libwidgetx.1.dylib",
				CompatVersion: "1.0.0", CurrentVersion: "1.0.0"},
		}}
	after := &verify.Manifest{Port: "libwidget", Version: "3.0_0", Platform: "Sequoia",
		Dylibs: []verify.Dylib{
			{Path: "/opt/local/lib/libwidget.3.dylib", InstallName: "/opt/local/lib/libwidget.3.dylib",
				CompatVersion: "3.0.0", CurrentVersion: "3.0.0"},
			{Path: "/opt/local/lib/libwidgetx.1.dylib", InstallName: "/opt/local/lib/libwidgetx.1.dylib",
				CompatVersion: "1.0.0", CurrentVersion: "1.0.0"},
		}}
	abi := ABIDelta(measured(before, after))
	require.Equal(t, ABIChanged, abi.Verdict)

	// One name, and it is the new one: a dependent rebuilt against this
	// change records the name it now has.
	assert.Equal(t, []string{"/opt/local/lib/libwidget.3.dylib"}, abi.Broke())
	assert.Contains(t, installNames(after), "/opt/local/lib/libwidgetx.1.dylib",
		"the installation publishes a library that never moved")

	onlyUnmoved := map[string][]string{
		"/opt/local/lib/libwidgetx.1.dylib": {"/opt/local/lib/libgdal.36.dylib"},
	}
	b := LinkProof("gdal", abi.Broke(), onlyUnmoved)
	assert.False(t, b.Linked, "it links a library of the headline's, and not one that moved")
	assert.Empty(t, b.Lines)

	// The whole-installation set is what made this wrong: it answers
	// "did you link anything of ours", which is true of nearly every
	// dependent and evidence for nothing.
	assert.True(t, LinkProof("gdal", installNames(after), onlyUnmoved).Linked,
		"the question this replaced, and the answer that made the line unearned")
}

// Each kind of break contributes the name a dependent's own binary
// would carry, which is not the same field in each case.
func TestBrokeNamesWhatADependentWouldHaveRecorded(t *testing.T) {
	moved := ABI{Changes: []ABIChange{
		{Library: "libwidget", Subject: "/l/libwidget.3.dylib", Kind: InstallNameMoved,
			Before: "/l/libwidget.2.dylib", After: "/l/libwidget.3.dylib", Break: true},
		{Library: "libgone", Subject: "/l/libgone.1.dylib", Kind: LibraryRemoved,
			Before: "/l/libgone.1.dylib", Break: true},
		{Library: "libnarrow", Subject: "/l/libnarrow.1.dylib", Kind: CompatNarrowed,
			Before: "3.0.0", After: "2.0.0", Break: true},
		{Library: "libwide", Subject: "/l/libwide.1.dylib", Kind: CompatWidened,
			Before: "2.0.0", After: "3.0.0"},
		{Library: "libfat", Subject: "/l/libfat.dylib", Kind: Unmeasurable,
			Before: "its architectures disagree"},
	}}

	// The rename contributes the AFTER name, the removal the BEFORE one —
	// a dependent still recording a name this installation no longer
	// publishes is proven broken, and that recording is the proof — and
	// a narrowed compatibility version its own subject. Nothing that did
	// not break contributes at all.
	assert.Equal(t, []string{
		"/l/libgone.1.dylib",
		"/l/libnarrow.1.dylib",
		"/l/libwidget.3.dylib",
	}, moved.Broke())

	assert.Empty(t, ABI{}.Broke(), "a measurement with no break has nothing to prove a link against")
}
