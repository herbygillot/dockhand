package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevisionBumpPublicationCLIWaitAndResume(t *testing.T) {
	for _, wait := range []bool{false, true} {
		t.Run(fmt.Sprint(wait), func(t *testing.T) {
			config, repo, source := preparationCLI(t)
			configureReuseImage(t, &config)
			var stdout, stderr bytes.Buffer
			require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--no-verify", "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
			var prior ActionResult
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &prior))
			seedCLIVerification(t, config, prior.Status.Jobs[0].Job.Prepared.Branch)
			config.Tart.Image = ""
			config.VerificationProvider = "tart"
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
			require.NoError(t, os.WriteFile(config.GitExecutable, []byte(script), 0700))
			var mu sync.Mutex
			writes := 0
			var pr map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch r.URL.Path {
				case "/user":
					assert.Equal(t, "Bearer fixture", r.Header.Get("Authorization"))
					fmt.Fprint(w, `{"login":"author"}`)
				case "/repos/author/ports":
					fmt.Fprint(w, `{"full_name":"author/ports","default_branch":"main","clone_url":"https://github.com/author/ports.git","fork":false}`)
				case "/repos/author/ports/pulls":
					if r.Method == "POST" {
						writes++
						var input struct{ Head, Title, Body string }
						assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
						head := strings.TrimPrefix(input.Head, "author:")
						commit, _, err := repo.Branch(r.Context(), head)
						assert.NoError(t, err)
						pr = map[string]any{"number": 1, "html_url": "https://github.com/author/ports/pull/1", "state": "open", "title": input.Title, "body": input.Body, "head": map[string]any{"ref": head, "sha": commit, "repo": map[string]any{"full_name": "author/ports"}}, "base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "author/ports"}}}
						w.WriteHeader(http.StatusCreated)
						json.NewEncoder(w).Encode(pr)
					} else if pr == nil {
						fmt.Fprint(w, "[]")
					} else {
						json.NewEncoder(w).Encode([]any{pr})
					}
				default:
					t.Errorf("unexpected HTTP request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			config.GitHub = github.Config{BaseURL: server.URL, Token: "fixture"}
			stdout.Reset()
			stderr.Reset()
			args := []string{"bump-revision", "fixture", "--publish", "--remote", "contribution", "--base", "main", "--json"}
			if wait {
				args = append(args, "--wait")
			}
			require.NoError(t, Run(t.Context(), args, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
			var result ActionResult
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			id := result.Status.Jobs[0].Job.ID
			if !wait {
				require.Equal(t, record.JobActive, result.Status.Jobs[0].Job.State)
				require.True(t, workflow.Reached(result.Status, workflow.Admission))
				require.False(t, workflow.Reached(result.Status, workflow.Completion))
				require.Empty(t, result.Status.Jobs[0].Publications)
				mu.Lock()
				assert.Zero(t, writes)
				mu.Unlock()
				stdout.Reset()
				stderr.Reset()
				require.NoError(t, Run(t.Context(), []string{"wait", string(id), "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
				require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
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
			require.Contains(t, stderr.String(), "publication confirmed")
			mu.Lock()
			assert.Equal(t, 1, writes)
			mu.Unlock()
			all, err := app.Status(t.Context(), config)
			require.NoError(t, err)
			require.Len(t, all.Jobs, 3, "one preparation fixture, one evidence fixture, one combined job")
			stdout.Reset()
			stderr.Reset()
			require.NoError(t, Run(t.Context(), []string{"publish", "--branch", entry.Job.Prepared.Branch, "--remote", "contribution", "--base", "main", "--dry-run"}, Streams{Out: &stdout, Err: &stderr}, config))
			require.Contains(t, stdout.String(), body)
			mu.Lock()
			assert.Equal(t, 1, writes)
			mu.Unlock()
		})
	}
}
