package preparation_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/stretchr/testify/require"
)

func manifestArchive(t *testing.T, name, manifest, version string) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "fixture-" + version + "/" + name, Mode: 0600, Size: int64(len(manifest)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(manifest))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return data.Bytes()
}
func dependencyHelper(t *testing.T, body string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "dependency helper")
	require.NoError(t, os.WriteFile(file, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0700))
	return file
}
func TestGoDependencyPreparation(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, scenario := range []string{"success", "removed", "missing", "failed", "partial", "override", "patched", "unsupported-context"} {
		t.Run(scenario, func(t *testing.T) {
			old := "module github.com/owner/fixture\ngo 1.24\nrequire example.com/old v1.0.0\n"
			next := "module github.com/owner/fixture\ngo 1.24\nrequire example.com/new/v2 v2.0.0\n"
			if scenario == "removed" {
				next = "module github.com/owner/fixture\ngo 1.24\n"
			}
			before := manifestArchive(t, "go.mod", old, "1.0")
			after := manifestArchive(t, "go.mod", next, "2.0")
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
			if scenario == "unsupported-context" {
				extra += "if {${os.major} < 23 && ${build_arch} eq \"arm64\"} { pre-fetch { set distfiles changed.tar.gz } }\n"
			}
			var requests atomic.Int64
			service, request := versionFixture(t, "go-setup", extra, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if strings.Contains(r.URL.Path, "/1.0/") {
					_, _ = w.Write(before)
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
			service.DependencyTools = dependency.Tools{Go2Port: executable, Cargo2Port: "absent"}
			result, err := service.Prepare(t.Context(), request)
			switch scenario {
			case "unsupported-context":
				require.ErrorContains(t, err, "pre-fetch hook 1 has unrecognized behavior")
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
				if scenario == "success" {
					require.Contains(t, string(result.Files[0].After), "example.com/new/v2 lock v2.0.0")
				}
				require.Equal(t, int64(2), requests.Load())
				require.Len(t, result.Downloads, 1)
				for _, fidelity := range result.Fidelity {
					require.Empty(t, fidelity.UnexpectedChanges)
				}
			}
			if err != nil {
				require.Empty(t, result.PreparedTree)
			}
		})
	}
}
func TestCargoDependencyPreparation(t *testing.T) {
	sha := strings.Repeat("b", 64)
	for _, scenario := range []string{"success", "auxiliary", "added", "removed", "missing", "failed", "partial"} {
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
			service.DependencyTools = dependency.Tools{Cargo2Port: executable, Go2Port: "absent"}
			result, err := service.Prepare(t.Context(), request)
			switch scenario {
			case "unsupported-context":
				require.ErrorContains(t, err, "pre-fetch hook 1 has unrecognized behavior")
				require.NotContains(t, err.Error(), "dependency resolution failed")
				require.Zero(t, requests.Load(), "local refusal must precede old-source download and helper")
			case "missing":
				require.ErrorContains(t, err, "missing executable cargo2port")
				require.Zero(t, requests.Load())
			case "failed":
				require.ErrorContains(t, err, "invalid lockfile")
			case "partial":
				require.ErrorContains(t, err, "registry checksums exactly")
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
			}
			if err != nil {
				require.Empty(t, result.PreparedTree)
			}
		})
	}
}

func TestCargoGitArchivesUseEvaluatedPortGroupLocations(t *testing.T) {
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
	require.Len(t, result.Downloads, 2)
	require.Contains(t, string(result.Files[0].After), "gitdep owner/gitdep main "+newCommit+" "+checksum)
	require.NotContains(t, string(result.Files[0].After), oldCommit)
}
