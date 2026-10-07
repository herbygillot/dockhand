package editprep_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func manifestArchive(t *testing.T, name, manifest, version string, more ...string) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	// more are further name, content pairs, as a go.sum beside go.mod.
	files := append([]string{name, manifest}, more...)
	for i := 0; i+1 < len(files); i += 2 {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: "fixture-" + version + "/" + files[i], Mode: 0600, Size: int64(len(files[i+1])), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(files[i+1]))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return data.Bytes()
}

// shippedChecksums declares the current version's archive in the fixture's
// Portfile as archive, where the fixture declares placeholders: an update
// keeping archives to compare fetches it as MacPorts shipped it.
func shippedChecksums(t *testing.T, service *editprep.Service, source model.Source, archive []byte) model.Source {
	t.Helper()
	state, data, err := service.Repo.File(t.Context(), string(source.Tree), "devel/fixture/Portfile")
	require.NoError(t, err)
	placeholder := fmt.Sprintf("checksums rmd160 %s \\\n    sha256 %s \\\n    size 1\n", strings.Repeat("0", 40), strings.Repeat("0", 64))
	require.Contains(t, string(data), placeholder)
	declared := fmt.Sprintf("checksums sha256 %x \\\n    size %d\n", sha256.Sum256(archive), len(archive))
	tree, err := service.Repo.EditTree(t.Context(), string(source.Tree), []git.FileEdit{{Path: "devel/fixture/Portfile", Before: state, After: []byte(strings.Replace(string(data), placeholder, declared, 1)), Mode: state.Mode}})
	require.NoError(t, err)
	return model.Source{Tree: model.ObjectID(tree), Base: source.Base}
}

func fileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func dependencyHelper(t *testing.T, body string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "dependency helper")
	testsupport.WriteExecutable(t, file, "#!/bin/sh\nset -eu\n"+body+"\n")
	return file
}
func TestGoDependencyPreparation(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	for _, scenario := range []string{"success", "kept", "kept-stealth", "kept-unshipped", "removed", "missing", "failed", "partial", "override", "patched", "local-patch", "unsupported-context", "moved"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			old := "module github.com/owner/fixture\ngo 1.24\nrequire example.com/old v1.0.0\n"
			next := "module github.com/owner/fixture\ngo 1.24\nrequire example.com/new/v2 v2.0.0\n"
			if scenario == "removed" {
				next = "module github.com/owner/fixture\ngo 1.24\n"
			}
			// The module moved since the current version, as pomo's did
			// from GitHub to Codeberg (field testing, 2026-10-02).
			if scenario == "moved" {
				old = strings.Replace(old, "github.com/owner/fixture", "github.com/elsewhere/fixture", 1)
			}
			before := manifestArchive(t, "go.mod", old, "1.0")
			after := manifestArchive(t, "go.mod", next, "2.0")
			// What upstream serves under the current version's name after a
			// stealth update: not what MacPorts shipped.
			stealthy := manifestArchive(t, "go.mod", old+"// regenerated\n", "1.0")
			served := before
			if scenario == "kept-stealth" || scenario == "kept-unshipped" {
				served = stealthy
			}
			extra := `options go.vendors
 default go.vendors {}
 proc fixture_vendors {} {
  foreach {module lock value sha checksum} [option go.vendors] {
   distfiles-append dep.tar.gz:vendor
   master_sites-append https://invalid.example:vendor
   checksums-append dep.tar.gz sha256 $checksum
  }
 }
 port::register_callback fixture_vendors
 ` + "go.vendors example.com/old lock v1.0.0 sha256 " + sha + "\n# preserve these instructions\nconfigure.args --keep\n"
			if scenario == "override" {
				extra = strings.ReplaceAll(extra, "lock v1.0.0", "lock v9.0.0")
			}
			if scenario == "patched" {
				extra += "post-patch { reinplace s/foo/bar/ ${worksrcpath}/go.mod }\n"
			}
			if scenario == "local-patch" {
				// A patch under files/ that leaves the manifest alone: the
				// policy reads it where the stripped baseline was evaluated,
				// an overlay, whose filespath names that overlay.
				extra += "patchfiles fix.patch\n"
			}
			if scenario == "unsupported-context" {
				extra += "if {${os.major} < 23 && ${build_arch} eq \"arm64\"} { pre-fetch { set distfiles changed.tar.gz } }\n"
			}
			var requests atomic.Int64
			service, request := versionFixture(t, "go-setup", extra, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if strings.Contains(r.URL.Path, "/1.0/") {
					_, _ = w.Write(served)
				} else {
					_, _ = w.Write(after)
				}
			})
			output := "go.vendors example.com/new/v2 lock v2.0.0 sha256 " + sha
			if scenario == "removed" || scenario == "partial" {
				output = ""
			}
			script := "for tag do :; done\nif [ \"$tag\" = v1.0 ]; then\nprintf '%s\\n' 'go.vendors example.com/old lock v1.0.0 sha256 " + sha + "'\nelse\nprintf '%s\\n' '" + output + "'\nfi"
			if scenario == "failed" || scenario == "unsupported-context" {
				script = "echo 'dependency resolution failed' >&2; exit 3"
			}
			executable := dependencyHelper(t, script)
			if scenario == "missing" {
				executable = filepath.Join(t.TempDir(), "absent")
			}
			service.DependencyTools = depblock.Tools{Go2Port: executable, Cargo2Port: "absent"}
			if scenario == "local-patch" {
				tree, err := service.Repo.EditTree(t.Context(), string(request.Source.Tree), []git.FileEdit{{Path: "devel/fixture/files/fix.patch", After: []byte("--- a/README\n+++ b/README\n@@ -1 +1 @@\n-foo\n+bar\n"), Mode: 0o100644}})
				require.NoError(t, err)
				request.Source = model.Source{Tree: model.ObjectID(tree)}
			}
			var mirrored atomic.Value
			if strings.HasPrefix(scenario, "kept") {
				request.KeepArchives = t.TempDir()
				request.Source = shippedChecksums(t, service, request.Source, before)
			}
			if scenario == "kept-stealth" {
				mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mirrored.Store(r.URL.Path)
					_, _ = w.Write(before)
				}))
				t.Cleanup(mirror.Close)
				service.Mirror = mirror.URL + "/"
			}
			result, err := service.Prepare(t.Context(), request)
			switch scenario {
			case "unsupported-context":
				require.ErrorContains(t, err, "pre-fetch hook 1 ends with `set distfiles changed.tar.gz` rather than return -code error, which writes `distfiles`, at Portfile line 46")
				require.NotContains(t, err.Error(), "dependency resolution failed")
				require.Zero(t, requests.Load(), "local refusal must precede old-source download and helper")
			case "missing":
				require.ErrorContains(t, err, "fixture: dependency: cannot regenerate go.vendors: missing executable go2port")
				require.Zero(t, requests.Load())
			case "failed":
				require.ErrorContains(t, err, "dependency resolution failed")
			case "partial":
				require.ErrorContains(t, err, "requirements exactly")
			case "override":
				require.ErrorContains(t, err, "preserve these overrides")
			case "patched":
				require.ErrorContains(t, err, "edits a dependency manifest")
				require.Zero(t, requests.Load())
			default:
				require.NoError(t, err)
				require.NotEmpty(t, result.PreparedTree)
				require.Equal(t, request.Source, result.Base)
				require.Len(t, result.Files, 1)
				require.Contains(t, string(result.Files[0].After), "# preserve these instructions\nconfigure.args --keep")
				require.NotContains(t, string(result.Files[0].After), "example.com/old")
				if scenario == "success" || scenario == "moved" {
					require.Contains(t, string(result.Files[0].After), "example.com/new/v2 lock v2.0.0")
				}
				unchecked := ""
				if scenario == "moved" {
					unchecked = "The module moved from github.com/elsewhere/fixture to github.com/owner/fixture, so the existing go.vendors wasn't checked against 1.0's source; it's regenerated whole."
				}
				require.Equal(t, unchecked, result.Regenerated[0].Unchecked)
				fetched := int64(2)
				if scenario == "kept-unshipped" {
					fetched = 3 // once as shipped, which fails, and once as served
				}
				require.Equal(t, fetched, requests.Load(), "each version's archive is fetched once, kept or not")
				require.Len(t, result.Downloads, 1)
				for _, fidelity := range result.Fidelity {
					require.Empty(t, fidelity.UnexpectedChanges)
				}
				switch scenario {
				case "kept", "kept-stealth":
					// The current version's archive is kept beside the new
					// one, as MacPorts shipped it, so the update's upstream
					// comparison has a pair: from upstream, or where upstream
					// now serves something else, from MacPorts' mirror under
					// the port's dist_subdir.
					require.Len(t, result.Pairs, 1)
					require.Empty(t, result.PreviousProblem)
					require.FileExists(t, result.Pairs[0].Previous.Path)
					require.FileExists(t, result.Downloads[0].Path)
					require.Equal(t, before, fileBytes(t, result.Pairs[0].Previous.Path))
					require.Equal(t, after, fileBytes(t, result.Pairs[0].Next.Path))
					if scenario == "kept-stealth" {
						require.Regexp(t, `^/fixture/fixture-`, mirrored.Load())
					}
				case "kept-unshipped":
					// Neither has it as shipped: the update goes on, and its
					// comparison says why it can't be made.
					require.Empty(t, result.Pairs)
					require.Contains(t, result.PreviousProblem, "upstream no longer serves")
				default:
					require.Empty(t, result.Pairs, "kept only when asked")
				}
			}
			if err != nil {
				require.Empty(t, result.PreparedTree)
			}
		})
	}
}

