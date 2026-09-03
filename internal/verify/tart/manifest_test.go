package tart

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/verify"
)

// A captured baseline, exactly as the submit wrote it. The bytes are the
// shape macports/build's fixtures pin against real otool output; what is
// asserted here is that the provider reads back what the submit left,
// which is a different claim from the parser working.
const plantedBaseline = `===> dockhand manifest: port
libwidget
===> dockhand manifest: version
  libwidget @2.4.1_0 (active)
===> dockhand manifest: platform
15.6 arm64
===> dockhand manifest: files
/opt/local/lib/libwidget.2.dylib
===> dockhand manifest: id
/opt/local/lib/libwidget.2.dylib:
/opt/local/lib/libwidget.2.dylib
===> dockhand manifest: links
/opt/local/lib/libwidget.2.dylib:
	/opt/local/lib/libwidget.2.dylib (compatibility version 2.0.0, current version 2.4.1)
===> dockhand manifest: end
`

// job is the value a settle holds: a plain record of what was submitted,
// not a handle. Reading a manifest from it is the whole point of the
// baseline living in the guest rather than in this process.
func (g *fakeGuest) job() verify.Job { return verify.Job{Provider: "tart", ID: g.vm} }

// The before was measured at submit and is read back at settle, possibly
// in another process. The after is taken live, because the guest is
// still holding it.
func TestManifestsReadsTheBaselineAndTakesTheInstalledSideLive(t *testing.T) {
	g := newFakeGuest(t, `
  "-q installed libwidget") echo "  libwidget @3.0.0_0 (active)";;
  "-q contents libwidget") echo "  /opt/local/lib/libwidget.3.dylib";;
`)
	g.plant("manifest.ports", "libwidget\n")
	g.plant("baseline", verify.BaselineArchive+"\n")
	g.plant("manifest.pre", plantedBaseline)
	g.answer("D", "libwidget.3.dylib", "@FILE@:\n/opt/local/lib/libwidget.3.dylib\n")
	g.answer("L", "libwidget.3.dylib",
		"@FILE@:\n\t/opt/local/lib/libwidget.3.dylib (compatibility version 3.0.0, current version 3.0.0)\n")

	got, err := g.provider().Manifests(t.Context(), g.job())
	require.NoError(t, err)

	assert.Equal(t, verify.BaselineArchive, got.BaselineSource)
	assert.Empty(t, got.BaselineReason)
	require.NotNil(t, got.Baseline)
	assert.Equal(t, "2.4.1_0", got.Baseline.Version)
	assert.Equal(t, []verify.Dylib{{
		Path:           "/opt/local/lib/libwidget.2.dylib",
		InstallName:    "/opt/local/lib/libwidget.2.dylib",
		CompatVersion:  "2.0.0",
		CurrentVersion: "2.4.1",
	}}, got.Baseline.Dylibs)

	require.NotNil(t, got.Installed)
	assert.Equal(t, "3.0.0_0", got.Installed.Version)
	assert.Equal(t, "/opt/local/lib/libwidget.3.dylib", got.Installed.Dylibs[0].InstallName)
	assert.Equal(t, "3.0.0", got.Installed.Dylibs[0].CompatVersion)
}

// The link proof: which of the cohort's members actually bound to what
// the headline now publishes.
//
// Restricted to the headline's own install names on purpose. Every
// dependent links against libSystem and against its own libraries too,
// and a map carrying those would answer a question nobody asked while
// burying the one that was.
func TestTheLinkProofNamesOnlyWhatTheHeadlinePublishes(t *testing.T) {
	g := newFakeGuest(t, `
  "-q installed libwidget") echo "  libwidget @3.0.0_0 (active)";;
  "-q contents libwidget") echo "  /opt/local/lib/libwidget.3.dylib";;
  "-q installed gdal") echo "  gdal @3.9.0_1 (active)";;
  "-q contents gdal") printf '  %s\n' /opt/local/bin/gdalinfo /opt/local/lib/libgdal.36.dylib;;
`)
	g.plant("manifest.ports", "libwidget\ngdal\n")
	g.plant("baseline", verify.BaselineNone+"\nnothing to measure against\n")
	g.answer("D", "libwidget.3.dylib", "@FILE@:\n/opt/local/lib/libwidget.3.dylib\n")
	g.answer("L", "libwidget.3.dylib",
		"@FILE@:\n\t/opt/local/lib/libwidget.3.dylib (compatibility version 3.0.0, current version 3.0.0)\n")
	// The dependent's program: otool -D prints its header and no body,
	// which is how an executable is told from a library.
	g.answer("D", "gdalinfo", "@FILE@:\n")
	g.answer("L", "gdalinfo", "@FILE@:\n"+
		"\t/opt/local/lib/libgdal.36.dylib (compatibility version 36.0.0, current version 36.3.9)\n"+
		"\t/opt/local/lib/libwidget.3.dylib (compatibility version 3.0.0, current version 3.0.0)\n"+
		"\t/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1356.0.0)\n")
	g.answer("D", "libgdal.36.dylib", "@FILE@:\n/opt/local/lib/libgdal.36.dylib\n")
	g.answer("L", "libgdal.36.dylib", "@FILE@:\n"+
		"\t/opt/local/lib/libgdal.36.dylib (compatibility version 36.0.0, current version 36.3.9)\n"+
		"\t/opt/local/lib/libwidget.3.dylib (compatibility version 3.0.0, current version 3.0.0)\n")

	got, err := g.provider().Manifests(t.Context(), g.job())
	require.NoError(t, err)

	// Keyed by the MEMBER that recorded them. The roster's position is
	// the member's name and a capture is read per position, so this is
	// the last place the attribution exists — a file path does not say
	// which port installed it, and a body that says "gdal links against
	// libwidget.3.dylib" per member has nothing else to say it from.
	assert.Equal(t, map[string]map[string][]string{
		"gdal": {
			"/opt/local/lib/libwidget.3.dylib": {
				"/opt/local/bin/gdalinfo",
				"/opt/local/lib/libgdal.36.dylib",
			},
		},
	}, got.Links, "the members' bindings to the headline, and nothing else")
	assert.Equal(t, "libwidget", got.Installed.Port, "the manifest is the headline's")
}

