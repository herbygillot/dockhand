package depblock

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func sourceArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	file := filepath.Join(t.TempDir(), "source.tar.gz")
	require.NoError(t, os.WriteFile(file, data.Bytes(), 0600))
	return file
}
func helper(t *testing.T, body string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "helper with spaces")
	testsupport.WriteExecutable(t, file, "#!/bin/sh\nset -eu\n"+body+"\n")
	return file
}
func outputHelper(t *testing.T, body string) string {
	return helper(t, "cat <<'BLOCK'\n"+body+"\nBLOCK")
}
func TestManifestSelectionAndUnsafeMembers(t *testing.T) {
	t.Parallel()
	archive := sourceArchive(t, map[string]string{"root/go.mod": "root", "root/sub/go.mod": "nested"})
	data, member, err := Manifest(t.Context(), archive, "root/sub", "go.mod")
	require.NoError(t, err)
	require.Equal(t, "nested", string(data))
	require.Equal(t, "root/sub/go.mod", member)
	data, _, err = Manifest(t.Context(), archive, "different-root/sub", "go.mod")
	require.NoError(t, err)
	require.Equal(t, "nested", string(data))
	for _, files := range []map[string]string{
		{"a/go.mod": "one", "b/go.mod": "two"},
		{"../go.mod": "escape"}, {"/go.mod": "absolute"},
		{"root/readme": "missing"},
	} {
		_, _, err := Manifest(t.Context(), sourceArchive(t, files), "missing", "go.mod")
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = Manifest(ctx, archive, "root", "go.mod")
	require.ErrorIs(t, err, context.Canceled)
}
func TestManifestZipRejectsLinks(t *testing.T) {
	t.Parallel()
	for _, link := range []bool{false, true} {
		var data bytes.Buffer
		zw := zip.NewWriter(&data)
		h := &zip.FileHeader{Name: "root/Cargo.lock"}
		h.SetMode(0600)
		if link {
			h.SetMode(os.ModeSymlink | 0600)
		}
		w, err := zw.CreateHeader(h)
		require.NoError(t, err)
		_, err = w.Write([]byte("manifest"))
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		file := filepath.Join(t.TempDir(), "source.zip")
		require.NoError(t, os.WriteFile(file, data.Bytes(), 0600))
		body, _, err := Manifest(t.Context(), file, "root", "Cargo.lock")
		if link {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, "manifest", string(body))
		}
	}
}
func TestLiteralBlockEditingPreservesUnrelatedPortfile(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	src := []byte("# retained\nname fixture\ncargo.crates old 1.0 " + sha + "\n# human notes\nconfigure.args --keep\n")
	plan, err := Inspect(src, map[string]string{Cargo: "old 1.0 " + sha})
	require.NoError(t, err)
	stripped, err := plan.Strip(src)
	require.NoError(t, err)
	require.NotContains(t, string(stripped), "old 1.0")
	updated, err := plan.ApplyPlain(stripped, map[string][]string{Cargo: {"new", "2.0", sha, "extra", "1.0", sha}})
	require.NoError(t, err)
	require.Contains(t, string(updated), "# human notes\nconfigure.args --keep")
	values, err := generated(updated, Cargo)
	require.NoError(t, err)
	require.Len(t, values, 6)
	require.Contains(t, string(updated), "\\\n    extra")
	for _, source := range []string{"cargo.crates $crates\n", "cargo.crates a 1 hash\ncargo.crates b 2 hash\n"} {
		_, err := Inspect([]byte(source), map[string]string{Cargo: "a 1 hash"})
		require.Error(t, err)
	}
	_, err = plan.ApplyPlain(stripped, map[string][]string{Cargo: {"[exec evil]", "1", sha}})
	require.Error(t, err)
}
func TestGoGeneratorChecksExactManifestRequirements(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	manifest := "module github.com/owner/fixture\ngo 1.24\nrequire example.com/dep v1.2.3\n"
	in := Input{Archive: sourceArchive(t, map[string]string{"root/go.mod": manifest}), Worksrcdir: "root", Package: "github.com/owner/fixture", Tag: "v2.0"}
	script := helper(t, "[ \"$1\" = get ]\n[ \"$2\" = --dir ]\n[ \"$3\" = / ]\n[ \"$4\" = -- ]\n[ \"$5\" = github.com/owner/fixture ]\n[ \"$6\" = v2.0 ]\nprintf '%s\\n' 'go.vendors example.com/dep lock v1.2.3 sha256 "+sha+"'")
	result, err := Generate(t.Context(), Go, script, in)
	require.NoError(t, err)
	require.Len(t, result.Values[Go], 5)
	for _, output := range []string{"", "go.vendors example.com/dep lock v1.2.4 sha256 " + sha, "go.vendors example.com/dep lock v1.2.3", "go.vendors example.com/dep lock v1.2.3 sha256 [exec evil]"} {
		_, err := Generate(t.Context(), Go, outputHelper(t, output), in)
		require.Error(t, err)
	}
	in.Archive = sourceArchive(t, map[string]string{"root/go.mod": manifest, "root/go.work": "go 1.24\nuse ./submodule\n"})
	_, err = Generate(t.Context(), Go, script, in)
	require.ErrorContains(t, err, "Go workspaces")
	in.Archive = sourceArchive(t, map[string]string{"root/go.mod": manifest + "replace example.com/dep => ../local\n"})
	_, err = Generate(t.Context(), Go, script, in)
	require.ErrorContains(t, err, "replace/exclude")
	in.Archive = sourceArchive(t, map[string]string{"root/go.mod": "module github.com/owner/fixture\ngo 1.24\n"})
	result, err = Generate(t.Context(), Go, outputHelper(t, "# no dependencies"), in)
	require.NoError(t, err)
	require.Empty(t, result.Values[Go])
}
func TestCargoGeneratorRetainsRegistryAndGitDependencies(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	commit := strings.Repeat("b", 40)
	lock := fmt.Sprintf(`version = 4
[[package]]
name = "fixture"
version = "2.0.0"
[[package]]
name = "dep"
version = "1.2.3"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "%s"
[[package]]
name = "gitdep"
version = "0.1.0"
source = "git+https://github.com/owner/dep?branch=main#%s"
`, sha, commit)
	in := Input{Archive: sourceArchive(t, map[string]string{"root/Cargo.lock": lock}), Worksrcdir: "root"}
	result, err := Generate(t.Context(), Cargo, outputHelper(t, "cargo.crates dep 1.2.3 "+sha), in)
	require.NoError(t, err)
	require.Equal(t, []GitCrate{{Name: "gitdep", Repository: "owner/dep", Commit: commit, Reference: GitReference{Kind: GitBranch, Value: "main"}}}, result.Git)
	values, err := result.WithGitChecksums(map[string]string{"gitdep-" + commit + ".tar.gz": sha})
	require.NoError(t, err)
	require.Len(t, values[CargoGit], 5)
	_, err = result.WithGitChecksums(nil)
	require.Error(t, err)
	_, err = Generate(t.Context(), Cargo, outputHelper(t, ""), in)
	require.ErrorContains(t, err, "registry checksums exactly")
	for _, invalid := range []string{
		strings.ReplaceAll(lock, "branch=main", "rev=main"),
		strings.ReplaceAll(lock, "branch=main", "tag=v1"),
		strings.ReplaceAll(lock, "registry+https://github.com/rust-lang/crates.io-index", "registry+https://private.invalid/index"),
		strings.ReplaceAll(lock, sha, "invalid"),
		strings.ReplaceAll(lock, `name = "dep"`, `name = "../dep"`),
		strings.ReplaceAll(lock, `version = "1.2.3"`, `version = "../1.2.3"`),
		"# not a lockfile",
	} {
		in.Archive = sourceArchive(t, map[string]string{"root/Cargo.lock": invalid})
		_, err = Generate(t.Context(), Cargo, outputHelper(t, ""), in)
		require.Error(t, err)
	}
}
func TestToolsDistinguishMissingFailureAndCancellation(t *testing.T) {
	t.Parallel()
	tools := Tools{Go2Port: filepath.Join(t.TempDir(), "missing"), Cargo2Port: outputHelper(t, "")}
	_, err := tools.Resolve(Go)
	require.ErrorContains(t, err, "missing executable go2port")
	available := tools.Probe()
	require.False(t, available[0].Available)
	require.True(t, available[1].Available)
	file := helper(t, "echo 'generator failed' >&2\nexit 7")
	_, err = run(t.Context(), file, t.TempDir())
	require.ErrorContains(t, err, "generator failed")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = run(ctx, file, t.TempDir())
	require.ErrorIs(t, err, context.Canceled)
}

