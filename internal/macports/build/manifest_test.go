package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/verify"
)

// fixture is one of the captured manifests in testdata. They are real
// output from the script this package ships, run against a real
// MacPorts installation and against real Mach-O files — not transcribed
// and not hand-written, because every property under test here is a
// property of what otool actually prints.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(b)
}

func TestBaselineArgsAreBinaryOnlyAndCarryTheFrame(t *testing.T) {
	v, err := info.Variants("+quartz", "-x11")
	require.NoError(t, err)

	assert.Equal(t, []string{"-N", "-b", "install", "cairo", "+quartz", "-x11"},
		BaselineArgs("cairo", v))
	assert.Equal(t, []string{"-N", "-b", "install", "cairo"},
		BaselineArgs("cairo", info.VariantSet("")))
	assert.NotContains(t, BaselineArgs("cairo", info.VariantSet("")), "-s",
		"a baseline that could build from source is not a baseline")
	assert.NotContains(t, BaselineArgs("cairo", info.VariantSet("")), "-d",
		"the baseline is a measurement, not the artifact")
}

func TestUninstallIsForced(t *testing.T) {
	assert.Equal(t, []string{"-N", "-f", "uninstall", "cairo"}, UninstallArgs("cairo"))
}

// port(1)'s exit code cannot answer whether a port is installed: `port
// -q installed` on a port that is not exits 0 and prints nothing. So the
// answer is the output, and the absence of an answer is an error rather
// than an empty version.
func TestParseInstalled(t *testing.T) {
	for _, tc := range []struct {
		name, out, want string
		err             error
	}{
		{
			name: "the captured line, indent and all",
			out:  "  brotli @1.2.0_0 (active)\n",
			want: "1.2.0_0",
		},
		{
			name: "variants are part of what names the archive",
			out:  "  cairo @1.18.4_2+quartz+x11 (active)\n",
			want: "1.18.4_2+quartz+x11",
		},
		{
			name: "the active version is the one a dependent linked against",
			out:  "  libwidget @2.4.1_0\n  libwidget @3.0.0_0 (active)\n",
			want: "3.0.0_0",
		},
		{
			name: "installed and inactive is a real state",
			out:  "  libwidget @2.4.1_0\n",
			want: "2.4.1_0",
		},
		{
			name: "nothing at all is the not-installed answer",
			out:  "",
			err:  ErrNotInstalled,
		},
		{
			name: "and so is a line with no version on it",
			out:  "No ports are installed.\n",
			err:  ErrNotInstalled,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseInstalled(tc.out)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// A baseline that could not be taken must say which unavailability it
// was, in the environment's own words. The two quoted lines are
// MacPorts' own, from portunarchive.tcl and portarchivefetch.tcl.
func TestBaselineFailureQuotesTheLineThatRefused(t *testing.T) {
	for _, tc := range []struct{ name, out, want string }{
		{
			name: "no archive published for this frame",
			out: "--->  Fetching archive for libwidget\n" +
				"Error: Failed to unarchive libwidget: Archive for libwidget 2.4.1_0 not found, required when binary-only is set!\n" +
				"Error: See /opt/local/var/macports/logs/libwidget/main.log for details.\n",
			want: "Error: Failed to unarchive libwidget: Archive for libwidget 2.4.1_0 not found, required when binary-only is set!",
		},
		{
			name: "no archive site configured at all",
			out:  "--->  Fetching archive for libwidget\nError: Binary-only mode requested with no usable archive sites configured\n",
			want: "Error: Binary-only mode requested with no usable archive sites configured",
		},
		{
			name: "anything else falls back to the last thing said",
			out:  "--->  Computing dependencies\nError: Port libwidget not found\n\n",
			want: "Error: Port libwidget not found",
		},
		{
			name: "and silence is reported as silence",
			out:  "   \n\n",
			want: "the environment said nothing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, BaselineFailure(tc.out))
		})
	}
}

// The port name never reaches the script as syntax: it is read out of
// the roster file, the way an argv word is. Everything a Portfile could
// have written stays data.
func TestTheManifestScriptSpellsNoPortName(t *testing.T) {
	s := ManifestScript("/opt/local/bin/port", "/tmp/dockhand-verify/manifest.pre", "/tmp/dockhand-verify/manifest.ports", 3)

	assert.NotContains(t, s, "libwidget")
	assert.Contains(t, s, `while IFS= read -r line; do`)
	assert.Contains(t, s, `if [ "$n" -eq 3 ]`)
	assert.Contains(t, s, `-arch all -D`, "every slice of a universal file, always")
	assert.Contains(t, s, `-arch all -L`)
	assert.Contains(t, s, `xargs -0`, "otool is batched; a per-file loop is minutes")
	assert.Contains(t, s, `sed 's/^  //'`, "port contents indents, even under -q")
	assert.Contains(t, s, `.part && mv -f`, "a torn write parses as libraries that vanished")
}

// A frame that does not close is refused, and refused as itself: a
// capture cut off mid-file-list would otherwise parse into a manifest
// whose missing libraries read as the strongest possible ABI break.
func TestATruncatedCaptureIsRefusedByName(t *testing.T) {
	full := fixture(t, "manifest-brotli.txt")
	cut := full[:strings.Index(full, "===> dockhand manifest: links")]

	_, err := ParseManifest(cut)
	require.ErrorIs(t, err, ErrManifestTruncated)

	_, err = ParseManifest("--->  Computing dependencies for brotli\nError: something else entirely\n")
	require.ErrorIs(t, err, ErrNoManifest)
}

// The real capture of a real port. Three files announce one install
// name — the versioned dylib and both symlinks — which is the case that
// makes keying a manifest by path wrong: it would report three
// libraries where MacPorts installed one.
func TestParseManifestOverARealPort(t *testing.T) {
	got, err := ParseManifest(fixture(t, "manifest-brotli.txt"))
	require.NoError(t, err)

	assert.Equal(t, "brotli", got.Manifest.Port)
	assert.Equal(t, "1.2.0_0", got.Manifest.Version)
	assert.Equal(t, "26.6.2 arm64", got.Manifest.Platform)
	assert.Len(t, got.Manifest.Files, 23)
	assert.Equal(t, "/opt/local/bin/brotli", got.Manifest.Files[0])

	// Nine dylibs, three per logical library, and the executable is not
	// among them: `otool -D` on an executable prints its header and an
	// empty body, which is how a program is told from a library.
	assert.Len(t, got.Manifest.Dylibs, 9)
	for _, d := range got.Manifest.Dylibs {
		assert.NotEqual(t, "/opt/local/bin/brotli", d.Path,
			"an executable has no install name to announce")
		assert.Empty(t, d.Arch, "a thin file is one library and otool names no architecture")
	}
	assert.Equal(t, verify.Dylib{
		Path:           "/opt/local/lib/libbrotlicommon.1.2.0.dylib",
		InstallName:    "/opt/local/lib/libbrotlicommon.1.dylib",
		CompatVersion:  "1.0.0",
		CurrentVersion: "1.2.0",
	}, got.Manifest.Dylibs[0])

	var common []string
	for _, d := range got.Manifest.Dylibs {
		if d.InstallName == "/opt/local/lib/libbrotlicommon.1.dylib" {
			common = append(common, d.Path)
		}
	}
	assert.Equal(t, []string{
		"/opt/local/lib/libbrotlicommon.1.2.0.dylib",
		"/opt/local/lib/libbrotlicommon.1.dylib",
		"/opt/local/lib/libbrotlicommon.dylib",
	}, common, "one logical library, three files that announce it")
}

// The bindings the same sweep saw. The executable is in them even
// though it is not a library, because an executable breaks exactly as
// loudly when the library it named moves.
func TestTheCaptureCarriesWhoLinksToWhat(t *testing.T) {
	got, err := ParseManifest(fixture(t, "manifest-brotli.txt"))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"/opt/local/bin/brotli",
		"/opt/local/lib/libbrotlicommon.1.2.0.dylib",
		"/opt/local/lib/libbrotlicommon.1.dylib",
		"/opt/local/lib/libbrotlicommon.dylib",
		"/opt/local/lib/libbrotlidec.1.2.0.dylib",
		"/opt/local/lib/libbrotlidec.1.dylib",
		"/opt/local/lib/libbrotlidec.dylib",
		"/opt/local/lib/libbrotlienc.1.2.0.dylib",
		"/opt/local/lib/libbrotlienc.1.dylib",
		"/opt/local/lib/libbrotlienc.dylib",
	}, got.LinksTo["/opt/local/lib/libbrotlicommon.1.dylib"])
	assert.Equal(t, []string{"/opt/local/bin/brotli"},
		got.LinksTo["/opt/local/lib/libbrotlienc.1.dylib"][:1],
		"the program that links against the encoder is listed first, in file order")
	assert.NotEmpty(t, got.LinksTo["/usr/lib/libSystem.B.dylib"],
		"a binding outside the port is still a binding")
}