// A solo request has no members, so there is nothing standing on the
// change yet and no link proof to give. An empty map and a map full of
// libSystem are different answers, and this is the honest one.
func TestASoloSubjectHasNoLinkProof(t *testing.T) {
	g := newFakeGuest(t, `
  "-q installed libwidget") echo "  libwidget @3.0.0_0 (active)";;
  "-q contents libwidget") echo "  /opt/local/lib/libwidget.3.dylib";;
`)
	g.plant("manifest.ports", "libwidget\n")
	g.plant("baseline", verify.BaselineArchive+"\n")
	g.plant("manifest.pre", plantedBaseline)
	g.answer("D", "libwidget.3.dylib", "@FILE@:\n/opt/local/lib/libwidget.3.dylib\n")
	g.answer("L", "libwidget.3.dylib",
		"@FILE@:\n\t/opt/local/lib/libwidget.3.dylib (compatibility version 3.0.0, current version 3.0.0)\n")

	got, err := g.provider().Manifests(t.Context(), g.job())
	require.NoError(t, err)

	assert.Empty(t, got.Links)
}

// The three ways there is no baseline, each said in its own words.
// "none" alone is the shape of a guess: a port that did not exist at the
// merge base, an archive that was never published and a capture that was
// cut off are three facts with three remedies.
func TestManifestsSaysWhyThereIsNoBaseline(t *testing.T) {
	for _, tc := range []struct {
		name     string
		baseline string
		pre      string
		want     string
	}{
		{
			name:     "the submit recorded the reason",
			baseline: "none\nno merge-base portdir was staged, so there is nothing to install as the before\n",
			want:     "no merge-base portdir was staged, so there is nothing to install as the before",
		},
		{
			name:     "the submit recorded none and no reason",
			baseline: "none\n",
			want:     "the environment did not say why",
		},
		{
			name:     "the submit recorded a baseline and the capture is gone",
			baseline: verify.BaselineArchive + "\n",
			want:     "the environment recorded a baseline and the capture is not there",
		},
		{
			name:     "the capture was cut off",
			baseline: verify.BaselineArchive + "\n",
			pre:      plantedBaseline[:strings.Index(plantedBaseline, "===> dockhand manifest: links")],
			want:     "the baseline capture could not be read: build: the manifest frame is truncated",
		},
		{
			name:     "the file says something nobody wrote",
			baseline: "sideways\n",
			want:     "the environment recorded an unreadable baseline: sideways",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newFakeGuest(t, `
  "-q installed libwidget") echo "  libwidget @3.0.0_0 (active)";;
`)
			g.plant("manifest.ports", "libwidget\n")
			g.plant("baseline", tc.baseline)
			if tc.pre != "" {
				g.plant("manifest.pre", tc.pre)
			}

			got, err := g.provider().Manifests(t.Context(), g.job())
			require.NoError(t, err)

			assert.Equal(t, verify.BaselineNone, got.BaselineSource)
			assert.Nil(t, got.Baseline)
			assert.Equal(t, tc.want, got.BaselineReason)
		})
	}
}

// A job whose environment never recorded anything at all is still "no
// baseline" and not an empty source. An empty source reads as a field
// nobody filled in, which is the one thing a finding must not have to
// guess about.
func TestAnEnvironmentThatRecordedNothingStillSaysNone(t *testing.T) {
	g := newFakeGuest(t, `
  "-q installed libwidget") echo "  libwidget @3.0.0_0 (active)";;
`)
	g.plant("manifest.ports", "libwidget\n")

	got, err := g.provider().Manifests(t.Context(), g.job())
	require.NoError(t, err)

	assert.Equal(t, verify.BaselineNone, got.BaselineSource)
	assert.Equal(t, "the environment recorded no baseline", got.BaselineReason)
}

