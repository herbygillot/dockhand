package macports

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// The evaluator's facts are read as their types, and an unread or failed
// one is told apart from a false one (the code-organization review's
// finding 27).
func TestTheEvaluatorsFactsAreReadAsTheirTypes(t *testing.T) {
	t.Parallel()
	port := PortInfo{Options: map[string]string{"dockhand.metadata_only": "1", "dockhand.livecheck_standard": "0", "dockhand.test_run": "0", "dockhand.portgroups": "github {cmake}",
		"fetch.has_credentials": "0", "fetch.archive_compatible": "1", "dockhand.base_version": "2.12.6"}}
	only, err := port.MetadataOnly()
	require.NoError(t, err)
	require.True(t, only)
	standard, err := port.LivecheckStandard()
	require.NoError(t, err)
	require.False(t, standard)
	declares, known := port.DeclaresTests()
	require.Equal(t, [2]bool{false, true}, [2]bool{declares, known})
	groups, known := port.PortGroups()
	require.True(t, known)
	require.Equal(t, []string{"github", "cmake"}, groups)
	credentials, err := port.FetchCredentials()
	require.NoError(t, err)
	require.False(t, credentials)
	compatible, problem, err := port.ArchiveCompatible()
	require.NoError(t, err)
	require.True(t, compatible)
	require.Empty(t, problem)
	base, ok := port.BaseVersion()
	require.Equal(t, "2.12.6", base)
	require.True(t, ok)

	unread := PortInfo{Options: map[string]string{}, OptionErrors: map[string]string{"dockhand.metadata_only": "cannot tell whether the port builds anything: boom"}}
	_, err = unread.MetadataOnly()
	require.ErrorContains(t, err, "cannot tell whether the port builds anything")
	_, known = unread.DeclaresTests()
	require.False(t, known)
	_, known = unread.PortGroups()
	require.False(t, known)
	_, err = unread.FetchCredentials()
	require.Error(t, err, "not evaluated isn't no credentials")
	_, _, err = unread.ArchiveCompatible()
	require.Error(t, err, "not assessed isn't compatible")
	_, ok = unread.BaseVersion()
	require.False(t, ok)

	custom := PortInfo{Options: map[string]string{"fetch.archive_compatible": "0", "dockhand.base_version": "2.12.6"}, Fetch: &FetchSemantics{Kind: "custom", Problem: "pre-fetch hook 1 writes distfiles"}}
	compatible, problem, err = custom.ArchiveCompatible()
	require.NoError(t, err)
	require.False(t, compatible)
	require.Equal(t, "MacPorts Base 2.12.6: pre-fetch hook 1 writes distfiles", problem)

	require.True(t, IsLivecheckOption("livecheck.regex"))
	require.True(t, IsLivecheckOption("dockhand.livecheck_standard"))
	require.False(t, IsLivecheckOption("dockhand.metadata_only"))
}

// A stub whose probe failed is said, not taken for an ordinary port that
// builds, which fidelity then refused with a misleading reason.
func TestAStubWhoseProbeFailedIsSaid(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{Ports: map[string]PortInfo{
		"py-requests":    {Name: "py-requests", Version: "2.32.0", OptionErrors: map[string]string{"dockhand.metadata_only": "cannot tell whether the port builds anything: boom"}},
		"py314-requests": {Name: "py314-requests", Version: "2.32.0", Options: map[string]string{"dockhand.metadata_only": "0"}},
	}}
	_, _, err := ResolveStub(snapshot, model.Target{Name: "py-requests"})
	require.ErrorContains(t, err, "can't tell whether py-requests builds anything")
}