// A universal file is several libraries under one path, and they can
// disagree: this fixture's mixed dylib is a real lipo of a 2.0.0 x86_64
// slice onto a 3.0.0 arm64 one. Collapsing the slices would invent a
// measurement; a row per slice lets the disagreement be seen.
func TestParseManifestOverUniversalFiles(t *testing.T) {
	got, err := ParseManifest(fixture(t, "manifest-universal.txt"))
	require.NoError(t, err)

	assert.Equal(t, "2.4.1_0+universal", got.Manifest.Version)
	assert.Equal(t, []verify.Dylib{
		{Path: "/tmp/dhfat/lib/libwidget.2.4.1.dylib", Arch: "x86_64",
			InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
		{Path: "/tmp/dhfat/lib/libwidget.2.4.1.dylib", Arch: "arm64",
			InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
		{Path: "/tmp/dhfat/lib/libwidget.2.dylib", Arch: "x86_64",
			InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
		{Path: "/tmp/dhfat/lib/libwidget.2.dylib", Arch: "arm64",
			InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
		{Path: "/tmp/dhfat/lib/libwidget.mixed.dylib", Arch: "x86_64",
			InstallName: "/opt/local/lib/libwidget.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.4.1"},
		{Path: "/tmp/dhfat/lib/libwidget.mixed.dylib", Arch: "arm64",
			InstallName: "/opt/local/lib/libwidget.3.dylib", CompatVersion: "3.0.0", CurrentVersion: "3.0.0"},
		{Path: "/tmp/dhfat/lib/w-arm.dylib", Arch: "",
			InstallName: "/opt/local/lib/libwidget.3.dylib", CompatVersion: "3.0.0", CurrentVersion: "3.0.0"},
	}, got.Manifest.Dylibs)
}

// Two things the environment does that a parser must survive: a file
// listed by `port contents` that is not Mach-O is answered on STDOUT
// with "is not an object file", and a file that vanished between the
// listing and the sweep is answered on stderr — so it is simply absent,
// and otool exits non-zero while still printing everything good.
func TestTheSweepSurvivesWhatIsNotAMachO(t *testing.T) {
	got, err := ParseManifest(fixture(t, "manifest-universal.txt"))
	require.NoError(t, err)

	assert.Contains(t, got.Manifest.Files, "/tmp/dhfat/share/widget.pc")
	assert.Contains(t, got.Manifest.Files, "/tmp/dhfat/lib/gone.dylib")
	for _, d := range got.Manifest.Dylibs {
		assert.NotEqual(t, "/tmp/dhfat/share/widget.pc", d.Path)
		assert.NotEqual(t, "/tmp/dhfat/lib/gone.dylib", d.Path,
			"a file that vanished before the sweep is absent, not empty")
	}
	assert.Empty(t, got.LinksTo["/tmp/dhfat/share/widget.pc"])
}

// The before and after of the acceptance test's own scenario, both
// captured from real Mach-O files: the install name moves and the
// compatibility version with it.
func TestTheCapturedPairShowsTheInstallNameMoving(t *testing.T) {
	before, err := ParseManifest(fixture(t, "manifest-universal.txt"))
	require.NoError(t, err)
	after, err := ParseManifest(fixture(t, "manifest-universal-after.txt"))
	require.NoError(t, err)

	names := func(c Capture) map[string]string {
		out := map[string]string{}
		for _, d := range c.Manifest.Dylibs {
			out[d.InstallName] = d.CompatVersion
		}
		return out
	}
	assert.Equal(t, map[string]string{
		"/opt/local/lib/libwidget.2.dylib": "2.0.0",
		"/opt/local/lib/libwidget.3.dylib": "3.0.0",
	}, names(before))
	assert.Equal(t, map[string]string{
		"/opt/local/lib/libwidget.3.dylib": "3.0.0",
	}, names(after))
	assert.Equal(t, "3.0.0_0+universal", after.Manifest.Version)
}

// An install name and a header differ by one colon, and every install
// name a real port publishes is also one of its own files — so which
// lines are headers is decided against the file list the capture
// carries, never by shape.
func TestAnInstallNameIsNotMistakenForAHeader(t *testing.T) {
	got, err := ParseManifest(fixture(t, "manifest-brotli.txt"))
	require.NoError(t, err)

	// libbrotlicommon.1.dylib appears both as a header (a file that was
	// swept) and as a body line (the install name three files announce).
	// Read as a header the second time, the sweep would lose a library
	// and gain one whose path is an install name.
	assert.Len(t, got.Manifest.Dylibs, 9)
	for _, d := range got.Manifest.Dylibs {
		assert.Contains(t, got.Manifest.Files, d.Path,
			"every measured library is one of the files the port laid down")
	}
}

func TestProbeScriptAsksOnlyTheProgramsThePortLaidDown(t *testing.T) {
	s := ProbeScript("/opt/local/bin/port", "/tmp/dockhand-verify/probe.0", "/tmp/dockhand-verify/manifest.ports", 0, "/opt/local")

	assert.Contains(t, s, "case \"$f\" in /opt/local/bin/*|/opt/local/sbin/*)")
	assert.Contains(t, s, "--version")
	assert.Contains(t, s, "sleep 10", "a program that waits for a person must not hold the environment")
	assert.Contains(t, s, "[ \"$seen\" -le 5 ]")
	assert.NotContains(t, s, "libwidget")
}

func TestParseProbes(t *testing.T) {
	out := "===> dockhand probe: binary\n/opt/local/bin/brotli\n" +
		"===> dockhand probe: argv\n/opt/local/bin/brotli --version\n" +
		"===> dockhand probe: output\nbrotli 1.2.0\n" +
		"===> dockhand probe: binary\n/opt/local/bin/widget\n" +
		"===> dockhand probe: argv\n/opt/local/bin/widget --version\n" +
		"===> dockhand probe: output\nwidget: unknown option --version\nusage: widget [-v]\n" +
		"===> dockhand probe: done\n"

	assert.Equal(t, []verify.ProbeLine{
		{Binary: "/opt/local/bin/brotli", Argv: "/opt/local/bin/brotli --version", Output: "brotli 1.2.0"},
		{Binary: "/opt/local/bin/widget", Argv: "/opt/local/bin/widget --version",
			Output: "widget: unknown option --version\nusage: widget [-v]"},
	}, ParseProbes(out))

	assert.Empty(t, ParseProbes(""))
}

// A probe sweep is corroboration, not a measurement, so half of one is
// half the corroboration — the opposite of the manifest's rule, and
// deliberately so.
func TestATruncatedProbeSweepKeepsWhatItGot(t *testing.T) {
	out := "===> dockhand probe: binary\n/opt/local/bin/brotli\n" +
		"===> dockhand probe: argv\n/opt/local/bin/brotli --version\n" +
		"===> dockhand probe: output\nbrotli 1.2.0\n"

	assert.Equal(t, []verify.ProbeLine{
		{Binary: "/opt/local/bin/brotli", Argv: "/opt/local/bin/brotli --version", Output: "brotli 1.2.0"},
	}, ParseProbes(out))
}