// A caller holding a banked measurement gets the source back and no
// value: a manifest banked in this repository is the caller's own fact,
// and a provider that carried one would be keeping records.
func TestABankedBaselineComesBackAsItsSourceAndNoValue(t *testing.T) {
	g := newFakeGuest(t, `
  "-q installed libwidget") echo "  libwidget @3.0.0_0 (active)";;
`)
	g.plant("manifest.ports", "libwidget\n")
	g.plant("baseline", verify.BaselineBanked+"\n")

	got, err := g.provider().Manifests(t.Context(), g.job())
	require.NoError(t, err)

	assert.Equal(t, verify.BaselineBanked, got.BaselineSource)
	assert.Nil(t, got.Baseline)
	assert.Empty(t, got.BaselineReason)
}

// A build that failed before it installed anything produced nothing to
// measure. That is not a port that laid nothing down, and it is not an
// error either — the run's own verdict says what happened.
func TestAnInstallThatNeverHappenedHasNoManifest(t *testing.T) {
	g := newFakeGuest(t, "")
	g.plant("manifest.ports", "libwidget\n")
	g.plant("baseline", verify.BaselineNone+"\nthe archive was not published\n")

	got, err := g.provider().Manifests(t.Context(), g.job())
	require.NoError(t, err)

	assert.Nil(t, got.Installed)
	assert.Equal(t, verify.BaselineNone, got.BaselineSource)
}

// A job nobody asked a manifest of is refused by name. Answering with an
// empty comparison would be read as a port that installed nothing beside
// a baseline in which every library had vanished.
func TestAJobWithNoRosterIsRefusedByName(t *testing.T) {
	g := newFakeGuest(t, "")

	_, err := g.provider().Manifests(t.Context(), g.job())
	require.ErrorIs(t, err, verify.ErrUnsupported)

	_, err = g.provider().Probe(t.Context(), g.job(), "libwidget")
	require.ErrorIs(t, err, verify.ErrUnsupported)
}

func TestManifestsAndProbeGuardTheJobTheWayPollDoes(t *testing.T) {
	g := newFakeGuest(t, "")
	g.plant("manifest.ports", "libwidget\n")

	_, err := g.provider().Manifests(t.Context(), verify.Job{Provider: "ci", ID: "x"})
	require.ErrorIs(t, err, verify.ErrUnknownJob)

	_, err = g.provider().Manifests(t.Context(), verify.Job{Provider: "tart", ID: "dockhand-worker-gone"})
	require.ErrorIs(t, err, verify.ErrUnknownJob)

	_, err = g.provider().Probe(t.Context(), verify.Job{Provider: "ci", ID: "x"}, "libwidget")
	require.ErrorIs(t, err, verify.ErrUnknownJob)
}

// A port the job did not build is unknown rather than silent: a caller
// probing a member that is not in this cohort has asked about the wrong
// job, and the answer "no lines" would look like a port with no
// binaries.
func TestProbingAPortTheJobDidNotBuildIsRefused(t *testing.T) {
	g := newFakeGuest(t, "")
	g.plant("manifest.ports", "libwidget\n")

	_, err := g.provider().Probe(t.Context(), g.job(), "gdal")
	require.ErrorIs(t, err, verify.ErrUnknownJob)
}

// The probe runs the port's own programs and reports what they said,
// with the argv beside the output — because output with no visible
// provenance is not evidence.
func TestProbeRunsThePortsOwnBinaries(t *testing.T) {
	g := newFakeGuest(t, "")
	prog := g.program("widget", "#!/bin/sh\necho widget 3.0.0\n")
	g.setPort(`
  "-q contents libwidget") printf '  %s\n' ` + prog + ` /opt/local/share/doc/widget.1;;
`)
	g.plant("manifest.ports", "libwidget\n")

	lines, err := g.provider().Probe(t.Context(), g.job(), "libwidget")
	require.NoError(t, err)

	assert.Equal(t, []verify.ProbeLine{{
		Binary: prog,
		Argv:   prog + " --version",
		Output: "widget 3.0.0",
	}}, lines)
}

// A program that answers its usage instead of a version is still the
// evidence being asked for: the thing loads and runs. A program that
// waits for a person is killed rather than allowed to hold the
// environment.
func TestAProbeThatWillNotAnswerIsNotAProbeThatHangs(t *testing.T) {
	g := newFakeGuest(t, "")
	prog := g.program("sulky", "#!/bin/sh\necho 'sulky: unknown option --version' >&2\nexit 2\n")
	g.setPort(`
  "-q contents libwidget") printf '  %s\n' ` + prog + `;;
`)
	g.plant("manifest.ports", "libwidget\n")

	lines, err := g.provider().Probe(t.Context(), g.job(), "libwidget")
	require.NoError(t, err)

	require.Len(t, lines, 1)
	assert.Equal(t, "sulky: unknown option --version", lines[0].Output)
}