// A port's plain-HTTP URLs are its homepage, its livecheck.url, and its
// master_sites that are URLs, tags taken off as Base takes them, and never
// a mirror group, which is MacPorts' own; MacPorts prefers HTTPS. Each is
// asked as its https form, a site as a distfile it serves, which its tags
// say (the architecture thread, 2026-10-29).
func TestAPortsPlainHTTPURLs(t *testing.T) {
	t.Parallel()
	info := PortInfo{Options: map[string]string{
		"homepage":      "http://txt.hellman.io/",
		"livecheck.url": "http://txt.hellman.io/releases",
		"master_sites":  "http://ftp.example.org/pub/ http://ftp.example.org/pub/:docs http://mirror.example.org/x/:nosubdir:src https://example.org/ gnu sourceforge:project",
		"distfiles":     "txt-1.0.tar.gz txt-doc-1.0.pdf:docs txt-src.zip:src",
	}}
	require.Equal(t, []PlainURL{
		{"homepage", "http://txt.hellman.io/", "https://txt.hellman.io/"},
		{"livecheck.url", "http://txt.hellman.io/releases", "https://txt.hellman.io/releases"},
		{"master_sites", "http://ftp.example.org/pub/", "https://ftp.example.org/pub/txt-1.0.tar.gz"},
		{"master_sites", "http://mirror.example.org/x/", "https://mirror.example.org/x/txt-src.zip"},
	}, info.PlainHTTP())
	require.Empty(t, PortInfo{Options: map[string]string{"homepage": "https://example.org", "master_sites": "https://example.org/ gnu"}}.PlainHTTP())
	// A livecheck.url of ${master_sites} is asked as a page, and the site
	// still for its distfile.
	require.Equal(t, []PlainURL{
		{"livecheck.url", "http://www.example.org/dnsmasq/", "https://www.example.org/dnsmasq/"},
		{"master_sites", "http://www.example.org/dnsmasq/", "https://www.example.org/dnsmasq/dnsmasq-2.93.tar.xz"},
	}, PortInfo{Options: map[string]string{"livecheck.url": "http://www.example.org/dnsmasq/", "master_sites": "http://www.example.org/dnsmasq/", "distfiles": "dnsmasq-2.93.tar.xz"}}.PlainHTTP())
}

// A snapshot read with links switched to https as plain HTTP again has
// each https form of them, in any option, a value derived from one among
// them, written back as http, and nothing else.
func TestASnapshotAsPlainHTTP(t *testing.T) {
	t.Parallel()
	snapshot := Snapshot{Ports: map[string]PortInfo{"txt": {Name: "txt", Options: map[string]string{
		"homepage":      "https://txt.example/",
		"master_sites":  "https://txt.example/files/ https://txt.example/:docs https://mirror.example/",
		"livecheck.url": "https://txt.example/releases",
		"description":   "see https://other.example/",
	}}}}
	plain := snapshot.AsPlainHTTP([]string{"http://txt.example/"})
	require.Equal(t, map[string]string{
		"homepage":      "http://txt.example/",
		"master_sites":  "http://txt.example/files/ http://txt.example/:docs https://mirror.example/",
		"livecheck.url": "http://txt.example/releases",
		"description":   "see https://other.example/",
	}, plain.Ports["txt"].Options)
	require.Equal(t, "https://txt.example/", snapshot.Ports["txt"].Options["homepage"], "the snapshot itself stands")
	require.Equal(t, snapshot, snapshot.AsPlainHTTP(nil))
}

// A native library's ties are what's named for it: its PortGroup, and the
// ports depended on, versioned or without their lib prefix, never a port a
// name only begins, nor one a stripped name's version would name.
func TestWhatsTiedToANativeLibraryIsWhatsNamedForIt(t *testing.T) {
	port := PortInfo{Options: map[string]string{"dockhand.portgroups": "cargo openssl github"},
		Dependencies: []Dependency{{Port: "openssl3", Phase: "lib"}, {Port: "openssl3", Phase: "build"}, {Port: "opensslx"}, {Port: "sqlite3"}, {Port: "z3"}, {Port: "libgit2"}, {Port: "zlib"}}}
	for library, want := range map[string]LibraryTies{
		"openssl":    {PortGroups: []string{"openssl"}, Ports: []string{"openssl3"}},
		"libsqlite3": {Ports: []string{"sqlite3"}},
		"libgit2":    {Ports: []string{"libgit2"}},
		"libz":       {Ports: []string{"zlib"}},
		"onig":       {},
	} {
		require.Equal(t, want, port.TiesTo(library), library)
	}
	require.Equal(t, LibraryTies{Ports: []string{"openssl3"}}, PortInfo{Dependencies: port.Dependencies}.TiesTo("openssl"), "a port whose PortGroups weren't read")
	require.Equal(t, LibraryTies{Ports: []string{"oniguruma6"}}, PortInfo{Dependencies: []Dependency{{Port: "oniguruma6"}, {Port: "oniguruma7"}}}.TiesTo("onig"), "named otherwise, and only as named")
}