// A module the Portfile keeps that the current version's go2port output
// leaves out, but the new version's has, is the new output's: listed once,
// and not said to be kept. go-reflex 0.3.2 listed kr/text twice, and its
// check failed at extract (the rc3 full run, 2026-10-06).
func TestAKeptGoModuleTheNewOutputHasIsListedOnce(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	old := "module github.com/owner/fixture\ngo 1.24\nrequire example.com/old v1.0.0\n"
	next := "module github.com/owner/fixture\ngo 1.24\nrequire (\n\texample.com/new/v2 v2.0.0\n\tgithub.com/kr/text v0.1.0\n)\n"
	before := manifestArchive(t, "go.mod", old, "1.0")
	// The new go.sum still pins it, as 0.3.2's did.
	after := manifestArchive(t, "go.mod", next, "2.0", "go.sum", "github.com/kr/text v0.1.0 h1:abc=\nexample.com/new/v2 v2.0.0 h1:def=\n")
	extra := `options go.vendors
 default go.vendors {}
 proc fixture_vendors {} {
  foreach {module lock value sha checksum} [option go.vendors] {
   distfiles-append dep.tar.gz:vendor
   master_sites-append https://invalid.example:vendor
   checksums-append dep.tar.gz sha256 $checksum
  }
 }
 port::register_callback fixture_vendors
 ` + "go.vendors example.com/old lock v1.0.0 sha256 " + sha + " github.com/kr/text lock v0.1.0 sha256 " + sha + "\n"
	service, request := versionFixture(t, "go-setup", extra, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/1.0/") {
			_, _ = w.Write(before)
		} else {
			_, _ = w.Write(after)
		}
	})
	script := "for tag do :; done\nif [ \"$tag\" = v1.0 ]; then\nprintf '%s\\n' 'go.vendors example.com/old lock v1.0.0 sha256 " + sha + "'\nelse\nprintf '%s\\n' 'go.vendors github.com/kr/text lock v0.1.0 sha256 " + sha + " example.com/new/v2 lock v2.0.0 sha256 " + sha + "'\nfi"
	service.DependencyTools = depblock.Tools{Go2Port: dependencyHelper(t, script), Cargo2Port: "absent"}
	result, err := service.Prepare(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, result.Files, 1)
	require.Equal(t, 1, strings.Count(string(result.Files[0].After), "github.com/kr/text"), string(result.Files[0].After))
	for _, block := range result.Regenerated {
		for _, notice := range block.Notices {
			require.NotContains(t, notice, "Kept the modules")
		}
	}
}

