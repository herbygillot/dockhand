package upstream

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func TestCoords(t *testing.T) {
	cases := []struct {
		style portstyle.Type
		opts  map[string]string
		want  string
	}{
		{portstyle.GithubSetup,
			map[string]string{"github.author": "openai", "github.project": "tart", "github.tag_prefix": "v"},
			"https://github.com/openai/tart"},
		{portstyle.GitlabSetup,
			map[string]string{"gitlab.author": "a", "gitlab.project": "p", "gitlab.instance": "https://gitlab.com"},
			"https://gitlab.com/a/p"},
		{portstyle.GiteaSetup,
			map[string]string{"gitea.author": "a", "gitea.project": "p", "gitea.domain": "git.example.org"},
			"https://git.example.org/a/p"},
		{portstyle.CodebergSetup,
			map[string]string{"codeberg.author": "dnkl", "codeberg.project": "foot"},
			"https://codeberg.org/dnkl/foot"},
		{portstyle.SourcehutSetup,
			map[string]string{"sourcehut.author": "sircmpwn", "sourcehut.project": "aerc", "sourcehut.instance": "https://git.sr.ht"},
			"https://git.sr.ht/~sircmpwn/aerc"},
	}
	for _, c := range cases {
		r, ok := Coords(c.style, c.opts)
		require.True(t, ok, c.want)
		assert.Equal(t, c.want, r.URL)
	}

	// No forge for non-forge carriers; incomplete coordinates refuse.
	_, ok := Coords(portstyle.VersionLine, nil)
	assert.False(t, ok)
	_, ok = Coords(portstyle.GithubSetup, map[string]string{"github.author": "x"})
	assert.False(t, ok)
	assert.Nil(t, CoordOptions(portstyle.PureSetup))
}

func TestStable(t *testing.T) {
	for v, want := range map[string]bool{
		"2.36.0":       true,
		"1.0":          true,
		"3.0-rc1":      false,
		"3.0rc1":       false,
		"2.0-beta.2":   false,
		"1.0alpha":     false,
		"5.0-preview":  false,
		"7.2-nightly":  false,
		"1.2.3-dev":    false,
		"1.99":         true,
		"0.4.0":        true,
		"2024-05-01":   true,
		"1.0-SNAPSHOT": false,
	} {
		assert.Equal(t, want, Stable(v), v)
	}
}

