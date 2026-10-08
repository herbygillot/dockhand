package command

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/model"
)

// concerned is bumper with what an update can find that a check can't:
// upstream's archives, kept to compare, a Go requirement, a patch the
// update didn't check, and a change outside any port, which tidy can't
// commit unasked: its group has no port to name.
type concerned struct {
	bumper
	upstream  [2]map[string]string
	toolchain *editprep.GoToolchain
	patch     bool
	outside   bool
}

func (c concerned) Prepare(ctx context.Context, r editprep.Request) (editprep.Result, error) {
	result, err := c.bumper.Prepare(ctx, r)
	if err != nil || len(result.Files) == 0 {
		return result, err
	}
	options := map[string]string{}
	if c.toolchain != nil {
		options["go.package"], options["go.offline_build"] = "example.org/jq", "no"
		result.GoToolchain = c.toolchain
	}
	if c.patch {
		options["patchfiles"] = "fix-build.diff"
		result.Patches = []patchcheck.Result{{Name: "fix-build.diff", Detail: "outside the source directory"}}
	}
	for _, snapshot := range []macports.Snapshot{result.Fidelity[0].Before, result.Fidelity[0].After} {
		info := snapshot.Ports["jq"]
		info.Options = options
		snapshot.Ports["jq"] = info
	}
	if r.KeepArchives != "" && c.upstream[1] != nil {
		next := distfetch.Download{Path: tarball(r.KeepArchives, "new", c.upstream[1])}
		next.Name = "new.tar.gz"
		result.Downloads = []distfetch.Download{next}
		if c.upstream[0] != nil {
			result.Pairs = []editprep.ArchivePair{{Previous: distfetch.Download{Path: tarball(r.KeepArchives, "old", c.upstream[0])}, Next: next}}
		}
	}
	if c.outside {
		const group = "_resources/port1.0/group/github-1.0.tcl"
		before, data, err := c.repo.File(ctx, string(r.Source.Tree), group)
		if err != nil {
			return result, err
		}
		edit := git.FileEdit{Path: group, Before: before, After: append(data, "# also changed\n"...), Mode: before.Mode}
		tree, err := c.repo.EditTree(ctx, string(result.PreparedTree), []git.FileEdit{edit})
		if err != nil {
			return result, err
		}
		result.Files, result.PreparedTree = append(result.Files, edit), model.ObjectID(tree)
	}
	return result, nil
}