func TestEmptyDependencyBlocksAndUnrelatedPorts(t *testing.T) {
	t.Parallel()
	_, err := Inspect([]byte("go.vendors\n"), map[string]string{Go: "", Cargo: ""})
	require.ErrorContains(t, err, "mixed Go and Cargo")
	// A cargo port declaring no crates and building online, as fnox,
	// whose empty cargo.offline_cmd has Cargo fetch them as it builds,
	// has nothing to regenerate (field testing's batch 13); one building
	// offline needs them declared.
	plan, err := Inspect([]byte("name fnox\ncargo.offline_cmd\n"), map[string]string{Cargo: "", "cargo.offline_cmd": ""})
	require.NoError(t, err)
	require.Nil(t, plan)
	plan, err = Inspect([]byte("name fixture\n"), map[string]string{Cargo: "", "cargo.offline_cmd": "--frozen --offline"})
	require.NoError(t, err)
	require.Equal(t, Cargo, plan.Kind)
	plan, err = Inspect([]byte("go.vendors\n"), map[string]string{Go: ""})
	require.NoError(t, err)
	require.Equal(t, Go, plan.Kind)
	plan, err = Inspect([]byte("name fixture\n"), map[string]string{Go: "", "go.offline_build": "no"})
	require.NoError(t, err)
	require.Nil(t, plan)
}