func TestJudge(t *testing.T) {
	// Agreement.
	r := Judge(Observation{Livecheck: "2.0", ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, Agreement, r.Verdict)
	assert.Equal(t, "2.0", r.Latest)

	// Rot: livecheck matched nothing, forge has versions.
	r = Judge(Observation{Livecheck: "", ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, LivecheckRot, r.Verdict)
	assert.Empty(t, r.Latest)

	// Behind: forge has a newer stable.
	r = Judge(Observation{Livecheck: "1.0", ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, LivecheckBehind, r.Verdict)
	assert.Empty(t, r.Latest)

	// Only prereleases newer: livecheck's conservatism is policy.
	r = Judge(Observation{Livecheck: "1.0", ForgeVersions: []string{"1.0", "2.0.rc.1"}})
	assert.Equal(t, Agreement, r.Verdict)
	assert.Equal(t, "1.0", r.Latest)

	// Ahead: livecheck newer than every tag.
	r = Judge(Observation{Livecheck: "3.0", ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, LivecheckAhead, r.Verdict)

	// Livecheck disabled, forge answers.
	r = Judge(Observation{LivecheckDisabled: true, ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, ForgeOnly, r.Verdict)
	assert.Equal(t, "2.0", r.Latest)

	// No forge: livecheck stands alone.
	r = Judge(Observation{Livecheck: "2.0"})
	assert.Equal(t, LivecheckOnly, r.Verdict)
	assert.Equal(t, "2.0", r.Latest)

	// Nothing at all.
	r = Judge(Observation{LivecheckDisabled: true})
	assert.Equal(t, NoSignal, r.Verdict)
}

func TestTags(t *testing.T) {
	testenv.Tool(t, "git")
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s: %s", args, out)
	}
	run("init", "-q")
	run("commit", "--allow-empty", "-q", "-m", "x")
	run("tag", "v1.0.0")
	run("tag", "v1.1.0")
	run("tag", "-a", "v2.0.0-rc1", "-m", "rc") // annotated: exercises peeled dedup
	run("tag", "other-3.0")                    // wrong scheme: excluded

	versions, err := Tags(context.Background(), Repo{URL: dir, TagPrefix: "v"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"1.0.0", "1.1.0", "2.0.0-rc1"}, versions)
}

// go.setup delegates tag naming to the forge family its domain selects;
// the scheme is found in that family's options.
func TestCoordsGoDelegatedTagScheme(t *testing.T) {
	r, ok := Coords(portstyle.GoSetup, map[string]string{
		"go.domain": "github.com", "go.author": "robpike", "go.project": "ivy",
		"github.tag_prefix": "v", "github.tag_suffix": "",
	})
	require.True(t, ok)
	assert.Equal(t, "https://github.com/robpike/ivy", r.URL)
	assert.Equal(t, "v", r.TagPrefix)
}

func TestTagsStripsSuffix(t *testing.T) {
	testenv.Tool(t, "git")
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s: %s", args, out)
	}
	run("init", "-q")
	run("commit", "--allow-empty", "-q", "-m", "x")
	run("tag", "v1.0.0-release")
	run("tag", "v1.1.0-release")
	run("tag", "v2.0.0") // suffix missing: not this port's scheme

	versions, err := Tags(context.Background(),
		Repo{URL: dir, TagPrefix: "v", TagSuffix: "-release"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"1.0.0", "1.1.0"}, versions)
}

func TestCoordsCarriesSuffix(t *testing.T) {
	r, ok := Coords(portstyle.GithubSetup, map[string]string{
		"github.author": "a", "github.project": "p",
		"github.tag_prefix": "REL_", "github.tag_suffix": "_final",
	})
	require.True(t, ok)
	assert.Equal(t, "REL_", r.TagPrefix)
	assert.Equal(t, "_final", r.TagSuffix)
}

func TestCoordsNewFamilies(t *testing.T) {
	// notabug: a fixed-host forge of its own.
	r, ok := Coords(portstyle.NotabugSetup, map[string]string{
		"notabug.author": "a", "notabug.project": "p", "notabug.tag_prefix": "v",
	})
	require.True(t, ok)
	assert.Equal(t, "https://notabug.org/a/p", r.URL)
	assert.Equal(t, "v", r.TagPrefix)

	// cgit: no author, project hangs off the instance, clone URL has .git.
	r, ok = Coords(portstyle.CgitSetup, map[string]string{
		"cgit.url": "git.zx2c4.com", "cgit.project": "wireguard-tools",
	})
	require.True(t, ok)
	assert.Equal(t, "https://git.zx2c4.com/wireguard-tools.git", r.URL)

	// octave delegates to github; the coordinates land there.
	r, ok = Coords(portstyle.OctaveSetup, map[string]string{
		"github.author": "gnu-octave", "github.project": "statistics", "github.tag_prefix": "release-",
	})
	require.True(t, ok)
	assert.Equal(t, "https://github.com/gnu-octave/statistics", r.URL)
	assert.Equal(t, "release-", r.TagPrefix)

	// R delegates to gitlab for gitlab-domain packages.
	r, ok = Coords(portstyle.RSetup, map[string]string{
		"gitlab.author": "r-pkg", "gitlab.project": "thing", "gitlab.instance": "https://gitlab.com",
	})
	require.True(t, ok)
	assert.Equal(t, "https://gitlab.com/r-pkg/thing", r.URL)

	// An R port on CRAN sets no forge family options: no forge, honestly.
	_, ok = Coords(portstyle.RSetup, map[string]string{})
	assert.False(t, ok)
}
