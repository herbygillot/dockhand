package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/stretchr/testify/require"
)

func TestBumpParsesOptionalVersionWithoutInitializingState(t *testing.T) {
	t.Parallel()
	config := app.Config{DBPath: filepath.Join(t.TempDir(), "absent", "state.db"), Repository: "/missing/repository"}
	for _, args := range [][]string{{"bump", "jq"}, {"bump", "jq", "v1.8.1"}, {"bump", "jq", "1.8.1"}} {
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.ErrorContains(t, err, "git rev-parse")
	}
	for _, args := range [][]string{{"bump"}, {"bump", "jq", "1", "2"}, {"bump-revision", "jq", "1"}, {"bump", "jq", "1", "--dry-run", "--detach"}, {"bump-revision", "jq", "--dry-run", "--branch="}, {"bump-revision", "jq", "--dry-run", "--variant=bad"}, {"bump", "jq", "--publish"}, {"bump-revision", "jq", "--wait"}, {"bump", "jq", "--dry-run", "--to", "verified"}, {"bump", "jq", "--trace", "--detach"}, {"bump", "jq", "--provider", "tart", "--to", "verified", "--remote", "origin"}, {"bump-revision", "jq", "--provider", "tart", "--to", "branch", "--base", "main"}, {"bump", "jq", "--provider", "tart", "--to", "verified", "--upstream", "upstream"}} {
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.Error(t, err)
		require.NotErrorIs(t, err, errNotImplemented)
		require.NotContains(t, err.Error(), "git ")
	}
	var out bytes.Buffer
	require.ErrorIs(t, Run(t.Context(), []string{"bump", "jq", ""}, Streams{Out: &out, Err: &out}, config), upstream.ErrVersionInput)
	require.ErrorContains(t, Run(t.Context(), []string{"bump", "jq", "2", "--dry-run"}, Streams{Out: &out, Err: &out}, config), "git rev-parse")
	require.NoDirExists(t, filepath.Dir(config.DBPath))
}

func TestRevisionPreviewCLIUsesCommittedSourceWithoutStateOrProvider(t *testing.T) {
	t.Parallel()
	config, repo, commit := preparationCLI(t)
	var err error
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--dry-run", "--subject", "rebuild", "-v", "--timestamps"}, Streams{Out: &stdout, Err: &stderr}, config))
	require.Contains(t, stdout.String(), "-revision 0\n+revision 1")
	require.Contains(t, stderr.String(), "branch master")
	for _, line := range strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n") {
		require.Regexp(t, `^[0-9]{2}:[0-9]{2}:[0-9]{2} `, line, "--timestamps prefixes every progress line")
	}
	require.Contains(t, stderr.String(), "working-tree edits are excluded")
	require.NoDirExists(t, filepath.Dir(config.DBPath))
	require.NoFileExists(t, filepath.Join(repo.CommonDir, "index"))
	actual, _, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, commit, actual)
	stdout.Reset()
	stderr.Reset()
	err = Run(t.Context(), []string{"bump-revision", "fixture", "--dry-run", "--json"}, Streams{Out: &stdout, Err: &stderr}, config)
	require.ErrorContains(t, err, "--subject", "a revision bump without a reason is refused before anything is read")
	require.NoDirExists(t, filepath.Dir(config.DBPath))
	err = Run(t.Context(), []string{"bump-revision", "fixture", "--dry-run", "--json", "--subject", "rebuild", "--closes", "ticket"}, Streams{Out: &stdout, Err: &stderr}, config)
	require.ErrorContains(t, err, "--closes takes a Trac ticket number or a URL")
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--dry-run", "--json", "-vv", "--subject", "rebuild", "--closes", "74379", "--see", "#74422", "--see", "https://github.com/macports/macports-ports/issues/1"}, Streams{Out: &stdout, Err: &stderr}, config))
	var result app.Preview
	decodeResult(t, stdout.Bytes(), &result)
	require.Equal(t, "master", result.Branch)
	require.Contains(t, result.Diff, "+revision 1")
	require.Len(t, result.Preparation.Commits, 1)
	require.Equal(t, "fixture: rebuild", result.Preparation.Commits[0].Subject)
	require.Equal(t, []record.Reference{{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74379"}, {Relation: record.ReferenceSee, URL: "https://trac.macports.org/ticket/74422"}, {Relation: record.ReferenceSee, URL: "https://github.com/macports/macports-ports/issues/1"}}, result.Preparation.Commits[0].References)
	require.Contains(t, stderr.String(), "PortIndex for source")
	require.NoDirExists(t, filepath.Dir(config.DBPath))
}

func preparationCLI(t *testing.T) (app.Config, *git.Repository, string) {
	t.Helper()
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts port-tclsh is required for CLI integration test")
	}
	root := t.TempDir()
	output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", "-b", "candidate", root).CombinedOutput()
	require.NoError(t, err, "%s", output)
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	blob, err := repo.WriteBlob(t.Context(), []byte("PortSystem 1.0\nname fixture\nversion 1.2\nrevision 0\ncategories devel\n"))
	require.NoError(t, err)
	tree, err := repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Mode: 0o100644, Type: "blob", Object: blob}})
	require.NoError(t, err)
	tree, err = repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "fixture", Mode: 0o40000, Type: "tree", Object: tree}})
	require.NoError(t, err)
	tree, err = repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "devel", Mode: 0o40000, Type: "tree", Object: tree}})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Message: "fixture", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Desired: git.RefValue{Exists: true, Object: commit}}}))
	config := app.Config{Repository: root, DBPath: filepath.Join(t.TempDir(), "absent", "state.db"), TclExecutable: executable}
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/master", Desired: git.RefValue{Exists: true, Object: commit}}}))
	for _, setting := range [][2]string{{"user.name", "Fixture"}, {"user.email", "fixture@example.invalid"}, {"url." + root + ".insteadOf", "https://github.com/macports/macports-ports.git"}} {
		output, err = exec.CommandContext(t.Context(), "git", "-C", root, "config", setting[0], setting[1]).CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	return config, repo, commit
}