// A library's ports are looked for by its own name, without its lib
// prefix, and by the names ports provide it under otherwise.
func TestTheNamesALibrarysPortMayHave(t *testing.T) {
	for library, want := range map[string][]string{
		"zstd":       {"zstd"},
		"libsqlite3": {"libsqlite3", "sqlite3"},
		"libz":       {"libz", "z", "zlib"},
		"onig":       {"onig", "oniguruma6"},
		"lib":        {"lib"},
	} {
		require.Equal(t, want, LibraryPorts(library), library)
	}
}

// A port's file is in its tree where filespath names below its directory,
// as the evaluation wrote it with its own root; files where it wrote none;
// and nowhere it can say where filespath is elsewhere.
func TestWhereAPortsFileIsInItsTree(t *testing.T) {
	for _, test := range []struct {
		filespath, want string
		ok              bool
	}{
		{"", "devel/libuv/files/patch-a.diff", true},
		{"/tmp/workspace-1/devel/libuv/files", "devel/libuv/files/patch-a.diff", true},
		{"/tmp/workspace-1/devel/libuv/files/legacy", "devel/libuv/files/legacy/patch-a.diff", true},
		{"/tmp/devel/libuv/workspace/devel/libuv/files", "devel/libuv/files/patch-a.diff", true},
		{"/opt/elsewhere/files", "", false},
	} {
		where, ok := PortInfo{Options: map[string]string{"filespath": test.filespath}}.FilesPath("devel/libuv", "patch-a.diff")
		require.Equal(t, test.ok, ok, test.filespath)
		require.Equal(t, test.want, where, test.filespath)
	}
}

// A metaport or a _select port fetches nothing, and its version is
// tracked only where its livecheck reads one (batch 37).
func TestWhatAPortFetchesAndWhetherItsLivecheckReadsAVersion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		options         map[string]string
		nothing, reads  bool
		fetchErr, lcErr bool
	}{
		{"a _select port", map[string]string{"distfiles": "", "livecheck.type": "none"}, true, false, false, false},
		{"a metaport whose livecheck reads its release", map[string]string{"distfiles": "", "livecheck.type": "regex"}, true, true, false, false},
		{"the fallback reads only a page's change", map[string]string{"distfiles": "{}", "livecheck.type": "fallback"}, true, false, false, false},
		{"an archive", map[string]string{"distfiles": "foo-1.0.tar.gz", "livecheck.type": "regexm"}, false, true, false, false},
		{"a clone", map[string]string{"fetch.type": "git", "distfiles": "", "livecheck.type": "git"}, false, false, false, false},
		{"distfiles not read", map[string]string{"livecheck.type": "none"}, true, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := PortInfo{Options: test.options}
			nothing, err := port.FetchesNothing()
			require.Equal(t, test.fetchErr, err != nil, "%v", err)
			if err == nil {
				require.Equal(t, test.nothing, nothing)
			}
			reads, err := port.LivecheckReadsVersion()
			require.NoError(t, err)
			require.Equal(t, test.reads, reads)
			require.Equal(t, test.nothing && !test.reads && !test.fetchErr, port.OwnVersion(), "an unread fact is no own version")
		})
	}
	_, err := PortInfo{OptionErrors: map[string]string{"livecheck.type": "boom"}}.LivecheckReadsVersion()
	require.Error(t, err, "an unread livecheck isn't one that reads nothing")
}

// A distfile is extracted where extract.only names it, as it names each
// unless a Portfile says otherwise: bashunit's empty extract.only extracts
// nothing, and cargo's names the project's archive and not its crates.
// One whose extract.only wasn't read is taken as extracted.
func TestADistfileIsExtractedWhereExtractOnlyNamesIt(t *testing.T) {
	t.Parallel()
	require.False(t, PortInfo{Options: map[string]string{"extract.only": ""}}.Extracts("bashunit"))
	cargo := PortInfo{Options: map[string]string{"extract.only": "herdr-0.9.5.tar.gz"}}
	require.True(t, cargo.Extracts("herdr-0.9.5.tar.gz"))
	require.False(t, cargo.Extracts("adler2-2.0.1.crate"))
	require.True(t, PortInfo{}.Extracts("demo-1.0.tar.gz"), "not read")
	require.True(t, PortInfo{Options: map[string]string{"extract.only": "x"}, OptionErrors: map[string]string{"extract.only": "can't read"}}.Extracts("demo-1.0.tar.gz"), "unsettled")
}