// A module before go 1.17 whose go.mod leaves out one its Portfile already
// keeps by hand, at the version go.sum pins, updates, keeping it, where
// every update was refused: go-reflex, at go 1.15, keeps kr/text v0.1.0
// beside go2port's rows (the rc7 full stage).
func TestAnUnprunedGoModuleKeepsWhatGoVendorsKeeps(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	mod := "module github.com/owner/fixture\ngo 1.15\nrequire github.com/kr/pretty v0.1.0\n"
	sum := "github.com/kr/pretty v0.1.0 h1:a=\ngithub.com/kr/pretty v0.1.0/go.mod h1:b=\ngithub.com/kr/text v0.1.0 h1:c=\ngithub.com/kr/text v0.1.0/go.mod h1:d=\n"
	before := manifestArchive(t, "go.mod", mod, "1.0", "go.sum", sum)
	after := manifestArchive(t, "go.mod", mod, "2.0", "go.sum", sum)
	extra := `options go.vendors
 default go.vendors {}
 proc fixture_vendors {} {
  foreach {module lock value sha checksum} [option go.vendors] {
   distfiles-append dep.tar.gz:vendor
   master_sites-append https://invalid.example:vendor
   checksums-append dep.tar.gz sha256 $checksum
  }
 }
 port::register_callback fixture_vendors
 ` + "go.vendors github.com/kr/pretty lock v0.1.0 sha256 " + sha + " github.com/kr/text lock v0.1.0 sha256 " + sha + "\n"
	service, request := versionFixture(t, "go-setup", extra, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/1.0/") {
			_, _ = w.Write(before)
		} else {
			_, _ = w.Write(after)
		}
	})
	script := "printf '%s\\n' 'go.vendors github.com/kr/pretty lock v0.1.0 sha256 " + sha + "'"
	service.DependencyTools = depblock.Tools{Go2Port: dependencyHelper(t, script), Cargo2Port: "absent"}
	result, err := service.Prepare(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, result.Files, 1)
	require.Equal(t, 1, strings.Count(string(result.Files[0].After), "github.com/kr/text lock v0.1.0"), string(result.Files[0].After))
}

func TestCargoDependencyPreparation(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("b", 64)
	for _, scenario := range []string{"success", "auxiliary", "added", "removed", "missing", "failed", "partial", "shared", "shared-unauthorized", "cargo-update", "cargo-update-unlocked"} {
		t.Run(scenario, func(t *testing.T) {
			lock := func(name string) string {
				result := "version = 4\n[[package]]\nname = \"fixture\"\nversion = \"1.0.0\"\n"
				if name != "" {
					result += fmt.Sprintf("[[package]]\nname = %q\nversion = \"1.2.3\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = %q\n", name, sha)
				}
				return result
			}
			oldName, newName := "old", "new"
			if scenario == "added" {
				oldName = ""
			}
			if scenario == "removed" {
				newName = ""
			}
			before := manifestArchive(t, "Cargo.lock", lock(oldName), "1.0")
			after := manifestArchive(t, "Cargo.lock", lock(newName), "2.0")
			if scenario == "cargo-update-unlocked" {
				// A source that ships no Cargo.lock, which cargo.update
				// would make.
				before = manifestArchive(t, "Cargo.toml", "[package]\nname = \"fixture\"\n", "1.0")
			}
			extra := `options cargo.crates cargo.crates_github cargo.update cargo.dir
 default cargo.crates {}
 default cargo.crates_github {}
 default cargo.update no
 default cargo.dir {${worksrcpath}}
 proc fixture_crates {} {
  foreach {name version checksum} [option cargo.crates] {
   distfiles-append ${name}-${version}.crate:crate-${name}
   master_sites-append https://static.crates.io/crates/${name}:crate-${name}
   checksums-append ${name}-${version}.crate sha256 $checksum
  }
 }
 port::register_callback fixture_crates
 `
			if scenario == "auxiliary" {
				extra += `
checksums ${distname}${extract.suffix} sha256 aaaa size 1 pinned-v8.gz sha256 cccc size 4
master_sites-append https://invalid.example/frozen:pin
distfiles-append pinned-v8.gz:pin
extract.only ${distname}${extract.suffix}
extract.rename no
`
			}
			if oldName != "" {
				extra += "cargo.crates old 1.2.3 " + sha + "\n"
			}
			if strings.HasPrefix(scenario, "cargo-update") {
				extra += "cargo.update yes\n"
			}
			if strings.HasPrefix(scenario, "shared") {
				// A buildable subport sharing the version, the checksums, and
				// the regenerated block, as atuin-server shares atuin's.
				extra += "subport fixture-server {}\n"
			}
			var requests atomic.Int64
			service, request := versionFixture(t, "setup", extra, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if strings.Contains(r.URL.Path, "/1.0/") {
					_, _ = w.Write(before)
				} else {
					_, _ = w.Write(after)
				}
			})
			script := "if /usr/bin/grep -q 'name = \"old\"' \"$1\"; then\nprintf '%s\\n' 'cargo.crates old 1.2.3 " + sha + "'\nelif /usr/bin/grep -q 'name = \"new\"' \"$1\"; then\nprintf '%s\\n' 'cargo.crates new 1.2.3 " + sha + "'\nfi"
			if scenario == "partial" {
				script = "exit 0"
			}
			if scenario == "failed" || scenario == "unsupported-context" {
				script = "echo 'invalid lockfile' >&2;exit 4"
			}
			executable := dependencyHelper(t, script)
			if scenario == "missing" {
				executable = filepath.Join(t.TempDir(), "absent")
			}
			service.DependencyTools = depblock.Tools{Cargo2Port: executable, Go2Port: "absent"}
			// A main-port selection authorizes the siblings sharing its
			// release; a named subport still needs the flag.
			if scenario == "shared-unauthorized" {
				request.Selection.Subport = "fixture-server"
			}
			result, err := service.Prepare(t.Context(), request)
			switch scenario {
			case "shared-unauthorized":
				require.ErrorContains(t, err, "fixture, another port of the same Portfile, moves with fixture-server's release")
				require.ErrorContains(t, err, "--shared-release moves both")
			case "unsupported-context":
				require.ErrorContains(t, err, "pre-fetch hook 1 ends with `set distfiles changed.tar.gz` rather than return -code error, which writes `distfiles`, at Portfile line 46")
				require.NotContains(t, err.Error(), "dependency resolution failed")
				require.Zero(t, requests.Load(), "local refusal must precede old-source download and helper")
			case "missing":
				require.ErrorContains(t, err, "missing executable cargo2port")
				require.Zero(t, requests.Load())
			case "failed":
				require.ErrorContains(t, err, "invalid lockfile")
			case "partial":
				require.ErrorContains(t, err, "registry checksums exactly")
			case "cargo-update-unlocked":
				require.ErrorContains(t, err, "cargo.update is on and 1.0's source ships no Cargo.lock")
			default:
				require.NoError(t, err)
				require.NotEmpty(t, result.PreparedTree)
				require.Equal(t, request.Source, result.Base)
				require.NotContains(t, string(result.Files[0].After), "cargo.crates old")
				if newName != "" {
					require.Contains(t, string(result.Files[0].After), "new 1.2.3")
				}
				require.Equal(t, int64(2), requests.Load())
				if scenario == "auxiliary" {
					require.Contains(t, string(result.Files[0].After), "pinned-v8.gz sha256 cccc size 4")
					require.Len(t, result.Downloads, 1)
				}
				if scenario == "cargo-update" {
					// Taken, said, and the option left as it was (D19).
					require.Contains(t, result.Regenerated[0].Notices, "cargo.update is on; MacPorts re-resolves offline against these crates.")
					require.Contains(t, string(result.Files[0].After), "cargo.update yes\n")
				}
				if scenario == "shared" {
					require.NotNil(t, result.Scope)
					require.Len(t, result.Scope.Affected, 2, "the subport moves with the port through the regenerated block")
				}
			}
			if err != nil {
				require.Empty(t, result.PreparedTree)
			}
		})
	}
}