func TestRevisionBumpCLITracksCommittedChangeAndPreservesCheckout(t *testing.T) {
	t.Parallel()
	config, repo, commit := preparationCLI(t)
	out, err := exec.CommandContext(t.Context(), "git", "-C", repo.Root, "reset", "--hard", commit).CombinedOutput()
	require.NoError(t, err, "%s", out)
	portfile := filepath.Join(repo.Root, "devel/fixture/Portfile")
	require.NoError(t, os.WriteFile(portfile, []byte("staged edits"), 0600))
	out, err = exec.CommandContext(t.Context(), "git", "-C", repo.Root, "add", "devel/fixture/Portfile").CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.NoError(t, os.WriteFile(portfile, []byte("unstaged edits"), 0600))
	index, err := os.ReadFile(filepath.Join(repo.CommonDir, "index"))
	require.NoError(t, err)
	config.Tart = tart.Config{Executable: "/missing/tart"}
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--to", "branch", "--json", "-v", "--subject", "Rebuild fixture"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var result ActionResult
	decodeResult(t, stdout.Bytes(), &result)
	require.Len(t, result.Status.Jobs, 1)
	job := result.Status.Jobs[0].Job
	require.Equal(t, record.JobCompleted, job.State)
	require.Equal(t, record.VerificationSkipped, job.Spec.Verification)
	require.Empty(t, result.Status.Jobs[0].Attempts)
	require.Contains(t, stderr.String(), "working-tree edits are excluded")
	require.Equal(t, record.ObjectID(commit), job.Spec.Source.Commit)
	prepared, tree, err := repo.Branch(t.Context(), job.Prepared.Branch)
	require.NoError(t, err)
	require.Equal(t, string(job.Prepared.Source.Commit), prepared)
	_, data, err := repo.File(t.Context(), tree, "devel/fixture/Portfile")
	require.NoError(t, err)
	require.Contains(t, string(data), "revision 1")
	actual, _, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, commit, actual)
	afterIndex, err := os.ReadFile(filepath.Join(repo.CommonDir, "index"))
	require.NoError(t, err)
	require.Equal(t, index, afterIndex)
	afterFile, err := os.ReadFile(portfile)
	require.NoError(t, err)
	require.Equal(t, "unstaged edits", string(afterFile))
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"wait", "--job", string(job.ID), "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
	decodeResult(t, stdout.Bytes(), &result)
	require.Equal(t, job.ResultRevision, result.Status.Jobs[0].Job.ResultRevision)
}

func TestRevisionBumpCLIPreservesBranchWhenVerificationCannotStart(t *testing.T) {
	t.Parallel()
	for _, image := range []string{"", "unavailable-image"} {
		t.Run("image="+image, func(t *testing.T) {
			config, repo, _ := preparationCLI(t)
			config.Tart = tart.Config{Executable: "/missing/tart", Image: image}
			var stdout, stderr bytes.Buffer
			err := Run(t.Context(), []string{"bump-revision", "fixture", "--subject", "rebuild", "--provider", "tart", "--to", "verified", "--json"}, Streams{Out: &stdout, Err: &stderr}, config)
			require.ErrorIs(t, err, errNeedsAttention, "%s", stderr.String())
			var result ActionResult
			decodeResult(t, stdout.Bytes(), &result)
			job := result.Status.Jobs[0].Job
			require.Equal(t, record.VerificationRequired, job.Spec.Verification)
			require.Equal(t, record.JobNeedsAttention, job.State)
			require.NotEmpty(t, job.Spec.Preparation.VerificationProblem)
			require.Contains(t, job.Detail, "prepared branch is preserved")
			actual, _, err := repo.Branch(t.Context(), job.Prepared.Branch)
			require.NoError(t, err)
			require.Equal(t, string(job.Prepared.Source.Commit), actual)
		})
	}
}