func TestUnchangedDependencyBlockKeepsOriginalFormatting(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	src := []byte("version 1\ncargo.crates \\\n    first      1.0.0   " + sha + " \\\n    second     2.0.0   " + sha + "\n")
	values := []string{"first", "1.0.0", sha, "second", "2.0.0", sha}
	plan, err := Inspect(src, map[string]string{Cargo: strings.Join(values, " ")})
	require.NoError(t, err)
	stripped, err := plan.Strip(src)
	require.NoError(t, err)
	updated, err := plan.Apply([]byte(strings.Replace(string(stripped), "version 1", "version 2", 1)), map[string][]string{Cargo: values})
	require.NoError(t, err)
	require.Equal(t, strings.Replace(string(src), "version 1", "version 2", 1), string(updated))
}

func TestCargoGitReferencesFollowThePortPolicy(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	commit := strings.Repeat("b", 40)
	lock := func(selector string) string {
		return fmt.Sprintf(`version = 4
[[package]]
name = "fixture"
version = "2.0.0"
[[package]]
name = "dep"
version = "1.2.3"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "%s"
[[package]]
name = "pinned"
version = "0.1.0"
source = "git+https://github.com/owner/pinned%s#%s"
`, sha, selector, commit)
	}
	helper := outputHelper(t, "cargo.crates dep 1.2.3 "+sha)
	generate := func(selector string, policy GitPolicy) (GeneratedBlocks, error) {
		in := Input{Archive: sourceArchive(t, map[string]string{"root/Cargo.lock": lock(selector)}), Worksrcdir: "root", Git: policy}
		return Generate(t.Context(), Cargo, helper, in)
	}
	branch := GitCrate{Name: "pinned", Repository: "owner/pinned", Commit: commit, Reference: GitReference{Kind: GitBranch, Value: "main"}}
	for _, policy := range []GitPolicy{"", GitDeclared, GitMixed} {
		result, err := generate("?branch=main", policy)
		require.NoError(t, err)
		require.Equal(t, []GitCrate{branch}, result.Git)
		require.Empty(t, result.Online)
	}
	result, err := generate("?branch=main", GitOnline)
	require.NoError(t, err)
	require.Empty(t, result.Git)
	require.Equal(t, []GitCrate{branch}, result.Online, "a port that declares nothing keeps branch pins online too")
	for selector, reference := range map[string]GitReference{"?rev=" + commit: {Kind: GitRev, Value: commit}, "?tag=v1": {Kind: GitTag, Value: "v1"}, "": {}} {
		for _, policy := range []GitPolicy{"", GitDeclared} {
			_, err := generate(selector, policy)
			require.ErrorContains(t, err, "pinned is pinned to Git "+reference.String())
			require.ErrorContains(t, err, "declares branches only")
		}
		for _, policy := range []GitPolicy{GitMixed, GitOnline} {
			result, err := generate(selector, policy)
			require.NoError(t, err)
			require.Empty(t, result.Git)
			require.Equal(t, []GitCrate{{Name: "pinned", Repository: "owner/pinned", Commit: commit, Reference: reference}}, result.Online)
			values, err := result.WithGitChecksums(nil)
			require.NoError(t, err, "online crates need no archive checksum")
			require.Empty(t, values[CargoGit])
		}
	}
	for _, unsupported := range []string{"?branch=main&rev=" + commit, "?ref=main", "?branch=", "?branch=[exec]", "?branch=main&branch=other"} {
		_, err := generate(unsupported, GitOnline)
		require.ErrorContains(t, err, "selector", unsupported)
	}
	require.Equal(t, "pinned@bbbbbbbb", GitSummary(result.Online))
}