func TestCargoGitArchivesUseEvaluatedPortGroupLocations(t *testing.T) {
	t.Parallel()
	oldCommit, newCommit := strings.Repeat("a", 40), strings.Repeat("b", 40)
	gitBody := "git dependency archive"
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(gitBody)))
	lock := func(commit string) string {
		return fmt.Sprintf(`version = 4
[[package]]
name = "gitdep"
version = "0.1.0"
source = "git+https://github.com/owner/gitdep?branch=main#%s"
`, commit)
	}
	before := manifestArchive(t, "Cargo.lock", lock(oldCommit), "1.0")
	after := manifestArchive(t, "Cargo.lock", lock(newCommit), "2.0")
	extra := `options cargo.crates cargo.crates_github
 default cargo.crates {}
 default cargo.crates_github {}
 proc fixture_git_crates {} {
  set site [lindex [option master_sites] 0]
  foreach {name repo branch commit sum} [option cargo.crates_github] {
   set file ${name}-${commit}.tar.gz
   distfiles-append ${file}:gitcrate
   master_sites-append ${site}/git/${commit}.tar.gz?dummy=:gitcrate
   checksums-append ${file} sha256 $sum
  }
 }
 port::register_callback fixture_git_crates
 ` + "cargo.crates_github gitdep owner/gitdep main " + oldCommit + " " + checksum + "\n"
	var gitDownloads atomic.Int64
	service, request := versionFixture(t, "setup", extra, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/git/") {
			gitDownloads.Add(1)
			require.Contains(t, r.URL.RawQuery, "dummy=/gitdep-")
			fmt.Fprint(w, gitBody)
		} else if strings.Contains(r.URL.Path, "/1.0/") {
			_, _ = w.Write(before)
		} else {
			_, _ = w.Write(after)
		}
	})
	service.DependencyTools.Cargo2Port = dependencyHelper(t, "exit 0")
	result, err := service.Prepare(t.Context(), request)
	require.NoError(t, err)
	require.NotEmpty(t, result.PreparedTree)
	require.Equal(t, int64(2), gitDownloads.Load())
	require.Len(t, result.Downloads, 1, "the port's source")
	require.Len(t, result.Crates, 1, "the Git crate, apart, since the current version has no counterpart to compare")
	require.Contains(t, string(result.Files[0].After), "gitdep owner/gitdep main "+newCommit+" "+checksum)
	require.NotContains(t, string(result.Files[0].After), oldCommit)
}