func TestExplicitVersionBumpCLIFromPreviewToTrackedBranch(t *testing.T) {
	t.Parallel()
	config, repo, original := preparationCLI(t)
	body := "source archive fixture"
	upstreamCommit := strings.Repeat("a", 40)
	var downloads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/project/git/ref/tags/v2.0":
			fmt.Fprintf(w, `{"ref":"refs/tags/v2.0","object":{"type":"commit","sha":%q}}`, upstreamCommit)
		case "/dist/2.0/fixture-2.0.tar.gz":
			downloads.Add(1)
			fmt.Fprint(w, body)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	config.GitHub = github.Config{BaseURL: server.URL}
	config.Tart.Executable = "/missing/tart"
	contents := fmt.Sprintf(`PortSystem 1.0
name fixture
version 1.2
revision 3
categories devel
options github.author github.project github.version github.tag_prefix github.tag_suffix git.branch
github.author owner
github.project project
github.version ${version}
github.tag_prefix v
github.tag_suffix ""
git.branch v${version}
master_sites %s/dist/${version}
checksums rmd160 %s \
    sha256 %s \
    size 1
`, server.URL, strings.Repeat("0", 40), strings.Repeat("0", 64))
	_, tree, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	before, _, err := repo.File(t.Context(), tree, "devel/fixture/Portfile")
	require.NoError(t, err)
	tree, err = repo.EditTree(t.Context(), tree, []git.FileEdit{{Path: "devel/fixture/Portfile", Before: before, After: []byte(contents), Mode: before.Mode}})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{original}, Message: "version fixture", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: original}, Desired: git.RefValue{Exists: true, Object: commit}}, {Name: "refs/heads/master", Expected: git.RefValue{Exists: true, Object: original}, Desired: git.RefValue{Exists: true, Object: commit}}}))
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump", "fixture", "2.0", "--dry-run", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var preview app.Preview
	decodeResult(t, stdout.Bytes(), &preview)
	require.Equal(t, "v2.0", preview.Preparation.Release.Tag)
	require.Contains(t, preview.Diff, "-revision 3")
	require.Contains(t, preview.Diff, "+revision 0")
	require.NoDirExists(t, filepath.Dir(config.DBPath))
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"bump", "fixture", "v2.0", "--to", "branch", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var result ActionResult
	decodeResult(t, stdout.Bytes(), &result)
	require.Len(t, result.Status.Jobs, 1)
	job := result.Status.Jobs[0].Job
	require.Equal(t, record.Bump, job.Spec.Action)
	require.Equal(t, record.JobCompleted, job.State)
	require.Equal(t, "v2.0", job.Spec.Version)
	require.Equal(t, "2.0", job.ResolvedRelease.Version)
	require.Equal(t, upstreamCommit, job.ResolvedRelease.Commit)
	require.True(t, strings.HasPrefix(job.Prepared.Branch, "dockhand/bump/fixture-"))
	require.Empty(t, result.Status.Jobs[0].Attempts)
	_, tree, err = repo.Branch(t.Context(), job.Prepared.Branch)
	require.NoError(t, err)
	_, data, err := repo.File(t.Context(), tree, "devel/fixture/Portfile")
	require.NoError(t, err)
	require.Contains(t, string(data), "version 2.0")
	require.Contains(t, string(data), "revision 0")
	require.Contains(t, string(data), fmt.Sprintf("%x", sha256.Sum256([]byte(body))))
	current, _, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, commit, current)
	require.NoFileExists(t, filepath.Join(repo.CommonDir, "index"))
	require.Equal(t, int64(2), downloads.Load())
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"wait", "--job", string(job.ID), "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
	require.Equal(t, int64(2), downloads.Load(), "reattachment must not prepare or download again")
}

func TestPreparationIgnoresLocalBranchAndRefusesFailedFetch(t *testing.T) {
	t.Parallel()
	config, repo, upstream := preparationCLI(t)
	_, tree, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	local, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{upstream}, Message: "unpublished local work", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: upstream}, Desired: git.RefValue{Exists: true, Object: local}}}))
	// Naming a fork origin or using a different upstream remote cannot redirect intake.
	for _, name := range []string{"origin", "upstream"} {
		out, err := exec.CommandContext(t.Context(), "git", "-C", repo.Root, "remote", "add", name, "/missing/fork").CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--subject", "rebuild", "--dry-run", "--json", "-vv"}, Streams{Out: &stdout, Err: &stderr}, config))
	var preview app.Preview
	decodeResult(t, stdout.Bytes(), &preview)
	require.Equal(t, record.ObjectID(upstream), preview.Preparation.Base.Commit)
	require.Equal(t, "https://github.com/macports/macports-ports.git", preview.Repository)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/master", Expected: git.RefValue{Exists: true, Object: upstream}}}))
	for _, mode := range [][]string{{"--dry-run"}, {"--to", "verified"}, {"--to", "branch"}} {
		err := Run(t.Context(), append([]string{"bump-revision", "fixture", "--subject", "rebuild"}, mode...), Streams{Out: &stdout, Err: &stderr}, config)
		require.ErrorContains(t, err, "fetching authoritative MacPorts master")
	}
	current, _, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, local, current)
}
