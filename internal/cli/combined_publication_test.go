package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevisionBumpPublicationCLIWaitAndResume(t *testing.T) {
	t.Parallel()
	for _, wait := range []bool{false, true} {
		t.Run(fmt.Sprint(wait), func(t *testing.T) {
			config, repo, source := preparationCLI(t)
			configureReuseImage(t, &config)
			var stdout, stderr bytes.Buffer
			require.NoError(t, runFixture(t.Context(), []string{"bump-revision", "fixture", "--subject", "rebuild", "--to", "branch", "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
			var prior ActionResult
			decodeResult(t, stdout.Bytes(), &prior)
			seedCLIVerification(t, config, prior.Status.Jobs[0].Job.Prepared.Branch)
			config.Tart.Image = ""
			config.VerificationProvider = "tart"
			forge := publicationCLI(t, &config, repo, source)
			stdout.Reset()
			stderr.Reset()
			args := []string{"bump-revision", "fixture", "--subject", "rebuild", "--remote", "contribution", "--base", "main", "--json"}
			if !wait {
				args = append(args, "--detach")
			}
			require.NoError(t, runFixture(t.Context(), args, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
			var result ActionResult
			decodeResult(t, stdout.Bytes(), &result)
			id := result.Status.Jobs[0].Job.ID
			if !wait {
				require.Equal(t, record.JobActive, result.Status.Jobs[0].Job.State)
				require.True(t, workflow.Reached(result.Status, workflow.Admission))
				require.False(t, workflow.Reached(result.Status, workflow.Completion))
				require.Empty(t, result.Status.Jobs[0].Publications)
				assert.Zero(t, forge.writes())
				stdout.Reset()
				stderr.Reset()
				require.NoError(t, runFixture(t.Context(), []string{"wait", "--job", string(id), "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
				decodeResult(t, stdout.Bytes(), &result)
			}
			entry := result.Status.Jobs[0]
			require.Equal(t, id, entry.Job.ID)
			require.Equal(t, record.JobCompleted, entry.Job.State)
			require.Equal(t, record.Published, entry.Job.Spec.Destination)
			require.Equal(t, record.ObjectID(source), entry.Job.Spec.Source.Commit)
			require.Equal(t, entry.Job.Prepared.Source.Commit, entry.Publications[0].Spec.Desired.Head)
			require.Equal(t, entry.Job.ResultRevision, result.Status.Changes[0].PublishedRevision)
			require.Equal(t, "https://github.com/author/ports/pull/1", result.Status.PullRequests[0].Ref.URL)
			require.Equal(t, record.AttemptID("original-attempt"), entry.Job.ReusedAttempt)
			body := entry.Publications[0].Spec.Desired.Body
			require.Contains(t, body, "original-attempt")
			require.Contains(t, body, "[x] Squashed")
			require.Contains(t, body, "[ ] Ran the port's tests")
			require.Contains(t, body, "[ ] Completed a full install")
			require.Nil(t, entry.Job.Spec.Build)
			require.Equal(t, record.BuildRequirements{Provider: "tart", Platform: entry.Reused.Spec.Config.Platform, CapabilitiesRequired: true, Tests: record.TestDeclared}, *entry.Job.Spec.BuildRequirements)
			require.Empty(t, entry.Attempts)
			require.Contains(t, stderr.String(), "fixture: PR https://github.com/author/ports/pull/1 (created)")
			require.NotContains(t, stderr.String(), "checking the remote before publishing", "the driver's detail stays at -v")
			assert.Equal(t, 1, forge.writes())
			all, err := app.FilteredStatus(t.Context(), config, workflow.StatusFilter{})
			require.NoError(t, err)
			require.Len(t, all.Jobs, 3, "one preparation fixture, one evidence fixture, one combined job")
			stdout.Reset()
			stderr.Reset()
			require.NoError(t, runFixture(t.Context(), []string{"publish", "--branch", entry.Job.Prepared.Branch, "--remote", "contribution", "--base", "main", "--dry-run"}, Streams{Out: &stdout, Err: &stderr}, config))
			require.Contains(t, stdout.String(), body)
			assert.Equal(t, 1, forge.writes())
		})
	}
}

// fakeGitHub serves the GitHub API calls a publication makes, redirecting the
// fork's push URL to a local bare repository. Only one PR is ever created.
type fakeGitHub struct {
	mu    sync.Mutex
	count int
	pr    map[string]any
}

func (f *fakeGitHub) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

func (f *fakeGitHub) body() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pr == nil {
		return ""
	}
	body, _ := f.pr["body"].(string)
	return body
}

// publicationCLI points config at a fake GitHub and a fork remote named
// "contribution" whose pushes land in a local bare repository.
func publicationCLI(t *testing.T, config *app.Config, repo *git.Repository, source string) *fakeGitHub {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "-q", remote).CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.NoError(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "main", Commit: source}))
	remoteURL := "https://github.com/author/ports.git"
	out, err = exec.CommandContext(t.Context(), "git", "-C", repo.Root, "remote", "add", "contribution", remoteURL).CombinedOutput()
	require.NoError(t, err, "%s", out)
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	config.GitExecutable = filepath.Join(t.TempDir(), "git-fixture")
	script := "#!/bin/bash\nargs=()\nfor arg in \"$@\"; do\nif [ \"$arg\" = " + quote(remoteURL) + " ]; then arg=" + quote(remote) + "; fi\nargs+=(\"$arg\")\ndone\nexec " + quote(gitPath) + " \"${args[@]}\"\n"
	testsupport.WriteExecutable(t, config.GitExecutable, script)
	forge := &fakeGitHub{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forge.mu.Lock()
		defer forge.mu.Unlock()
		switch r.URL.Path {
		case "/user":
			assert.Equal(t, "Bearer fixture", r.Header.Get("Authorization"))
			fmt.Fprint(w, `{"login":"author"}`)
		case "/repos/author/ports":
			fmt.Fprint(w, `{"full_name":"author/ports","default_branch":"main","clone_url":"https://github.com/author/ports.git","fork":false}`)
		case "/repos/author/ports/pulls/1":
			json.NewEncoder(w).Encode(forge.pr)
		case "/repos/author/ports/pulls":
			if r.Method == "POST" {
				forge.count++
				var input struct{ Head, Title, Body string }
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
				head := strings.TrimPrefix(input.Head, "author:")
				commit, _, err := repo.Branch(r.Context(), head)
				assert.NoError(t, err)
				forge.pr = map[string]any{"number": 1, "html_url": "https://github.com/author/ports/pull/1", "state": "open", "title": input.Title, "body": input.Body, "head": map[string]any{"ref": head, "sha": commit, "repo": map[string]any{"full_name": "author/ports"}}, "base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "author/ports"}}}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(forge.pr)
			} else if forge.pr == nil {
				fmt.Fprint(w, "[]")
			} else {
				json.NewEncoder(w).Encode([]any{forge.pr})
			}
		default:
			t.Errorf("unexpected HTTP request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	config.GitHub = github.Config{BaseURL: server.URL, Token: "fixture"}
	t.Cleanup(server.Close)
	return forge
}

func TestRevisionBumpSkipVerifyPublishesWithDisclosure(t *testing.T) {
	t.Parallel()
	config, repo, source := preparationCLI(t)
	forge := publicationCLI(t, &config, repo, source)
	var stdout, stderr bytes.Buffer
	require.NoError(t, runFixture(t.Context(), []string{"bump-revision", "fixture", "--subject", "rebuild", "--remote", "contribution", "--base", "main", "--unverified", "--json", "-v"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var result ActionResult
	decodeResult(t, stdout.Bytes(), &result)
	entry := result.Status.Jobs[0]
	require.Equal(t, record.JobCompleted, entry.Job.State)
	require.Equal(t, record.Published, entry.Job.Spec.Destination)
	require.Equal(t, record.VerificationSkipped, entry.Job.Spec.Verification)
	require.Nil(t, entry.Job.Spec.Build)
	require.Empty(t, entry.Attempts)
	require.Empty(t, entry.Job.ReusedAttempt)
	require.Len(t, entry.Publications, 1)
	require.True(t, entry.Publications[0].Spec.Unverified)
	require.Empty(t, entry.Publications[0].Spec.EvidenceAttempt)
	require.Equal(t, record.PublicationConfirmed, entry.Publications[0].State)
	require.Equal(t, "https://github.com/author/ports/pull/1", result.Status.PullRequests[0].Ref.URL)
	require.Equal(t, 1, forge.writes())
	require.Contains(t, forge.body(), "Not built locally")
	require.Contains(t, forge.body(), "--unverified")
	require.Contains(t, stderr.String(), "verification skipped at the author's request")
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, runFixture(t.Context(), []string{"status"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	require.Contains(t, stdout.String(), "published unverified")
	// A later publish of the same branch without the flag needs evidence.
	stdout.Reset()
	stderr.Reset()
	err := runFixture(t.Context(), []string{"publish", "--branch", entry.Job.Prepared.Branch, "--remote", "contribution", "--base", "main", "--dry-run"}, Streams{Out: &stdout, Err: &stderr}, config)
	require.ErrorContains(t, err, "verify the committed contribution before publishing")
	stdout.Reset()
	require.NoError(t, runFixture(t.Context(), []string{"publish", "--branch", entry.Job.Prepared.Branch, "--remote", "contribution", "--base", "main", "--dry-run", "--unverified"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	require.Contains(t, stdout.String(), "verification: skipped at the author's request")
	require.Contains(t, stdout.String(), "Not built locally")
	require.Equal(t, 1, forge.writes())
}

func TestSkipVerifyWithNoPublishStopsAtTheBranch(t *testing.T) {
	t.Parallel()
	config, _, _ := preparationCLI(t)
	var stdout, stderr bytes.Buffer
	require.NoError(t, runFixture(t.Context(), []string{"bump-revision", "fixture", "--subject", "rebuild", "--to", "branch", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var result ActionResult
	decodeResult(t, stdout.Bytes(), &result)
	entry := result.Status.Jobs[0]
	require.Equal(t, record.JobCompleted, entry.Job.State)
	require.Equal(t, record.BranchReady, entry.Job.Spec.Destination)
	require.Equal(t, record.VerificationSkipped, entry.Job.Spec.Verification)
	require.Empty(t, entry.Publications)
	require.Empty(t, entry.Attempts)
	for _, args := range [][]string{{"bump-revision", "fixture", "--subject", "rebuild", "--dependents", "--unverified"}, {"amend", "--dependents", "--unverified"}, {"bump-revision", "fixture", "--subject", "rebuild", "--trace", "--unverified"}} {
		err := runFixture(t.Context(), args, Streams{Out: &stdout, Err: &stderr}, config)
		require.Error(t, err, "%v", args)
	}
}
