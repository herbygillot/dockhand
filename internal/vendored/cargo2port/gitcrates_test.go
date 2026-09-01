package cargo2port

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/vendored"
)

// The yazi shape: one registry crate the block form already covers,
// one branch-pinned GitHub crate the second block must carry.
const gitLock = `[[package]]
name = "libc"
version = "0.2.156"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "ratatui-core"
version = "0.1.0"
source = "git+https://github.com/sxyazi/ratatui?branch=fix_buffer_diff_wide_cells#dde5e05e59606cbba07340bd1cbb2d88866bc4a5"
`

func TestGitCratesReadsBranchPinnedGithubSources(t *testing.T) {
	crates, err := gitCrates([]byte(gitLock))
	require.NoError(t, err)
	require.Len(t, crates, 1, "registry crates belong to cargo.crates, not here")
	assert.Equal(t, gitCrate{
		Name:     "ratatui-core",
		Repo:     "sxyazi/ratatui",
		Branch:   "fix_buffer_diff_wide_cells",
		Revision: "dde5e05e59606cbba07340bd1cbb2d88866bc4a5",
	}, crates[0])
}

func TestGitCratesEmptyWithoutGitSources(t *testing.T) {
	crates, err := gitCrates([]byte(`[[package]]
name = "libc"
source = "registry+https://github.com/rust-lang/crates.io-index"
`))
	require.NoError(t, err)
	assert.Empty(t, crates)
}

func declineFor(t *testing.T, source string) *plan.Decline {
	t.Helper()
	lock := fmt.Sprintf("[[package]]\nname = \"thing\"\nsource = %q\n", source)
	_, err := gitCrates([]byte(lock))
	var d *plan.Decline
	require.ErrorAs(t, err, &d, source)
	assert.Contains(t, d.Detail, "thing", "the decline names the crate")
	return d
}

// The PortGroup writes the branch into cargo's source replacement, so
// only ?branch= pins are expressible; everything else declines by name
// rather than building a Portfile that cannot build.
func TestGitCratesDeclinesInexpressibleSources(t *testing.T) {
	d := declineFor(t, "git+https://gitlab.com/o/p?branch=main#dde5e05e59606cbba07340bd1cbb2d88866bc4a5")
	assert.Contains(t, d.Detail, "GitHub sources only")

	for _, q := range []string{
		"git+https://github.com/o/p?tag=v1.0#dde5e05e59606cbba07340bd1cbb2d88866bc4a5",
		"git+https://github.com/o/p?rev=dde5e05#dde5e05e59606cbba07340bd1cbb2d88866bc4a5",
		"git+https://github.com/o/p#dde5e05e59606cbba07340bd1cbb2d88866bc4a5",
	} {
		d := declineFor(t, q)
		assert.Contains(t, d.Detail, "branch references only", q)
	}

	d = declineFor(t, "git+https://github.com/o/p?branch=main")
	assert.Contains(t, d.Detail, "cannot read", "no pinned commit at all")
}

// The pgdog shape: an evaluated cargo.crates_github option, whose
// entries the PortGroup fetches as ${cname}-${crevision}.tar.gz.
func TestGithubSuppliedInNamesTheBlocksDistfiles(t *testing.T) {
	got, err := GithubSuppliedIn(
		"pg_raw_parse pgdogdev/pg_raw_parse master 8758803494e9f6eb4c4fbb47168aecc13614aa8f sum1 " +
			"scram pgdogdev/scram master 053e108210a35232efe60023ec43288fd601d8c4 sum2")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"pg_raw_parse-8758803494e9f6eb4c4fbb47168aecc13614aa8f.tar.gz",
		"scram-053e108210a35232efe60023ec43288fd601d8c4.tar.gz",
	}, got)

	got, err = GithubSuppliedIn("")
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = GithubSuppliedIn("name repo branch rev")
	require.ErrorIs(t, err, vendored.ErrMalformed, "a ragged block is refused, not misread")
}