func TestInspectDerivesGitPolicyFromOfflineMode(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	crates := "cargo.crates dep 1.0 " + sha + "\n"
	declared := crates + "cargo.crates_github\n"
	for _, c := range []struct {
		src     string
		offline *string
		want    GitPolicy
	}{
		{crates, nil, GitDeclared},
		{crates, ptr("--frozen"), GitDeclared},
		{crates, ptr(""), GitOnline},
		{declared, ptr(""), GitMixed},
		{declared, ptr(" --offline "), GitDeclared},
	} {
		options := map[string]string{Cargo: "dep 1.0 " + sha}
		if c.offline != nil {
			options["cargo.offline_cmd"] = *c.offline
		}
		plan, err := Inspect([]byte(c.src), options)
		require.NoError(t, err)
		require.Equal(t, c.want, plan.Git)
	}
	plan, err := Inspect([]byte("go.vendors\n"), map[string]string{Go: "", "cargo.offline_cmd": ""})
	require.NoError(t, err)
	require.Empty(t, plan.Git)
}

func ptr(s string) *string { return &s }

// A Portfile declaring its crates in each branch of a platform's if, as
// cargo's does for current systems and frozen ones for older, has the
// declaration that ran edited, the one whose list MacPorts evaluated, and
// the other left as it is, laid out as it was: update refused cargo as
// declaring none (the rust and cargo run, batch 24).
func TestTheDeclarationThatRanIsTheOneEdited(t *testing.T) {
	t.Parallel()
	sha := func(c string) string { return strings.Repeat(c, 64) }
	src := []byte(`name cargo
if {${os.platform} eq "darwin" && ${os.major} <= 15} {
    # frozen
    cargo.crates \
        adler2                           2.0.0  ` + sha("a") + ` \
        anyhow                         1.0.80  ` + sha("b") + `
} else {
    cargo.crates \
        adler2                           2.0.1  ` + sha("c") + ` \
        anyhow                         1.0.98  ` + sha("d") + `
}
`)
	current := "adler2 2.0.1 " + sha("c") + " anyhow 1.0.98 " + sha("d")
	plan, err := Inspect(src, map[string]string{Cargo: current})
	require.NoError(t, err)
	updated, err := plan.Apply(src, map[string][]string{Cargo: {"adler2", "2.0.1", sha("c"), "anyhow", "1.0.99", sha("e"), "zstd", "0.13.3", sha("f")}})
	require.NoError(t, err)
	require.Contains(t, string(updated), "    # frozen\n    cargo.crates \\\n        adler2                           2.0.0  "+sha("a")+" \\\n        anyhow                         1.0.80  "+sha("b")+"\n} else {", "the frozen list stays as it is")
	require.Contains(t, string(updated), "anyhow                         1.0.99  "+sha("e"), "the list that ran takes the new crates, in its columns")
	require.Contains(t, string(updated), "        zstd ")
	stripped, err := plan.Strip(src)
	require.NoError(t, err)
	require.Contains(t, string(stripped), "1.0.80", "stripping sets aside only the list that ran")
	require.NotContains(t, string(stripped), "1.0.98")

	// Which ran is the one MacPorts evaluated: neither, or both alike,
	// can't say.
	_, err = Inspect(src, map[string]string{Cargo: "adler2 9.9.9 " + sha("c")})
	require.ErrorContains(t, err, "cargo.crates is declared 2 times, and none is what MacPorts evaluated")
}