func TestCargoRevPinnedCratesStayOnlineWhenThePortDisablesOfflineMode(t *testing.T) {
	t.Parallel()
	oldCommit, newCommit := strings.Repeat("a", 40), strings.Repeat("b", 40)
	lock := func(commit string) string {
		return fmt.Sprintf(`version = 4
[[package]]
name = "pinned"
version = "0.1.0"
source = "git+https://github.com/owner/pinned?rev=%s#%s"
`, commit, commit)
	}
	before := manifestArchive(t, "Cargo.lock", lock(oldCommit), "1.0")
	after := manifestArchive(t, "Cargo.lock", lock(newCommit), "2.0")
	extra := "options cargo.crates cargo.crates_github cargo.offline_cmd\n default cargo.crates {}\n default cargo.crates_github {}\n default cargo.offline_cmd {--frozen}\n"
	handler := func(w http.ResponseWriter, r *http.Request) {
		require.NotContains(t, r.URL.Path, "/git/")
		if strings.Contains(r.URL.Path, "/1.0/") {
			_, _ = w.Write(before)
		} else {
			_, _ = w.Write(after)
		}
	}
	service, request := versionFixture(t, "setup", extra, handler)
	service.DependencyTools.Cargo2Port = dependencyHelper(t, "exit 0")
	_, err := service.Prepare(t.Context(), request)
	require.ErrorContains(t, err, "pinned is pinned to Git rev "+oldCommit)
	require.ErrorContains(t, err, "checking the existing cargo.crates against 1.0's source, the version now", "says whose lock it read (field testing, batch 11: halloy)")
	require.ErrorContains(t, err, "declares branches only")

	service, request = versionFixture(t, "setup", extra+"# Disable offline mode to work around Git dependencies\ncargo.offline_cmd\n", handler)
	service.DependencyTools.Cargo2Port = dependencyHelper(t, "exit 0")
	result, err := service.Prepare(t.Context(), request)
	require.NoError(t, err)
	require.NotEmpty(t, result.PreparedTree)
	require.Len(t, result.Downloads, 1, "no Git crate archive is fetched for an online crate")
	contents := string(result.Files[0].After)
	require.NotContains(t, contents, "\ncargo.crates_github", "no declaration is added for online crates")
	require.NotContains(t, contents, newCommit)
	require.Contains(t, contents, "# Disable offline mode to work around Git dependencies\ncargo.offline_cmd\n")
}

// A port that declares its crates refreshes its own archive's checksums as
// any port does: the crates' declarations are Cargo.lock's, set aside while
// the archive is fetched and checked, and put back as they were, byte for
// byte, with no crate fetched. create writes such a port, and couldn't
// fill in its checksums, nor could checksums after it (the txt run's
// finding 1). Where the crates' checksums come before the port's own, the
// two can't be told apart, and it's refused.
func TestAPortWithCratesRefreshesItsOwnChecksums(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("b", 64)
	for _, order := range []string{"after", "before"} {
		t.Run(order, func(t *testing.T) {
			appendCrate := "checksums-append ${name}-${version}.crate sha256 $checksum"
			if order == "before" {
				appendCrate = "checksums ${name}-${version}.crate sha256 $checksum {*}[option checksums]"
			}
			extra := `options cargo.crates cargo.crates_github cargo.update cargo.dir
 default cargo.crates {}
 default cargo.crates_github {}
 default cargo.update no
 default cargo.dir {${worksrcpath}}
 proc fixture_crates {} {
  foreach {name version checksum} [option cargo.crates] {
   distfiles-append ${name}-${version}.crate:crate-${name}
   master_sites-append https://static.crates.io/crates/${name}:crate-${name}
   ` + appendCrate + `
  }
 }
 port::register_callback fixture_crates
cargo.crates old 1.2.3 ` + sha + "\n"
			archive := []byte("fixture 1.0 as upstream serves it now")
			var requests atomic.Int64
			service, request := versionFixture(t, "setup", extra, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = w.Write(archive)
			})
			request.Action, request.Version, request.Release = model.EditChecksums, "", nil
			result, err := service.Prepare(t.Context(), request)
			if order == "before" {
				require.ErrorContains(t, err, "reads its cargo.crates's checksums before its own archives'")
				return
			}
			require.NoError(t, err)
			require.Len(t, result.Files, 1)
			after := string(result.Files[0].After)
			sum := sha256.Sum256(archive)
			require.Contains(t, after, "sha256 "+hex.EncodeToString(sum[:]), "the port's own archive is refreshed")
			require.Contains(t, after, "\ncargo.crates old 1.2.3 "+sha+"\n", "the crates are as they were")
			require.Equal(t, int64(1), requests.Load(), "no crate is fetched")
			require.Equal(t, request.Source, result.Base)
		})
	}
}

