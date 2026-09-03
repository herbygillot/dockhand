package verdict

// The rows in this file are transcribed from real captures, not
// invented. Every install name and every compatibility version below
// appears verbatim in internal/macports/build/testdata — the output of
// build.ManifestScript run against a real MacPorts prefix and real
// Mach-O files — and TestTheCapturesStillSayWhatTheseRowsTranscribe
// reads those files back and fails if a transcription drifts from
// them.
//
// They are transcribed rather than parsed here on purpose. This package
// judges values and cannot reach the parser, which is the property the
// import list enforces; carrying the rows by hand is what that costs,
// and the guard test is what keeps the cost honest.

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// capturesDir is the one copy of the captured manifests, in the package
// that parses them. It is read from here rather than copied, for the
// reason the log corpus is: two copies drift the first time someone
// replaces a reconstruction with a capture.
const capturesDir = "../macports/build/testdata"

// The before side of the acceptance test's own scenario, as
// manifest-universal.txt captured it: a universal libwidget at 2.4.1
// whose two slices agree, announcing /opt/local/lib/libwidget.2.dylib
// with compatibility version 2.0.0.
func widgetBefore() *verify.Manifest {
	return &verify.Manifest{
		Port: "libwidget", Version: "2.4.1_0+universal", Platform: "26.6.2 arm64",
		Dylibs: []verify.Dylib{
			{Path: "/tmp/dhfat/lib/libwidget.2.4.1.dylib", Arch: "x86_64",
				InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
			{Path: "/tmp/dhfat/lib/libwidget.2.4.1.dylib", Arch: "arm64",
				InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
			{Path: "/tmp/dhfat/lib/libwidget.2.dylib", Arch: "x86_64",
				InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
			{Path: "/tmp/dhfat/lib/libwidget.2.dylib", Arch: "arm64",
				InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
		},
	}
}

// The after side, from manifest-universal-after.txt: the same port at
// 3.0.0, announcing libwidget.3.dylib at compatibility version 3.0.0.
func widgetAfter() *verify.Manifest {
	return &verify.Manifest{
		Port: "libwidget", Version: "3.0.0_0+universal", Platform: "26.6.2 arm64",
		Dylibs: []verify.Dylib{
			{Path: "/tmp/dhfat3/lib/libwidget.3.0.0.dylib", Arch: "x86_64",
				InstallName: "/opt/local/lib/libwidget.3.dylib", CompatVersion: "3.0.0", CurrentVersion: "3.0.0"},
			{Path: "/tmp/dhfat3/lib/libwidget.3.0.0.dylib", Arch: "arm64",
				InstallName: "/opt/local/lib/libwidget.3.dylib", CompatVersion: "3.0.0", CurrentVersion: "3.0.0"},
			{Path: "/tmp/dhfat3/lib/libwidget.3.dylib", Arch: "x86_64",
				InstallName: "/opt/local/lib/libwidget.3.dylib", CompatVersion: "3.0.0", CurrentVersion: "3.0.0"},
			{Path: "/tmp/dhfat3/lib/libwidget.3.dylib", Arch: "arm64",
				InstallName: "/opt/local/lib/libwidget.3.dylib", CompatVersion: "3.0.0", CurrentVersion: "3.0.0"},
		},
	}
}

// brotli's three files per library, from manifest-brotli.txt. The three
// libbrotlicommon files all announce the one install name, and the
// executable /opt/local/bin/brotli is absent because otool -D prints
// nothing for a program.
func brotli() *verify.Manifest {
	m := &verify.Manifest{Port: "brotli", Version: "1.2.0_0", Platform: "26.6.2 arm64"}
	for _, lib := range []string{"libbrotlicommon", "libbrotlidec", "libbrotlienc"} {
		name := "/opt/local/lib/" + lib + ".1.dylib"
		for _, suffix := range []string{".1.2.0.dylib", ".1.dylib", ".dylib"} {
			m.Dylibs = append(m.Dylibs, verify.Dylib{
				Path:        "/opt/local/lib/" + lib + suffix,
				InstallName: name, CompatVersion: "1.0.0", CurrentVersion: "1.2.0",
			})
		}
	}
	return m
}

// measured is the ordinary input: an environment that answered, a
// baseline unpacked from a binary archive, a branch built from source.
func measured(before, after *verify.Manifest) ABIInput {
	return ABIInput{
		Port: "libwidget", Portdir: "devel/libwidget", Described: true, FromSource: true,
		Manifests: verify.Manifests{
			Baseline: before, BaselineSource: verify.BaselineArchive, Installed: after,
		},
	}
}

func TestLogicalLibraryStripsOnlyNumericComponents(t *testing.T) {
	// Every left-hand side is an install name read off this machine's
	// own prefix with otool -D, and the right-hand side is what the
	// dependents of that library have in common across versions.
	for name, want := range map[string]string{
		"/opt/local/lib/libwidget.2.dylib":        "libwidget",
		"/opt/local/lib/libFLAC.14.dylib":         "libFLAC",
		"/opt/local/lib/libintl.8.dylib":          "libintl",
		"/opt/local/lib/libbrotlicommon.1.dylib":  "libbrotlicommon",
		"/opt/local/lib/libImath-3_2.30.dylib":    "libImath-3_2",
		"/opt/local/lib/libLASi.2.dylib":          "libLASi",
		"/opt/local/lib/libbrotlidec.1.2.0.dylib": "libbrotlidec",
		// The base's own trailing digit survives, because it is not a
		// dot-separated component: libxml2 is the library's name.
		"/opt/local/lib/libxml2.16.dylib": "libxml2",
		// A component that is not all digits stops the strip, so a
		// Q16 → Q17 move reads as the break it is.
		"/opt/local/lib/libMagickCore-6.Q16.7.dylib": "libMagickCore-6.Q16",
		"/opt/local/lib/libMagick++-6.Q16.9.dylib":   "libMagick++-6.Q16",
		// libpcap's major is a letter. A → B is a removal and an
		// addition rather than a rename, which is still a break and is
		// not a guessed pairing.
		"/usr/lib/libpcap.A.dylib": "libpcap.A",
		// The file says p11-kit-proxy and the linker says libp11-kit.
		// Only the linker's answer is what dependents recorded.
		"/opt/local/lib/libp11-kit.0.dylib": "libp11-kit",
	} {
		assert.Equal(t, want, LogicalLibrary(name), "logical library of %s", name)
	}
}

func TestARenamedInstallNameIsABreakAndTheCriterionSaysBetweenWhat(t *testing.T) {
	a := ABIDelta(measured(widgetBefore(), widgetAfter()))

	require.Equal(t, ABIChanged, a.Verdict)
	assert.True(t, a.Broken())
	require.Len(t, a.Changes, 2, "the rename, and the compatibility version that came with it")

	assert.Equal(t, InstallNameMoved, a.Changes[0].Kind)
	assert.True(t, a.Changes[0].Break)
	assert.Equal(t, "/opt/local/lib/libwidget.2.dylib", a.Changes[0].Before)
	assert.Equal(t, "/opt/local/lib/libwidget.3.dylib", a.Changes[0].After)

	// The compatibility version went UP, which is not a load-time break:
	// dyld requires the loaded library's to be at least what the
	// dependent recorded. The break stands on the rename, and the widen
	// is a true observation beside it.
	assert.Equal(t, CompatWidened, a.Changes[1].Kind)
	assert.False(t, a.Changes[1].Break, "an increase is not a break; it is reported")

	// The criterion is quoted verbatim into a commit body and a pull
	// request, so it says what moved AND between which two builds — the
	// provenance is what makes "nothing moved" mean anything, because a
	// baseline taken after the staging measures a change against itself
	// and always agrees.
	assert.Equal(t,
		"install name /opt/local/lib/libwidget.2.dylib → /opt/local/lib/libwidget.3.dylib; "+
			"libwidget compatibility_version 2.0.0 → 3.0.0 (widened), "+
			"measured between libwidget@2.4.1_0+universal (binary archive) and @3.0.0_0+universal (built from source) on 26.6.2 arm64",
		a.Criterion)

	f := a.Finding()
	assert.Equal(t, "abi-change", f.Kind)
	assert.Equal(t, []string{"libwidget"}, f.Ports)
	assert.Equal(t, a.Criterion, f.Criterion)
	// A measurement is not a question anybody can answer, and a Proposed
	// finding parks publication at the machine gate. The proposal that
	// rests on this measurement is a separate finding.
	assert.Equal(t, record.Accepted, f.Disposition)
	assert.True(t, f.At.IsZero(), "a judgment has no clock; the caller stamps it")
}

// The disagreeing universal file from the same capture: a real lipo of
// a 2.0.0 x86_64 slice onto a 3.0.0 arm64 one. It is a function rather
// than two rows inline in its test so the capture guard below covers it
// like every other transcription.
func widgetMixed() []verify.Dylib {
	return []verify.Dylib{
		{Path: "/tmp/dhfat/lib/libwidget.mixed.dylib", Arch: "x86_64",
			InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
		{Path: "/tmp/dhfat/lib/libwidget.mixed.dylib", Arch: "arm64",
			InstallName: "/opt/local/lib/libwidget.3.dylib", CompatVersion: "3.0.0", CurrentVersion: "3.0.0"},
	}
}

func TestAFatFileWhoseSlicesDisagreeIsNotMeasured(t *testing.T) {
	// One path, two install names, two compatibility versions — and no
	// arch's answer is more true than the other's.
	before := widgetBefore()
	before.Dylibs = append(before.Dylibs, widgetMixed()...)

	a := ABIDelta(measured(before, widgetAfter()))

	un := a.Unmeasured()
	require.Len(t, un, 1)
	assert.Equal(t, "/tmp/dhfat/lib/libwidget.mixed.dylib", un[0].Subject)
	assert.False(t, un[0].Break, "nothing was measured, so nothing was disproved")
	assert.Contains(t, un[0].String(), "arm64 /opt/local/lib/libwidget.3.dylib (3.0.0)")
	assert.Contains(t, un[0].String(), "x86_64 /opt/local/lib/libwidget.2.dylib (2.0.0)")
	assert.Contains(t, a.Criterion, "not measured", "what could not be looked at is in the sentence")
}

func TestThreeFilesAreOneLogicalLibrary(t *testing.T) {
	t.Run("an unchanged install is unchanged", func(t *testing.T) {
		a := ABIDelta(measured(brotli(), brotli()))

		assert.Equal(t, ABIUnchanged, a.Verdict)
		assert.False(t, a.Broken())
		assert.Empty(t, a.Changes)
		assert.Contains(t, a.Criterion, "no install name, compatibility version or library moved")
		assert.Equal(t, "abi-unchanged", a.Finding().Kind)
	})

	t.Run("an ordinary upgrade renames files and not libraries", func(t *testing.T) {
		// brotli 1.2.0 → 1.3.0: every versioned FILE is renamed, the
		// unversioned symlink is rebuilt, and all three still announce
		// the one install name /opt/local/lib/libbrotlicommon.1.dylib.
		// Keyed by path this is six libraries removed and six added — a
		// total, fabricated ABI break on an upgrade that moved nothing a
		// dependent binds to.
		after := brotli()
		after.Version = "1.3.0_0"
		for i := range after.Dylibs {
			after.Dylibs[i].Path = strings.Replace(after.Dylibs[i].Path, ".1.2.0.", ".1.3.0.", 1)
			after.Dylibs[i].CurrentVersion = "1.3.0"
		}

		a := ABIDelta(measured(brotli(), after))
		assert.Equal(t, ABIUnchanged, a.Verdict)
		assert.Empty(t, a.Changes)
	})

	t.Run("files that disagree about one name are not compared", func(t *testing.T) {
		// Three files announce libbrotlicommon.1.dylib. If they ever
		// disagreed about its compatibility version, taking whichever the
		// capture listed last would make the answer depend on a file
		// ordering — and half the readings of that are a break that is
		// not there.
		odd := brotli()
		odd.Dylibs[2].CompatVersion = "2.0.0"

		a := ABIDelta(measured(odd, brotli()))
		require.Len(t, a.Unmeasured(), 1)
		assert.Equal(t, "/opt/local/lib/libbrotlicommon.1.dylib", a.Unmeasured()[0].Subject)
		assert.Contains(t, a.Criterion, "two compatibility versions, 1.0.0 and 2.0.0")
		assert.Equal(t, ABIUnchanged, a.Verdict, "nothing was measured about it, so nothing was disproved")
	})

	t.Run("a symlink that vanishes is not a library that vanished", func(t *testing.T) {
		after := brotli()
		after.Dylibs = after.Dylibs[1:] // /opt/local/lib/libbrotlicommon.1.2.0.dylib

		a := ABIDelta(measured(brotli(), after))
		assert.Equal(t, ABIUnchanged, a.Verdict)
		assert.Empty(t, a.Changes, "the other two files announce the same install name")
	})
}

func TestALibraryThatGainsAMajorIsNotABreak(t *testing.T) {
	// xorg-libXaw publishes libXaw.6.dylib and libXaw.7.dylib side by
	// side, from the one port — confirmed with `port provides`. Both
	// strip to the logical name libXaw, and a map keyed by that name
	// with last-write-wins would report a rename on every rebuild and
	// propose revbumping every X11 dependent in the tree.
	one := &verify.Manifest{Port: "xorg-libXaw", Version: "1.0.16_0", Platform: "26.6.2 arm64",
		Dylibs: []verify.Dylib{{Path: "/opt/local/lib/libXaw6.6.dylib",
			InstallName: "/opt/local/lib/libXaw.6.dylib", CompatVersion: "6.0.0", CurrentVersion: "6.0.0"}}}
	both := &verify.Manifest{Port: "xorg-libXaw", Version: "1.0.17_0", Platform: "26.6.2 arm64",
		Dylibs: []verify.Dylib{
			{Path: "/opt/local/lib/libXaw6.6.dylib",
				InstallName: "/opt/local/lib/libXaw.6.dylib", CompatVersion: "6.0.0", CurrentVersion: "6.0.0"},
			{Path: "/opt/local/lib/libXaw7.7.dylib",
				InstallName: "/opt/local/lib/libXaw.7.dylib", CompatVersion: "7.0.0", CurrentVersion: "7.0.0"}}}

	t.Run("both sides carry both", func(t *testing.T) {
		a := ABIDelta(measured(both, both))
		assert.Equal(t, ABIUnchanged, a.Verdict)
		assert.Empty(t, a.Changes)
	})
	t.Run("a major is added", func(t *testing.T) {
		a := ABIDelta(measured(one, both))
		assert.Equal(t, ABIUnchanged, a.Verdict, "no dependent can have recorded a name that did not exist")
		assert.Empty(t, a.Changes)
	})
	t.Run("a major is dropped", func(t *testing.T) {
		a := ABIDelta(measured(both, one))
		require.Len(t, a.Changes, 1)
		assert.Equal(t, LibraryRemoved, a.Changes[0].Kind)
		assert.True(t, a.Changes[0].Break)
		assert.Equal(t, "/opt/local/lib/libXaw.7.dylib removed", a.Changes[0].String())
	})
}

func TestCompatibilityVersionNarrowsAndWidens(t *testing.T) {
	at := func(compat string) *verify.Manifest {
		return &verify.Manifest{Port: "openexr", Version: "3.2.4_0", Platform: "26.6.2 arm64",
			Dylibs: []verify.Dylib{{Path: "/opt/local/lib/libImath-3_2.30.3.2.2.dylib",
				InstallName: "/opt/local/lib/libImath-3_2.30.dylib", CompatVersion: compat, CurrentVersion: "30.3.2"}}}
	}

	t.Run("backwards is a break", func(t *testing.T) {
		a := ABIDelta(measured(at("30.0.0"), at("29.0.0")))
		require.Len(t, a.Changes, 1)
		assert.Equal(t, CompatNarrowed, a.Changes[0].Kind)
		assert.True(t, a.Changes[0].Break, "a dependent that recorded 30.0.0 no longer loads")
		assert.Equal(t, ABIChanged, a.Verdict)
		assert.Equal(t, "libImath-3_2 compatibility_version 30.0.0 → 29.0.0 (narrowed)", a.Changes[0].String())
	})
	t.Run("forwards is not", func(t *testing.T) {
		// The comparison is numeric, and this pair is why it has to be:
		// "30.0.0" sorts before "4.0.0" as text, so a string comparison
		// would call this ordinary upgrade a break.
		a := ABIDelta(measured(at("4.0.0"), at("30.0.0")))
		require.Len(t, a.Changes, 1)
		assert.Equal(t, CompatWidened, a.Changes[0].Kind)
		assert.False(t, a.Changes[0].Break)
		assert.Equal(t, ABIUnchanged, a.Verdict)
	})
	t.Run("a version that cannot be ordered is not compared", func(t *testing.T) {
		a := ABIDelta(measured(at("1.0.0"), at("1.0.0b")))
		require.Len(t, a.Changes, 1)
		assert.Equal(t, Unmeasurable, a.Changes[0].Kind)
		assert.False(t, a.Changes[0].Break, "unreadable is not broken")
		assert.Equal(t, ABIUnchanged, a.Verdict)
		assert.Contains(t, a.Criterion, "not a version this check can order")
	})
}

func TestAnRpathInstallNameIsNotComparable(t *testing.T) {
	// The one @rpath library in this machine's own prefix carries a
	// build hash in its name — librustc_driver-a9b31f558d66d404.dylib —
	// which moves on every build. Compared, it would report a removal
	// and an addition forever and propose a cohort for rust every time.
	at := func(hash string) *verify.Manifest {
		return &verify.Manifest{Port: "rust", Version: "1.89.0_0", Platform: "26.6.2 arm64",
			Dylibs: []verify.Dylib{{Path: "/opt/local/lib/rustlib/librustc_driver-" + hash + ".dylib",
				InstallName: "@rpath/librustc_driver-" + hash + ".dylib", CompatVersion: "0.0.0", CurrentVersion: "0.0.0"}}}
	}

	a := ABIDelta(measured(at("a9b31f558d66d404"), at("0c3e1f2b7d5a4e91")))

	// Every library on the before side was declined, so the comparison
	// made no comparison. "Unchanged" here would be a check that read
	// nothing published as a check that found nothing — the same
	// substitution the missing-baseline case refuses, pointing the other
	// way — and it is the answer a cohort decline would then quote.
	assert.Equal(t, ABIUnavailable, a.Verdict, "nothing was measured, which is not the same as nothing having moved")
	assert.False(t, a.Broken())
	require.Len(t, a.Unmeasured(), 2, "one per side; neither was compared")
	assert.Contains(t, a.Unmeasured()[0].String(), "resolved by the dependent")
	assert.Contains(t, a.Criterion, "could be compared")
	assert.Contains(t, a.Criterion, "resolved by the dependent", "and it names what it could not read")
}

func TestAnExecutableIsNoLibrary(t *testing.T) {
	// otool -D on a program prints its header and nothing else, so the
	// capture leaves it out. A row that arrives with an empty install
	// name anyway describes nothing to compare.
	m := brotli()
	m.Dylibs = append(m.Dylibs, verify.Dylib{Path: "/opt/local/bin/brotli"})

	a := ABIDelta(measured(m, brotli()))
	assert.Equal(t, ABIUnchanged, a.Verdict)
	assert.Empty(t, a.Changes)
}

func TestABIUnavailableTellsTheThreeAbsencesApart(t *testing.T) {
	// Each absence has a different remedy, so a finding that said only
	// "unavailable" would leave a reader to pick one.
	t.Run("no manifester", func(t *testing.T) {
		a := ABIDelta(ABIInput{Port: "libwidget", Portdir: "devel/libwidget"})
		assert.Equal(t, ABIUnavailable, a.Verdict)
		assert.False(t, a.Broken())
		assert.Contains(t, a.Criterion, "cannot describe an installation")
		assert.Equal(t, "abi-unavailable", a.Finding().Kind)
	})
	t.Run("nothing was installed", func(t *testing.T) {
		a := ABIDelta(ABIInput{Port: "libwidget", Portdir: "devel/libwidget", Described: true,
			Manifests: verify.Manifests{Baseline: widgetBefore(), BaselineSource: verify.BaselineArchive}})
		assert.Equal(t, ABIUnavailable, a.Verdict)
		assert.Contains(t, a.Criterion, "no installation to measure")
	})
	t.Run("no baseline, in the environment's own words, and no remedy that does not exist", func(t *testing.T) {
		a := ABIDelta(ABIInput{Port: "libwidget", Portdir: "devel/libwidget", Described: true,
			Manifests: verify.Manifests{
				Installed:      widgetAfter(),
				BaselineSource: verify.BaselineNone,
				BaselineReason: "Error: Failed to unarchive libwidget: Archive for libwidget 2.4.1_0 not found, required when binary-only is set!",
			}})
		assert.Equal(t, ABIUnavailable, a.Verdict)
		assert.Contains(t, a.Criterion, "no baseline for libwidget")
		assert.Contains(t, a.Criterion, "required when binary-only is set", "MacPorts' own refusal, verbatim")
		assert.Contains(t, a.Criterion, "nothing banks one yet",
			"what the comparison needed, and the fact that no verb produces it")
		// The sentence this replaced sent a reader to `dockhand verify
		// <portdir> on the primary branch`, which banks nothing: there is
		// no bank, no reader for one and no writer, so they would have met
		// the identical refusal afterwards with no way to tell they had
		// been sent in a circle.
		assert.NotContains(t, a.Criterion, "banks one`")
		assert.NotContains(t, a.Criterion, "dockhand verify")
	})
	t.Run("a refusal still names the port the environment reported", func(t *testing.T) {
		// The port rides the criterion and the cohort's own decline
		// sentence. A refusal that reached this package before anybody
		// filled the field in should read as a refusal about libwidget,
		// not as a tool that has lost track of what it was doing.
		a := ABIDelta(ABIInput{Described: true, Portdir: "devel/libwidget",
			Manifests: verify.Manifests{Installed: widgetAfter(), BaselineSource: verify.BaselineNone}})
		assert.Equal(t, "libwidget", a.Port)
		assert.Contains(t, a.Criterion, "no baseline for libwidget")
	})
	t.Run("a banked source with no banked value is its own refusal", func(t *testing.T) {
		// A different bug from a missing archive, and it must not read as
		// one: "banked" says a measurement was already taken and kept, and
		// a nil beside it says the caller that claimed it never handed the
		// value over. The fix is in this repository, not on a mirror.
		a := ABIDelta(ABIInput{Port: "libwidget", Portdir: "devel/libwidget", Described: true,
			Manifests: verify.Manifests{Installed: widgetAfter(), BaselineSource: verify.BaselineBanked}})
		assert.Equal(t, ABIUnavailable, a.Verdict)
		assert.Contains(t, a.Criterion, "recorded as banked and no banked manifest was supplied")
		assert.NotContains(t, a.Criterion, "no baseline for libwidget",
			"the generic sentence would send a reader looking for an archive that is not the problem")
	})
	t.Run("an empty baseline is never read as every library removed", func(t *testing.T) {
		// The strongest false break available: a capture that was cut
		// off, or a baseline that was never taken, compares as a total
		// wipe. It has to be refused by name instead.
		a := ABIDelta(ABIInput{Port: "libwidget", Described: true,
			Manifests: verify.Manifests{
				Baseline: &verify.Manifest{Port: "libwidget"}, BaselineSource: verify.BaselineNone,
				Installed: widgetAfter(),
			}})
		assert.Equal(t, ABIUnavailable, a.Verdict)
		assert.Empty(t, a.Changes)
	})
}

func TestWhatTheMeasurementCannotSeeTravelsWithIt(t *testing.T) {
	// An install name and a compatibility version are what otool can be
	// asked about, and they are not the whole ABI. The sentence saying so
	// lives here, once, so the commit body and the pull request quote it
	// rather than each rewording it — and so a reader weighing a proposal
	// reads it where the proposal is.
	assert.Contains(t, ABILimits, "symbols are removed")
	assert.Contains(t, ABILimits, "invisible to otool")

	assert.Equal(t, ABILimits, ABIDelta(measured(widgetBefore(), widgetAfter())).Limits())
	assert.Equal(t, ABILimits, ABIDelta(measured(brotli(), brotli())).Limits(),
		"an unchanged reading is the one a reader is likeliest to over-read")
	// A check that could not run has nothing to be insufficient about;
	// its criterion already says nothing was measured.
	assert.Empty(t, ABIDelta(ABIInput{Port: "libwidget"}).Limits())
}

func TestABankedBaselineIsANamedProvenanceOfItsOwn(t *testing.T) {
	in := measured(widgetBefore(), widgetAfter())
	in.Manifests.BaselineSource = verify.BaselineBanked
	assert.Contains(t, ABIDelta(in).Criterion, "(banked manifest)")
}

func TestTheCapturesStillSayWhatTheseRowsTranscribe(t *testing.T) {
	// The rows above are hand-carried because this package may not
	// import the parser. This is what keeps that honest: if a capture is
	// retaken and otool says something else, the transcription fails here
	// rather than agreeing with itself forever.
	//
	// It derives what to look for from the rows, and not from a list of
	// strings somebody has to remember to extend. Every field of every
	// row is checked, in the two shapes otool printed it: `otool -D`'s
	// path header with the install name on the line under it — which is
	// what pins a name to a file, and the reason three brotli files can
	// be shown to be one library — and `otool -L`'s line carrying both
	// version fields.
	for file, rows := range map[string][]verify.Dylib{
		"manifest-universal.txt":       append(widgetBefore().Dylibs, widgetMixed()...),
		"manifest-universal-after.txt": widgetAfter().Dylibs,
		"manifest-brotli.txt":          brotli().Dylibs,
	} {
		raw, err := os.ReadFile(capturesDir + "/" + file)
		require.NoError(t, err, "the captures live in %s", capturesDir)
		text := string(raw)
		require.Contains(t, text, "===> dockhand manifest: end",
			"%s was cut off, and a truncated manifest is refused by name rather than parsed", file)

		for _, d := range rows {
			header := d.Path
			if d.Arch != "" {
				header += " (architecture " + d.Arch + ")"
			}
			assert.Contains(t, text, header+":\n"+d.InstallName+"\n",
				"%s: otool -D no longer announces %s under %s", file, d.InstallName, header)
			assert.Contains(t, text, "\t"+d.InstallName+
				" (compatibility version "+d.CompatVersion+", current version "+d.CurrentVersion+")",
				"%s: otool -L no longer says this about %s", file, d.InstallName)
		}
	}

	// The two things no Dylib row carries and the criterion quotes: the
	// archive-naming version string — version, revision and variants,
	// because that and not the bare version identifies a build — and the
	// platform in the environment's own words.
	for file, wants := range map[string][]string{
		"manifest-universal.txt":       {"libwidget @2.4.1_0+universal (active)", "26.6.2 arm64"},
		"manifest-universal-after.txt": {"libwidget @3.0.0_0+universal (active)", "26.6.2 arm64"},
		"manifest-brotli.txt":          {"brotli @1.2.0_0 (active)", "26.6.2 arm64"},
	} {
		raw, err := os.ReadFile(capturesDir + "/" + file)
		require.NoError(t, err, "the captures live in %s", capturesDir)
		for _, want := range wants {
			assert.Contains(t, string(raw), want, "%s no longer says this", file)
		}
	}
}

// A manifest that parsed, listed files, and describes no library at
// all. One side like that is not a comparison, and concluding from it
// is a confident verdict in whichever direction the empty side points.
func TestOneSideWithNoLibraryIsRefusedRatherThanConcluded(t *testing.T) {
	// The guard upstream only rejects a capture with no version AND no
	// files, so a sweep whose otool sections came back empty — otool
	// unusable in the guest, registry paths that are not on disk, a
	// batch cut short, all of it with stderr sent to /dev/null — arrives
	// here looking like a complete answer.
	empty := &verify.Manifest{Port: "libwidget", Version: "2.4.1_0", Platform: "Sequoia",
		Files: []string{"/opt/local/lib/libwidget.2.dylib", "/opt/local/share/doc/widget"}}

	t.Run("an empty before compares as nothing moved", func(t *testing.T) {
		// The dangerous direction, and the counter-intuitive one: only the
		// libraries the BEFORE side published are asked about, so an empty
		// before is a confident all-clear over a measurement that never
		// happened.
		a := ABIDelta(ABIInput{Port: "libwidget", Portdir: "devel/libwidget", Described: true,
			Manifests: verify.Manifests{Baseline: empty, BaselineSource: verify.BaselineArchive,
				Installed: widgetAfter()}})
		assert.Equal(t, ABIUnavailable, a.Verdict)
		assert.Contains(t, a.Criterion, "describes 2 files and no library")
		assert.NotContains(t, a.Criterion, "no install name, compatibility version or library moved")
	})

	t.Run("an empty after compares as every library removed", func(t *testing.T) {
		a := ABIDelta(ABIInput{Port: "libwidget", Portdir: "devel/libwidget", Described: true,
			Manifests: verify.Manifests{Baseline: widgetBefore(), BaselineSource: verify.BaselineArchive,
				Installed: empty}})
		assert.Equal(t, ABIUnavailable, a.Verdict)
		assert.Contains(t, a.Criterion, "would read as removed")
		assert.False(t, a.Broken(), "and it proposes nothing, where a removal would propose the whole cohort")
	})

	t.Run("two installations with no library at all still compare", func(t *testing.T) {
		// A port with no dylibs is a real port, and "nothing moved" is the
		// true answer for it. The refusal above is about DISAGREEMENT
		// between the sides, not about emptiness.
		a := ABIDelta(ABIInput{Port: "widget-doc", Described: true,
			Manifests: verify.Manifests{Baseline: empty, BaselineSource: verify.BaselineArchive,
				Installed: empty}})
		assert.Equal(t, ABIUnchanged, a.Verdict)
	})
}

// The after side names its provenance too, or says it was not recorded.
//
// The before side always stated where it came from; the after side
// stated it only when a request flag said the build was from source —
// and that flag is false for a plain `dockhand bump`, the case this
// whole check was written for, even though the guest genuinely compiles
// because the new version names an archive that does not exist. A
// reader was left to assume the second half of the sentence.
func TestBothHalvesOfTheCriterionNameTheirSource(t *testing.T) {
	in := measured(widgetBefore(), widgetAfter())
	in.FromSource = true
	assert.Contains(t, ABIDelta(in).Criterion, "(binary archive) and @3.0.0_0+universal (built from source)")

	in.FromSource = false
	assert.Contains(t, ABIDelta(in).Criterion, "(binary archive) and @3.0.0_0+universal (source not recorded)",
		"an assumed provenance and a stated absence are different things to a reviewer, and only one can be noticed")
}