func TestGithubRepo(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/sxyazi/ratatui":     "sxyazi/ratatui",
		"https://github.com/sxyazi/ratatui.git": "sxyazi/ratatui",
		"https://github.com/sxyazi/ratatui/":    "sxyazi/ratatui",
	} {
		got, ok := githubRepo(url)
		require.True(t, ok, url)
		assert.Equal(t, want, got, url)
	}
	for _, url := range []string{"https://gitlab.com/o/p", "https://github.com/only-owner"} {
		_, ok := githubRepo(url)
		assert.False(t, ok, url)
	}
}

// fetchFake records each URL and hands back a checksum derived from it,
// so the rendered block proves which tarball each sum came from. What
// it writes to dest is a real tarball whose root Cargo.toml is the
// manifest given, because buildGithubBlock reads it back.
type fetchFake struct {
	urls     []string
	manifest string
}

const packageToml = "[package]\nname = \"x\"\n"

func (f *fetchFake) Fetch(_ context.Context, urls []string, _ distfile.Options, dest string) (checksums.Sums, error) {
	f.urls = append(f.urls, urls...)
	manifest := f.manifest
	if manifest == "" {
		manifest = packageToml
	}
	if err := os.WriteFile(dest, repoTarball(manifest), 0o644); err != nil {
		return checksums.Sums{}, err
	}
	return checksums.Sums{Sha256: fmt.Sprintf("sum-of-%d", len(f.urls))}, nil
}