// A crate the Portfile pins over its lock is an override: kept back while
// the new lock is still below it, and dropped, said, once the new lock
// moves past it. Any other difference is refused, named. termusic pinned
// soundtouch 0.4.1 over its lock's 0.4.0, and the refusal named nothing
// (field testing, 2026-10-02).
func TestCargoOverridesAreNamedAndDroppedOnceTheLockPassesThem(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("b", 64)
	lock := func(version string) string {
		return fmt.Sprintf("version = 4\n[[package]]\nname = \"fixture\"\nversion = \"1.0.0\"\n[[package]]\nname = \"soundtouch\"\nversion = %q\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = %q\n", version, sha)
	}
	// The helper writes the lock's one crate as cargo2port does.
	helper := "version=$(/usr/bin/sed -n 's/^version = \"\\(0[^\"]*\\)\"$/\\1/p' \"$1\")\nprintf 'cargo.crates soundtouch %s " + sha + "\\n' \"$version\""
	extra := "options cargo.crates cargo.crates_github\n default cargo.crates {}\n default cargo.crates_github {}\n"
	for _, test := range []struct {
		name, declared, next, err string
	}{
		{"moved past", "soundtouch 0.4.1 " + sha, "0.5.4", ""},
		{"caught up", "soundtouch 0.4.1 " + sha, "0.4.1", ""},
		{"still below", "soundtouch 0.4.1 " + sha, "0.4.0", "existing cargo.crates pins soundtouch 0.4.1 over the lock's 0.4.0, and 2.0's lock doesn't move past it"},
		{"another difference", "soundtouch 0.4.0 " + sha + " stray 1.0.0 " + sha, "0.5.4", "existing cargo.crates differs from the original manifest/helper output for stray"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := manifestArchive(t, "Cargo.lock", lock("0.4.0"), "1.0")
			after := manifestArchive(t, "Cargo.lock", lock(test.next), "2.0")
			service, request := versionFixture(t, "setup", extra+"cargo.crates "+test.declared+"\n", func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/1.0/") {
					_, _ = w.Write(before)
				} else {
					_, _ = w.Write(after)
				}
			})
			service.DependencyTools = depblock.Tools{Cargo2Port: dependencyHelper(t, helper), Go2Port: "absent"}
			result, err := service.Prepare(t.Context(), request)
			if test.err != "" {
				require.ErrorContains(t, err, test.err)
				return
			}
			require.NoError(t, err)
			require.Contains(t, string(result.Files[0].After), "soundtouch "+test.next+" "+sha)
			changed := 1
			if test.next == "0.4.1" {
				changed = 0 // the lock caught up with the pin, which reads the same
			} else {
				require.NotContains(t, string(result.Files[0].After), "0.4.1 "+sha)
			}
			require.Equal(t, []editprep.Regenerated{{Option: "cargo.crates", Count: 1, Changed: changed, Dropped: []editprep.Override{{Name: "soundtouch", Pinned: "0.4.1", Was: "0.4.0", Locked: test.next}}}}, result.Regenerated[:1])
		})
	}
}

