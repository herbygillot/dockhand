package sweep

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

func subject(portdir, subport string, src string, fields map[string]string) Subject {
	return Subject{
		Target:  tree.Target{Portdir: portdir, Subport: subport},
		Src:     []byte(src),
		Indexed: fields,
	}
}

func only(t *testing.T, s Subject) Excluded {
	t.Helper()
	keep, out := Exclusions([]Subject{s})
	require.Empty(t, keep)
	require.Len(t, out, 1)
	return out[0]
}

func kept(t *testing.T, s Subject) {
	t.Helper()
	keep, out := Exclusions([]Subject{s})
	require.Empty(t, out, "expected no exclusion, got %+v", out)
	require.Len(t, keep, 1)
}

// The index's replaced_by is the only machine-readable obsolescence
// marker a real PortIndex carries, and about 2004 of the 2158 entries
// that have one are the perl5 group's unversioned stubs — the worked
// example for why a sweep filters before it asks upstream anything.
func TestExcludeReplacedBy(t *testing.T) {
	ex := only(t, subject("/t/perl/p5-boolean", "", "PortSystem 1.0\n",
		map[string]string{"replaced_by": "p5.34-boolean"}))
	assert.Equal(t, ReplacedBy, ex.Reason)
	assert.Contains(t, ex.Detail, `"p5.34-boolean"`)
	assert.False(t, ex.Reason.Human(), "a replaced port is finished; nobody is owed a review")
}

// science/wfview's replaced_by is a URL, not a port name. The value is
// quoted and never resolved, because code that looked the replacement
// up would fail on one entry in two thousand.
func TestExcludeReplacedByQuotesWhateverItSays(t *testing.T) {
	ex := only(t, subject("/t/science/wfview", "", "",
		map[string]string{"replaced_by": "https://wfview.org/download/"}))
	assert.Contains(t, ex.Detail, `"https://wfview.org/download/"`)
}

// php/php-unit uses the obsolete PortGroup and names no replacement, so
// the group's own description is the second signal. Thirteen Portfiles
// in the tree are shaped this way.
func TestExcludeObsoleteGroupWithoutReplacedBy(t *testing.T) {
	ex := only(t, subject("/t/php/php-unit", "", "# Remove after 2027-03-01\n\nPortSystem              1.0\nPortGroup obsolete      1.0\n",
		map[string]string{"description": "Obsolete port", "known_fail": "yes"}))
	assert.Equal(t, ObsoleteGroup, ex.Reason)
}

func TestExcludeObsoleteGroupReplacementDescription(t *testing.T) {
	ex := only(t, subject("/t/devel/x", "", "",
		map[string]string{"description": "Obsolete port, replaced by y"}))
	assert.Equal(t, ObsoleteGroup, ex.Reason)
}

// known_fail is not a substitute for either signal: 1222 entries carry
// it and 1048 of those are neither obsolete nor replaced. Excluding on
// it would drop a thousand live ports.
func TestKnownFailIsNotObsolescence(t *testing.T) {
	kept(t, subject("/t/aqua/qt5", "qt5-qtwebengine", "PortSystem 1.0\nversion 5.15.17\n",
		map[string]string{"known_fail": "yes", "description": "Qt WebEngine"}))
}

// Exclusion keys off the Target's own index entry and never off a text
// scan of the Portfile. devel/libftdi holds an obsolete top-level port
// above two live subports in one file; 2041 portdirs in the tree are
// mixed this way, and a scan of the bytes would take all three.
func TestExclusionIsPerTargetNotPerPortfile(t *testing.T) {
	const src = `name                libftdi
if {${name} eq ${subport}} {
    replaced_by         libftdi0
    PortGroup           obsolete 1.0

    epoch               1
}
`
	dir := "/t/devel/libftdi"
	ex := only(t, subject(dir, "", src, map[string]string{"replaced_by": "libftdi0", "known_fail": "yes"}))
	assert.Equal(t, ReplacedBy, ex.Reason)

	kept(t, subject(dir, "libftdi0", src, map[string]string{}))
	kept(t, subject(dir, "libftdi1", src, map[string]string{}))
}

// devel/cmake-devel writes one obsolete subport with the PortGroup line
// commented out and replaced_by still set. The index sees it; a group
// scan does not.
func TestExclusionSeesACommentedOutPortGroup(t *testing.T) {
	const src = `subport ${old_subport_gui} {
    ### PortGroup obsolete 1.0
    replaced_by ${subport_gui}
}
`
	ex := only(t, subject("/t/devel/cmake-devel", "cmake-gui-devel", src,
		map[string]string{"replaced_by": "cmake-devel-gui"}))
	assert.Equal(t, ReplacedBy, ex.Reason)
}