// Differences name what Equivalent finds, by crate, module, or Git crate;
// a registry crate at two versions is an override, which a lock moves past
// only with the crate once, at the pin's version or later.
func TestDifferencesNameWhatDiffers(t *testing.T) {
	t.Parallel()
	sha, other := strings.Repeat("a", 64), strings.Repeat("b", 64)
	differences, err := Differences(Cargo, []string{"same", "1.0.0", sha, "pinned", "0.4.1", sha, "stray", "1.0.0", sha, "twice", "1.0.0", sha}, []string{"same", "1.0.0", sha, "pinned", "0.4.0", sha, "twice", "1.0.0", sha, "twice", "2.0.0", sha, "resummed", "1.0.0", other})
	require.NoError(t, err)
	require.Equal(t, []Difference{{Name: "pinned", Declared: "0.4.1", Generated: "0.4.0"}, {Name: "resummed"}, {Name: "stray"}, {Name: "twice"}}, differences)
	require.True(t, differences[0].Override())
	require.False(t, differences[1].Override())
	for next, want := range map[string]bool{"0.5.4": true, "0.4.1": true, "0.4.0": false, "0.4.1-rc.1": false} {
		locked, past := differences[0].MovedPast([]string{"pinned", next, sha})
		require.Equal(t, want, past, next)
		require.Equal(t, next, locked)
	}
	_, past := differences[0].MovedPast([]string{"pinned", "0.5.0", sha, "pinned", "0.6.0", sha})
	require.False(t, past, "a crate the lock has twice is no override's successor")

	differences, err = Differences(CargoGit, []string{"scram", "pgdogdev/scram", "master", strings.Repeat("c", 40), sha}, []string{"scram", "pgdogdev/scram", "master", strings.Repeat("d", 40), sha})
	require.NoError(t, err)
	require.Equal(t, []Difference{{Name: "scram"}}, differences)
	_, err = Differences(Cargo, []string{"incomplete", "1.0.0"}, nil)
	require.Error(t, err)
}

// A crate left to online resolution that the Portfile declares keeps the
// Portfile's label; one it doesn't declare stays online.
func TestKeepingDeclaredUsesThePortfilesLabel(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("c", 40)
	rev := GitReference{Kind: GitRev, Value: commit}
	blocks := GeneratedBlocks{Online: []GitCrate{{Name: "scram", Repository: "pgdogdev/scram", Commit: commit, Reference: rev}, {Name: "other", Repository: "owner/other", Commit: commit, Reference: rev}}}
	kept := blocks.KeepingDeclared([]string{"scram", "pgdogdev/scram", "master", strings.Repeat("e", 40), strings.Repeat("a", 64)})
	require.Equal(t, []GitCrate{{Name: "scram", Repository: "pgdogdev/scram", Commit: commit, Reference: GitReference{Kind: GitBranch, Value: "master"}}}, kept.Git)
	require.Equal(t, []GitCrate{{Name: "other", Repository: "owner/other", Commit: commit, Reference: rev}}, kept.Online)
	require.Equal(t, []GitCrate{{Name: "scram", Repository: "pgdogdev/scram", Commit: commit, Reference: rev}}, kept.Relabelled, "as the lock pins it")
	require.Equal(t, blocks, blocks.KeepingDeclared(nil))
}