// A Git crate the lock pins by rev, left to Cargo's online resolution,
// that the Portfile declares all the same stays declared under the
// Portfile's own label, at the new commit: pgdog declares its rev pins
// as "master" (field testing, 2026-10-02).
func TestADeclaredRevPinnedCrateKeepsThePortfilesLabel(t *testing.T) {
	t.Parallel()
	oldCommit, newCommit := strings.Repeat("a", 40), strings.Repeat("c", 40)
	gitBody := "git dependency archive"
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(gitBody)))
	lock := func(commit string) string {
		return fmt.Sprintf("version = 4\n[[package]]\nname = \"scram\"\nversion = \"0.1.0\"\nsource = \"git+https://github.com/pgdogdev/scram?rev=%s#%s\"\n", commit, commit)
	}
	before := manifestArchive(t, "Cargo.lock", lock(oldCommit), "1.0")
	after := manifestArchive(t, "Cargo.lock", lock(newCommit), "2.0")
	extra := `options cargo.crates cargo.crates_github cargo.offline_cmd
 default cargo.crates {}
 default cargo.crates_github {}
 default cargo.offline_cmd {--frozen}
 proc fixture_git_crates {} {
  set site [lindex [option master_sites] 0]
  foreach {name repo branch commit sum} [option cargo.crates_github] {
   set file ${name}-${commit}.tar.gz
   distfiles-append ${file}:gitcrate
   master_sites-append ${site}/git/${commit}.tar.gz?dummy=:gitcrate
   checksums-append ${file} sha256 $sum
  }
 }
 port::register_callback fixture_git_crates
cargo.offline_cmd
cargo.crates_github scram pgdogdev/scram master ` + oldCommit + " " + checksum + "\n"
	service, request := versionFixture(t, "setup", extra, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/git/"):
			fmt.Fprint(w, gitBody)
		case strings.Contains(r.URL.Path, "/1.0/"):
			_, _ = w.Write(before)
		default:
			_, _ = w.Write(after)
		}
	})
	service.DependencyTools.Cargo2Port = dependencyHelper(t, "exit 0")
	result, err := service.Prepare(t.Context(), request)
	require.NoError(t, err)
	contents := string(result.Files[0].After)
	require.Contains(t, contents, "scram pgdogdev/scram master "+newCommit+" "+checksum)
	require.NotContains(t, contents, oldCommit)
	require.NotContains(t, contents, "?rev=")
	// They're kept, and said to look unused: a notice, not a hold (the
	// person's word, 2026-10-02).
	var inert string
	for _, block := range result.Regenerated {
		inert += strings.Join(block.Notices, "")
	}
	require.Equal(t, "cargo.crates_github declares scram (pinned by rev) under a branch, where the lock pins them otherwise; Cargo's source replacement matches only a branch, so they're resolved online and the declarations look unused.", inert)
}

// A Go port whose go.vendors is declared empty, as create writes one, has
// it written on a checksums refresh, through go2port, from its own
// source's go.mod: an empty block holds no override to keep. mods 1.8.1's
// Portfile couldn't build without it (field testing, batch 58).
func TestChecksumsFillAnEmptyGoVendorsBlock(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	source := manifestArchive(t, "go.mod", "module github.com/owner/fixture\ngo 1.24\nrequire example.com/dep v1.0.0\n", "1.0")
	extra := `options go.vendors
 default go.vendors {}
 proc fixture_vendors {} {
  foreach {module lock value sha checksum} [option go.vendors] {
   distfiles-append dep.tar.gz:vendor
   master_sites-append https://invalid.example:vendor
   checksums-append dep.tar.gz sha256 $checksum
  }
 }
 port::register_callback fixture_vendors
go.vendors
`
	service, request := versionFixture(t, "go-setup", extra, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(source) })
	service.DependencyTools = depblock.Tools{Go2Port: dependencyHelper(t, "printf '%s\\n' 'go.vendors example.com/dep lock v1.0.0 sha256 "+sha+"'"), Cargo2Port: "absent"}
	request.Action, request.Version, request.Release = model.EditChecksums, "", nil
	result, err := service.Prepare(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, result.Files, 1)
	after := string(result.Files[0].After)
	sum := sha256.Sum256(source)
	require.Contains(t, after, "sha256 "+hex.EncodeToString(sum[:]), "the port's own archive is refreshed")
	require.Contains(t, after, "example.com/dep lock v1.0.0 sha256 "+sha, "and its modules written")
	require.Equal(t, []editprep.Regenerated{{Option: "go.vendors", Count: 1, Changed: 1}}, result.Regenerated)
}
