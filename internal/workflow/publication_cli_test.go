package workflow_test

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
	"github.com/herbygillot/dockhand/internal/cli"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishCLIAdoptsManualBranchOnlyAfterDryRun(t *testing.T) {
	t.Parallel()
	f, hosting := manualPublicationFixture(t)
	remoteURL := "https://github.com/author/ports.git"
	command := exec.CommandContext(t.Context(), "git", "remote", "set-url", "origin", remoteURL)
	command.Dir = f.repo.Root
	out, err := command.CombinedOutput()
	require.NoError(t, err, "%s", out)
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	wrapper := filepath.Join(t.TempDir(), "git-fixture")
	script := "#!/bin/bash\nargs=()\nfor arg in \"$@\"; do\nif [ \"$arg\" = " + quote(remoteURL) + " ]; then arg=" + quote(hosting.remote) + "; fi\nargs+=(\"$arg\")\ndone\nexec " + quote(gitPath) + " \"${args[@]}\"\n"
	testsupport.WriteExecutable(t, wrapper, script)
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
				var input map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
				pr = map[string]any{"number": 1, "html_url": "https://github.com/author/ports/pull/1", "state": "open", "title": input["title"], "body": input["body"], "head": map[string]any{"ref": "candidate", "sha": string(f.source.Commit), "repo": map[string]any{"full_name": "author/ports"}}, "base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "author/ports"}}}
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
	// The dry run needs the authenticated login to prove the push repository is owned by the user.
	config := app.Config{DBPath: f.store.Path(), Repository: f.repo.Root, GitExecutable: wrapper, GitHub: github.Config{BaseURL: server.URL, Token: "fixture"}}
	var output, diagnostics bytes.Buffer
	err = cli.Run(t.Context(), []string{"publish", "--adopt", "candidate", "--dry-run", "--json"}, cli.Streams{Out: &output, Err: &diagnostics}, config)
	require.NoError(t, err, "%s", diagnostics.String())
	var plan record.JobSpec
	decodeCLIResult(t, output.Bytes(), &plan)
	require.Equal(t, f.source.Commit, plan.Publication.Desired.Head)
	require.Zero(t, writes)
	requireUntrackedPublication(t, f)
	before, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Len(t, before.Jobs, 1, "dry run must not accept a publication job")
	head, err := f.repo.RemoteHead(t.Context(), hosting.remote, "candidate")
	require.NoError(t, err)
	require.False(t, head.Exists)
	config.GitHub.Token = ""
	output.Reset()
	diagnostics.Reset()
	err = cli.Run(t.Context(), []string{"publish", "--adopt", "candidate", "--detach", "--json"}, cli.Streams{Out: &output, Err: &diagnostics}, config)
	require.ErrorIs(t, err, github.ErrAuthentication)
	afterRejected, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Len(t, afterRejected.Jobs, 1, "failed authentication must not accept a publication job")
	head, err = f.repo.RemoteHead(t.Context(), hosting.remote, "candidate")
	require.NoError(t, err)
	require.False(t, head.Exists)
	config.GitHub.Token = "fixture"
	output.Reset()
	diagnostics.Reset()
	err = cli.Run(t.Context(), []string{"publish", "--adopt", "candidate", "--json"}, cli.Streams{Out: &output, Err: &diagnostics}, config)
	require.NoError(t, err, "%s", diagnostics.String())
	var result cli.ActionResult
	decodeCLIResult(t, output.Bytes(), &result)
	require.Len(t, result.Status.Jobs, 1)
	require.Equal(t, record.JobCompleted, result.Status.Jobs[0].Job.State)
	require.Equal(t, "https://github.com/author/ports/pull/1", result.Status.PullRequests[0].Ref.URL)
	mu.Lock()
	require.Equal(t, 1, writes)
	mu.Unlock()
	require.Empty(t, result.Status.Jobs[0].Attempts)
}

func decodeCLIResult(t *testing.T, raw []byte, v any) {
	t.Helper()
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope), "%s", raw)
	require.NoError(t, json.Unmarshal(envelope.Result, v))
}