// The do-not-upgrade family, quoted from the tree. Every one of these
// abuts the line that declares the version — above it, below it, or
// across the `name` and `epoch` lines that sit between.
func TestDoNotUpgradeTruePositives(t *testing.T) {
	cases := map[string]string{
		"gnome/adwaita-icon-theme": "PortSystem          1.0\n\n" +
			"# Note: Version 45 and later requires gtk4, so stay at version 44\n" +
			"name                adwaita-icon-theme\nversion             44.0\nrevision            1\n",
		"textproc/doxygen": "\n# please don't update doxygen without first checking the build status of doxygen-devel\n" +
			"version                 1.18.0\nepoch                   0\n",
		"security/keychain-cpp": "\nname                keychain-cpp\n" +
			"# 1.3.0 breaks the API. Stay at 1.2.1 until this is addressed,\n" +
			"# either by upstream or at least locally in MacPorts.\n" +
			"# https://github.com/hrantzsch/keychain/pull/33\n" +
			"github.setup        hrantzsch keychain 1.2.1 v\nrevision            0\n",
		"fuse/osxfuse": "name                osxfuse\nepoch               2\n" +
			"# Note: do not update past 3.8.3\n" +
			"# The current maintainer has decided not to make source available for\n" +
			"version             3.8.3\n",
		"devel/developer_cmds": "\n# Notice that version 66 lacks some of the tools.\n" +
			"# Do not update unless dependencies are verified to build, or make a separate subport.\n" +
			"name                    developer_cmds\nversion                 63\nrevision                1\n",
		"gnome/at-spi2-core": "\nname                at-spi2-core\n" +
			"# you probably want to keep this at the same version as at-spi2-atk\n" +
			"version             2.44.1\nrevision            1\n",
		"python/py-javaproperties": "\nname                py-javaproperties\n" +
			"# azure-cli requires ~=0.5.1; do not bump past 0.5.x without updating azure-cli.\n" +
			"version             0.5.2\ncategories-append   devel\n",
		// The note is written UNDER the version it holds. A
		// forward-only scan would read the instruction and bump anyway.
		"multimedia/mkvtoolnix-legacy": "if {${os.platform} ne \"darwin\" || ${os.major} >= 14} {\n" +
			"    version         81.0\n    revision        2\n" +
			"    # Versions newer than this requires Qt 6 - do not update\n" +
			"    checksums       rmd160  03c1ad9\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			ex := only(t, subject("/t/"+name, "", src, nil))
			assert.Equal(t, DoNotUpgrade, ex.Reason)
			assert.True(t, ex.Reason.Human(), "a pin is somebody's decision; it goes to a person")
			assert.NotEmpty(t, ex.Quote)
		})
	}
}

// The measured false positives, quoted too. Every one is a real comment
// about something else being held still, and every one abuts a `set` or
// a block opener rather than a version — which is the whole of what
// separates them.
func TestDoNotUpgradeFalsePositives(t *testing.T) {
	cases := map[string]string{
		"lang/llvm-14": "}\n\n# Do not update past 3.10 as this creates circular dependency issues\n" +
			"set py_ver              3.10\nset py_ver_nodot        [string map {. {}} ${py_ver}]\n",
		"audio/cmus": "\nvariant ffmpeg  description {Support ffmpeg} {\n" +
			"    # do not update to 8 yet\n" +
			"    # ip/ffmpeg.c:229:3: error: call to undeclared function 'avcodec_close'\n" +
			"    set ffmpeg_ver        7\n",
		"lang/pypy": "    } else {\n        # for compatibility, don't bump this needlessly\n" +
			"        set bootstrapper        \"pypy-5.1.0-osx64\"\n",
		"lang/ccl": "}\n\n# Pegged, do not update:\nplatform darwin powerpc {\n    if {${os.major} > 8} {\n",
		// Not a comment at all: the phrase is inside a
		// long_description continuation.
		"graphics/glfw": "\n    long_description ${description}. This version of GLFW is the latest to provide support for \\\n" +
			"        Mac OS X 10.6 and prior, and it will not be updated. It is provided in the \\\n" +
			"        hope that it allows ports depending on GLFW to build on these older installs.\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			kept(t, subject("/t/"+name, "", src, nil))
		})
	}
}

// The one residual false positive, pinned so it is a decision and not
// a surprise. cpmtools' note is about upstream not moving its version
// number, not about the maintainer holding it — but it is a comment,
// and it does abut the version. It costs a review, which is the side of
// the trade this filter is built to err on.
func TestDoNotUpgradeResidualFalsePositive(t *testing.T) {
	src := "name                cpmtools\nversion             2.24\nrevision            2\n\n" +
		"# https://trac.macports.org/ticket/70569\n" +
		"# New versions of this software do not update the version number, only\n" +
		"# updating the same archive on the website (a rather silly decision)\n" +
		"dist_subdir         ${name}/${version}_${revision}\n"
	ex := only(t, subject("/t/sysutils/cpmtools", "", src, nil))
	assert.Equal(t, DoNotUpgrade, ex.Reason)
	assert.True(t, ex.Reason.Human(), "which is why a false positive here is affordable")
}

