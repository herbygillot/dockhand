package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func automaticCLI(t *testing.T, current string) (app.Config, *git.Repository, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	config, repo, original := preparationCLI(t)
	downloads, catalogs := &atomic.Int64{}, &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/project/releases":
			catalogs.Add(1)
			fmt.Fprint(w, `[{"tag_name":"v2.0","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z"},{"tag_name":"v3.0","draft":false,"prerelease":true,"published_at":"2026-01-02T00:00:00Z"}]`)
		case "/repos/owner/project/git/ref/tags/v2.0":
			fmt.Fprintf(w, `{"ref":"refs/tags/v2.0","object":{"type":"commit","sha":%q}}`, strings.Repeat("a", 40))
		case "/dist/2.0/fixture-2.0.tar.gz":
			downloads.Add(1)
			fmt.Fprint(w, "fixture archive bytes")
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	config.GitHub = github.Config{BaseURL: server.URL}
	config.Tart.Executable = "/missing/tart"
	contents := fmt.Sprintf(`PortSystem 1.0
name fixture
version %s
revision 3
categories devel
options github.author github.project github.version github.tag_prefix github.tag_suffix github.tarball_from git.branch
github.author owner
github.project project
github.version ${version}
github.tag_prefix v
github.tag_suffix ""
github.tarball_from releases
git.branch v${version}
livecheck.type regex
livecheck.url https://github.com/owner/project/tags
livecheck.regex {archive/refs/tags/v(\d+(\.\d+)+)\.tar\.gz}
livecheck.version ${version}
master_sites %s/dist/${version}
checksums sha256 %s size 1
`, current, server.URL, strings.Repeat("0", 64))
	_, tree, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	before, _, err := repo.File(t.Context(), tree, "devel/fixture/Portfile")
	require.NoError(t, err)
	tree, err = repo.EditTree(t.Context(), tree, []git.FileEdit{{Path: "devel/fixture/Portfile", Before: before, After: []byte(contents), Mode: before.Mode}})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{original}, Message: "automatic fixture", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: original}, Desired: git.RefValue{Exists: true, Object: commit}}, {Name: "refs/heads/master", Expected: git.RefValue{Exists: true, Object: original}, Desired: git.RefValue{Exists: true, Object: commit}}}))
	return config, repo, downloads, catalogs
}

func TestAutomaticBumpCLIUsesResolvedReleaseAndPreparedBranch(t *testing.T) {
	t.Parallel()
	config, repo, downloads, catalogs := automaticCLI(t, "1.0")
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump", "fixture", "--diff", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var preview app.Preview
	decodeResult(t, stdout.Bytes(), &preview)
	require.Contains(t, preview.Diff, "+version 2.0")
	require.False(t, preview.Preparation.Release.NoUpdate)
	require.Empty(t, preview.Preparation.Release.Requested)
	require.NoDirExists(t, filepath.Dir(config.DBPath))
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"bump", "fixture", "--no-publish", "--skip-verify", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var result ActionResult
	decodeResult(t, stdout.Bytes(), &result)
	job := result.Status.Jobs[0].Job
	require.Equal(t, record.JobCompleted, job.State)
	require.Empty(t, job.Spec.Version)
	require.Equal(t, "2.0", job.ResolvedRelease.Version)
	require.Equal(t, "1.0", job.ResolvedRelease.CurrentVersion)
	require.NotEmpty(t, job.ResultRevision)
	require.Equal(t, int64(2), downloads.Load())
	require.Equal(t, int64(2), catalogs.Load())
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"wait", "--job", string(job.ID), "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
	require.Equal(t, int64(2), catalogs.Load())
	require.NoFileExists(t, filepath.Join(repo.CommonDir, "index"))
}

func TestCurrentBumpCLIIsSuccessfulWithoutDownloadsBranchOrProvider(t *testing.T) {
	t.Parallel()
	config, repo, downloads, catalogs := automaticCLI(t, "2.0")
	before, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump", "fixture", "--diff"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "fixture: already current at 2.0")
	require.NoDirExists(t, filepath.Dir(config.DBPath))
	require.Zero(t, downloads.Load())
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"bump", "fixture", "--no-publish", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var result ActionResult
	decodeResult(t, stdout.Bytes(), &result)
	job := result.Status.Jobs[0].Job
	require.Equal(t, record.JobCompleted, job.State)
	require.True(t, job.ResolvedRelease.NoUpdate)
	require.Nil(t, job.Prepared)
	require.Nil(t, job.AdmittedAt)
	require.Empty(t, job.ResultRevision)
	require.Empty(t, result.Status.Jobs[0].Attempts)
	require.Len(t, result.Status.Changes, 1)
	require.Equal(t, record.ChangeClosed, result.Status.Changes[0].Disposition)
	require.Equal(t, record.VerificationRequired, job.Spec.Verification)
	require.NotEmpty(t, job.Spec.Preparation.VerificationProblem, "missing image does not matter for a no-op")
	after, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Zero(t, downloads.Load())
	observed := catalogs.Load()
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"wait", "--job", string(job.ID), "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
	require.Equal(t, observed, catalogs.Load())
}

func TestFailedAutomaticDiscoveryCLIRequiresAttention(t *testing.T) {
	t.Parallel()
	config, _, downloads, _ := automaticCLI(t, "1.0")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	config.GitHub.BaseURL = server.URL
	var stdout, stderr bytes.Buffer
	err := Run(t.Context(), []string{"bump", "fixture", "--no-publish", "--skip-verify", "--json"}, Streams{Out: &stdout, Err: &stderr}, config)
	require.ErrorIs(t, err, errNeedsAttention)
	var result ActionResult
	decodeResult(t, stdout.Bytes(), &result)
	job := result.Status.Jobs[0].Job
	require.Equal(t, record.JobNeedsAttention, job.State)
	require.Nil(t, job.ResolvedRelease)
	require.Nil(t, job.Prepared)
	require.Contains(t, job.Detail, "503")
	require.Contains(t, job.Detail, "/repos/owner/project/releases")
	require.Zero(t, downloads.Load())
}