// tarball writes files under one top directory, as a release archive has
// them; a failure leaves no archive, which the comparison says.
func tarball(directory, top string, files map[string]string) string {
	name := filepath.Join(directory, top+".tar.gz")
	out, err := os.Create(name)
	if err != nil {
		return name
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for path, content := range files {
		if tw.WriteHeader(&tar.Header{Name: top + "/" + path, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}) != nil {
			return name
		}
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()
	_ = gz.Close()
	return name
}

// Nobody looks before bump or serve submits, so each stops where the other
// does, on the same findings: the submission's held concerns, which both
// read from the plan's, and an edit tidy can't commit unasked (the
// architecture review's M3). Each case runs bump in one world and serve
// --submit-passing in another, from the same update.
func TestBumpAndServeStopOnTheSameConditions(t *testing.T) {
	for _, test := range []struct {
		name     string
		concerns concerned
		search   error
		stops    bool
	}{
		{name: "nothing to look at"},
		{name: "upstream changed its license", concerns: concerned{upstream: [2]map[string]string{{"LICENSE": "MIT\n"}, {"LICENSE": "GPL-3\n"}}}, stops: true},
		{name: "an archive pairs with none", concerns: concerned{upstream: [2]map[string]string{nil, {"LICENSE": "MIT\n"}}}, stops: true},
		{name: "a Go minimum left low", concerns: concerned{upstream: [2]map[string]string{{"go.mod": "module m\n\ngo 1.24\n"}, {"go.mod": "module m\n\ngo 1.25\n"}},
			toolchain: &editprep.GoToolchain{Required: "1.25", Outcome: editprep.GoToolchainUndeclared}}, stops: true},
		// A patch the update couldn't check is coverage, not a finding: the
		// check's build applies it, and fails if it doesn't.
		{name: "an unchecked patch", concerns: concerned{patch: true}},
		{name: "other pull requests couldn't be looked for", search: errors.New("rate limited"), stops: true},
		// A change outside any port leaves tidy's plan for a person: serve
		// stops before its check, and bump, whose --yes would apply a plan
		// as shown, finds the commit has no subject to take.
		{name: "a change outside any port", concerns: concerned{outside: true}, stops: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bumped, bumpSaid := bumpWith(t, test.concerns, test.search)
			served, serveSaid := serveWith(t, test.concerns, test.search)
			require.Equal(t, test.stops, !bumped, "bump: %s", bumpSaid)
			require.Equal(t, test.stops, !served, "serve: %s", serveSaid)
		})
	}
}

// concernedWorld is a world whose update finds the concerns given, with
// GitHub knowing of no other pull request for jq, or failing to look.
func concernedWorld(t *testing.T, concerns concerned, search error) *forgetest.GitHub {
	t.Helper()
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(e *engine.Engine) engine.Preparer {
		concerns.bumper = bumper{repo: e.Repo}
		return concerns
	}
	t.Cleanup(func() { testPreparer = nil })
	g := withGitHub(t, w)
	g.Others, g.SearchErr = nil, search
	withScript(t, w, "passed")
	return g
}

// bumpWith says whether bump opened a pull request, and what it said.
func bumpWith(t *testing.T, concerns concerned, search error) (bool, string) {
	t.Helper()
	g := concernedWorld(t, concerns, search)
	out, errs, err := bumpOn(t, "jq")
	said := out + errs
	if err != nil {
		said += "error: " + err.Error()
	}
	return len(g.PRs) > 0, said
}

// serveWith says whether serve, preparing jq from its daily look and
// submitting what passed, opened a pull request, and what it said.
func serveWith(t *testing.T, concerns concerned, search error) (bool, string) {
	t.Helper()
	g := concernedWorld(t, concerns, search)
	withOutdated(t)
	poll := servePoll
	t.Cleanup(func() { servePoll = poll })
	servePoll = 20 * time.Millisecond
	// The day's look runs from serve.outdated_at, 07:00 local by default:
	// a clock left running found it not yet due past midnight, and serve
	// prepared nothing (CI on the rc9 tag, 00:21 to 00:45 UTC).
	now := serveNow
	t.Cleanup(func() { serveNow = now })
	morning := time.Date(2026, 10, 7, 8, 0, 0, 0, time.Local)
	serveNow = func() time.Time { return morning }
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	post := postNotification
	t.Cleanup(func() { postNotification = post })
	postNotification = func(string, string) error { return nil }
	config := filepath.Join(os.Getenv("HOME"), ".dockhand", "config.toml")
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(config, append([]byte("maintainer = \"{@ada example.org:ada}\"\n"), append(data, []byte("\n[serve]\nfor_outdated = \"check\"\n")...)...), 0o644))

	ctx, stop := context.WithCancel(t.Context())
	var served syncBuffer
	done := make(chan error)
	go func() {
		done <- Run(ctx, []string{"serve", "--submit-passing"}, Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	// It opens the pull request, holds the branch for a look, or stops
	// the update before its check.
	ends := []string{"serve: opened #", " is held for a look: ", "serve: jq-1.8.1: "}
	ended := func() bool {
		said := served.String()
		for _, end := range ends {
			if strings.Contains(said, end) {
				return true
			}
		}
		return false
	}
	for deadline := time.Now().Add(settle); !ended() && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, ended(), served.String())
	stop()
	require.NoError(t, <-done)
	return len(g.PRs) > 0, served.String()
}