// The quote is the whole block, verbatim — the '#' and the indentation
// as the Portfile writes them — because the condition a human has to
// weigh sits above the verb as often as below it.
func TestDoNotUpgradeQuotesTheWholeBlockVerbatim(t *testing.T) {
	src := "\nname                keychain-cpp\n" +
		"# 1.3.0 breaks the API. Stay at 1.2.1 until this is addressed,\n" +
		"# either by upstream or at least locally in MacPorts.\n" +
		"github.setup        hrantzsch keychain 1.2.1 v\n"
	ex := only(t, subject("/t/security/keychain-cpp", "", src, nil))
	assert.Equal(t,
		"# 1.3.0 breaks the API. Stay at 1.2.1 until this is addressed,\n"+
			"# either by upstream or at least locally in MacPorts.",
		ex.Quote)
}

// A hub whose comments oblige a cascade of revbumps goes to the human
// lane too: bumping gdal or ffmpeg inside a thousand-port sweep leaves
// the tree's dependents stale with nothing recorded. The rule is
// intent's own, so the sweep and the planner read the same comment the
// same way.
func TestExcludeRevbumpHub(t *testing.T) {
	src := "PortSystem 1.0\nname gdal\n" +
		"# Please revbump py-gdal whenever this port is updated.\n" +
		"set foo 1\n"
	ex := only(t, subject("/t/gis/gdal", "", src, nil))
	assert.Equal(t, RevbumpHub, ex.Reason)
	assert.True(t, ex.Reason.Human())
}

func TestExclusionsKeepsAnOrdinaryPort(t *testing.T) {
	kept(t, subject("/t/sysutils/kubectl", "", "PortSystem 1.0\nname kubectl\nversion 1.34.1\n",
		map[string]string{"description": "Kubernetes CLI"}))
}

// Select is the I/O half: one index pass, then one Portfile read per
// target.
func TestSelect(t *testing.T) {
	tr := fixture(t,
		entry{name: "p5-boolean", portdir: "perl/p5-boolean",
			fields: map[string]string{"replaced_by": "p5.34-boolean"}},
		entry{name: "p5.34-boolean", portdir: "perl/p5-boolean"},
		entry{name: "kubectl", portdir: "sysutils/kubectl"},
	)
	pf := filepath.Join(tr.Root(), "sysutils", "kubectl", macports.PortfileName)
	require.NoError(t, os.WriteFile(pf, []byte(
		"PortSystem 1.0\nname kubectl\n# Note: do not update past 1.34\nversion 1.34.1\n"), 0o644))

	sel, err := Select(tr, []tree.Target{
		{Portdir: filepath.Join(tr.Root(), "perl", "p5-boolean")},
		{Portdir: filepath.Join(tr.Root(), "perl", "p5-boolean"), Subport: "p5.34-boolean"},
		{Portdir: filepath.Join(tr.Root(), "sysutils", "kubectl")},
	})
	require.NoError(t, err)

	// The live subport survives; the stub and the pinned port do not.
	require.Len(t, sel.Keep, 1)
	assert.Equal(t, "p5.34-boolean", sel.Keep[0].Subport)
	require.Len(t, sel.Excluded, 2)
	assert.Equal(t, ReplacedBy, sel.Excluded[0].Reason)
	assert.Equal(t, DoNotUpgrade, sel.Excluded[1].Reason)
}

// A tree with no PortIndex loses the two index rules rather than
// failing: name resolution needs an index, and this does not.
func TestSelectWithoutAnIndex(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, macports.PortGroupDir), 0o755))
	dir := filepath.Join(root, "devel", "x")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName),
		[]byte("PortSystem 1.0\nversion 1.0\n"), 0o644))
	tr, err := tree.Open(root)
	require.NoError(t, err)

	sel, err := Select(tr, []tree.Target{{Portdir: dir}})
	require.NoError(t, err)
	assert.Len(t, sel.Keep, 1)
	assert.Empty(t, sel.Excluded)
}

// A Portfile that cannot be read excludes nothing. A filter that
// dropped ports on an I/O error would shrink a sweep for a reason that
// has nothing to do with the ports.
func TestSelectSurvivesAnUnreadablePortfile(t *testing.T) {
	tr := fixture(t, entry{name: "kubectl", portdir: "sysutils/kubectl"})
	dir := filepath.Join(tr.Root(), "sysutils", "kubectl")
	require.NoError(t, os.Remove(filepath.Join(dir, macports.PortfileName)))

	sel, err := Select(tr, []tree.Target{{Portdir: dir}})
	require.NoError(t, err)
	assert.Len(t, sel.Keep, 1)
}