// repoTarball is a GitHub-archive-shaped tar.gz: one root directory
// holding a Cargo.toml.
func repoTarball(manifest string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte(manifest)
	_ = tw.WriteHeader(&tar.Header{Name: "repo-rev/Cargo.toml", Mode: 0o644, Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestBuildGithubBlockFetchesEachRevisionAndRendersTheTreeShape(t *testing.T) {
	fake := &fetchFake{}
	block, err := buildGithubBlock(t.Context(), vendored.Regen{Fetch: fake}, []gitCrate{
		{Name: "ratatui-core", Repo: "sxyazi/ratatui", Branch: "fix", Revision: "dde5e05e59606cbba07340bd1cbb2d88866bc4a5"},
		{Name: "other", Repo: "o/p", Branch: "main", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"https://github.com/sxyazi/ratatui/archive/dde5e05e59606cbba07340bd1cbb2d88866bc4a5.tar.gz",
		"https://github.com/o/p/archive/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tar.gz",
	}, fake.urls, "the checksum is of the tarball the PortGroup will fetch")
	assert.Equal(t, `cargo.crates_github \
    ratatui-core sxyazi/ratatui fix \
    dde5e05e59606cbba07340bd1cbb2d88866bc4a5 \
    sum-of-1 \
    other o/p main \
    aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
    sum-of-2`, block)
}

// The PortGroup imports a repository as one package directory, so a
// workspace's virtual manifest at the root cannot feed cargo — the
// yazi failure, judged before any branch is minted.
func TestBuildGithubBlockDeclinesAWorkspaceRepo(t *testing.T) {
	fake := &fetchFake{manifest: "[workspace]\nmembers = [\"ratatui-core\"]\n"}
	_, err := buildGithubBlock(t.Context(), vendored.Regen{Fetch: fake}, []gitCrate{
		{Name: "ratatui-core", Repo: "yazi-rs/ratatui", Branch: "fix", Revision: "dde5e05e59606cbba07340bd1cbb2d88866bc4a5"},
	})
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Contains(t, d.Detail, "ratatui-core lives in a cargo workspace at yazi-rs/ratatui")
}

func TestPackageManifest(t *testing.T) {
	assert.True(t, packageManifest([]byte("[package]\nname = \"x\"\n")))
	assert.True(t, packageManifest([]byte("[workspace]\n\n[package]\nname = \"x\"\n")),
		"a root package that is also a workspace is a package manifest")
	assert.False(t, packageManifest([]byte("[workspace]\nmembers = [\"a\"]\n")))
	assert.False(t, packageManifest([]byte("[package.metadata.docs]\nall-features = true\n")),
		"a package subtable alone does not declare a package")
}

func TestBuildGithubBlockRefusesWithoutAFetcher(t *testing.T) {
	_, err := buildGithubBlock(t.Context(), vendored.Regen{}, []gitCrate{{Name: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no fetcher")
}

const githubPortfile = `PortSystem 1.0
name                demo
version             1.0

cargo.crates \
    libc            0.2.156 a5f43f1

cargo.crates_github \
    ratatui-core sxyazi/ratatui old_branch \
    0000000000000000000000000000000000000000 \
    oldsum

checksums           rmd160 aaa sha256 bbb size 1
`

const registryOnlyPortfile = `PortSystem 1.0
name                demo
version             1.0

cargo.crates \
    libc            0.2.156 a5f43f1

checksums           rmd160 aaa sha256 bbb size 1
`

func githubRegen(t *testing.T, portfile string) vendored.Regen {
	t.Helper()
	src := []byte(portfile)
	cst, errs := syntax.Parse(src)
	require.Empty(t, errs)
	return vendored.Regen{
		Src: src, CST: cst,
		Vals:  info.Values{Name: "demo"},
		Fetch: &fetchFake{},
	}
}

func applyEdits(t *testing.T, src []byte, rc vendored.Regen, crates []gitCrate) string {
	t.Helper()
	span, err := vendored.Locate(rc.Src, rc.CST, portstyle.ScopeOf(rc.Src, "demo"), vendored.CargoCrates)
	require.NoError(t, err)
	edits, err := githubEdits(t.Context(), rc, span, crates)
	require.NoError(t, err)
	require.Len(t, edits, 1)
	e := edits[0]
	return string(src[:e.Start]) + e.New + string(src[e.End:])
}

func TestGithubEditsInsertsBesideItsSibling(t *testing.T) {
	rc := githubRegen(t, registryOnlyPortfile)
	got := applyEdits(t, rc.Src, rc, []gitCrate{
		{Name: "ratatui-core", Repo: "sxyazi/ratatui", Branch: "fix", Revision: "dde5e05e59606cbba07340bd1cbb2d88866bc4a5"},
	})
	assert.Contains(t, got, `    libc            0.2.156 a5f43f1

cargo.crates_github \
    ratatui-core sxyazi/ratatui fix \
    dde5e05e59606cbba07340bd1cbb2d88866bc4a5 \
    sum-of-1

checksums`, "the new block is born one blank line after cargo.crates")
}

func TestGithubEditsReplacesTheExistingBlock(t *testing.T) {
	rc := githubRegen(t, githubPortfile)
	got := applyEdits(t, rc.Src, rc, []gitCrate{
		{Name: "ratatui-core", Repo: "sxyazi/ratatui", Branch: "new_branch", Revision: "1111111111111111111111111111111111111111"},
	})
	assert.NotContains(t, got, "old_branch")
	assert.NotContains(t, got, "oldsum")
	assert.Contains(t, got, "ratatui-core sxyazi/ratatui new_branch \\\n    1111111111111111111111111111111111111111 \\\n    sum-of-1")
	assert.Equal(t, 1, strings.Count(got, "cargo.crates_github"), "replaced, not duplicated")
}

func TestGithubEditsDropsTheBlockWhenGitSourcesLeave(t *testing.T) {
	rc := githubRegen(t, githubPortfile)
	got := applyEdits(t, rc.Src, rc, nil)
	assert.NotContains(t, got, "cargo.crates_github")
	assert.Contains(t, got, "    libc            0.2.156 a5f43f1\n\nchecksums",
		"the deletion consumes its own blank line, not its neighbors'")
}

func TestGithubEditsNothingWhenAbsentAndUnneeded(t *testing.T) {
	rc := githubRegen(t, registryOnlyPortfile)
	span, err := vendored.Locate(rc.Src, rc.CST, portstyle.ScopeOf(rc.Src, "demo"), vendored.CargoCrates)
	require.NoError(t, err)
	edits, err := githubEdits(t.Context(), rc, span, nil)
	require.NoError(t, err)
	assert.Empty(t, edits)
}
