package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

type localBuild struct {
	calls   int
	err     error
	options tart.BuildOptions
}

func (b *localBuild) BuildConfig(_ context.Context, platform record.Platform, options tart.BuildOptions) (record.BuildConfig, error) {
	b.calls++
	b.options = options
	return record.BuildConfig{Provider: "tart", Platform: platform, Tests: options.Tests, NeedsXcode: options.NeedsXcode}, b.err
}

func TestAutomaticProviderSelection(t *testing.T) {
	for _, test := range []struct {
		name, provider string
		localError     error
		xcode          bool
		want           string
		problem        bool
	}{
		{name: "local ready", provider: "auto", want: "tart"},
		{name: "local Xcode ready", provider: "auto", xcode: true, want: "tart"},
		{name: "no image", provider: "auto", localError: tart.ErrImageUnavailable, want: "github"},
		{name: "no Xcode image", provider: "auto", localError: tart.ErrImageUnavailable, xcode: true, want: "github"},
		{name: "no binary", provider: "auto", localError: tart.ErrExecutableUnavailable, want: "github"},
		{name: "explicit github", provider: "github", want: "github"},
		{name: "explicit tart unavailable", provider: "tart", localError: tart.ErrImageUnavailable, problem: true},
		{name: "local inspection failed", provider: "auto", localError: os.ErrPermission, problem: true},
		{name: "another tool missing", provider: "auto", localError: exec.ErrNotFound, problem: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for _, args := range [][]string{{"init", "-q", root}, {"-C", root, "remote", "add", "origin", "https://github.com/contributor/macports-ports.git"}} {
				out, err := exec.Command("git", args...).CombinedOutput()
				require.NoError(t, err, string(out))
			}
			repo, err := git.Open(t.Context(), root, "")
			require.NoError(t, err)
			remoteCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				remoteCalls++
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method: %s", r.Method)
				}
				switch r.URL.Path {
				case "/user":
					fmt.Fprint(w, `{"login":"contributor"}`)
				case "/repos/contributor/macports-ports":
					fmt.Fprint(w, `{"full_name":"contributor/macports-ports","default_branch":"master","clone_url":"https://github.com/contributor/macports-ports.git","fork":true,"parent":{"full_name":"macports/macports-ports"}}`)
				case "/repos/macports/macports-ports":
					fmt.Fprint(w, `{"full_name":"macports/macports-ports","default_branch":"master","clone_url":"https://github.com/macports/macports-ports.git"}`)
				case "/repos/contributor/macports-ports/actions/workflows/main.yml":
					fmt.Fprint(w, `{"id":7,"path":".github/workflows/main.yml","state":"active"}`)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client := &github.Client{Config: github.Config{BaseURL: server.URL, Token: "fixture-token"}}
			local := &localBuild{err: test.localError}
			services := &Services{providerName: test.provider, tartVerification: local, githubClient: client, Workflow: &workflow.Engine{Publisher: &publish.Service{Repo: repo, Forge: &forgegithub.Client{Client: client}, LockDirectory: filepath.Join(root, "locks")}}}
			snapshot := macports.Snapshot{Target: record.Target{Name: "fixture"}, Runtime: macports.Runtime{BaseVersion: "2.11.6"}, Ports: map[string]macports.PortInfo{"fixture": {Options: map[string]string{"use_xcode": fmt.Sprint(test.xcode)}}}}
			var messages []string
			ctx := progress.WithReporter(t.Context(), func(u progress.Update) { messages = append(messages, u.Message) })
			result, err := services.buildResolver(record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, "", false, true)(ctx, snapshot)
			if test.problem {
				require.True(t, err != nil || result.Problem != "")
				require.Zero(t, remoteCalls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, result.Build.Provider)
			if test.want == "tart" {
				require.Zero(t, remoteCalls, "local selection must not contact GitHub verification")
				require.Equal(t, record.TestDeclared, result.Build.Tests)
				require.Equal(t, test.xcode, local.options.NeedsXcode)
				require.Equal(t, "2.11.6", local.options.HostMacPortsVersion, "the host Base version reaches the provider so it can warn on skew")
			} else {
				require.Positive(t, remoteCalls)
				require.Equal(t, record.TestWorkflow, result.Build.Tests)
			}
			if test.localError == tart.ErrImageUnavailable {
				require.Contains(t, strings.Join(messages, "\n"), "dockhand setup")
			}
			if test.provider == "github" {
				require.Zero(t, local.calls)
			} else {
				require.Equal(t, 1, local.calls)
			}
		})
	}
}

func (b *localBuild) BuildConfigForImage(ctx context.Context, platform record.Platform, options tart.BuildOptions, image string) (record.BuildConfig, error) {
	value, err := b.BuildConfig(ctx, platform, options)
	value.EnvironmentDigest = image
	return value, err
}

func TestTargetImagesBoundAtIntake(t *testing.T) {
	local := &localBuild{}
	services := &Services{providerName: "tart", tartVerification: local, targetImages: map[string]string{"child": "xcode-image"}}
	evaluation := macports.Snapshot{Target: record.Target{Name: "root"}, Ports: map[string]macports.PortInfo{"root": {Options: map[string]string{"use_xcode": "no"}}}}
	resolve := services.buildResolver(record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, record.TestSkip, false, true)
	result, err := resolve(t.Context(), evaluation)
	require.NoError(t, err)
	require.Equal(t, "xcode-image", result.TargetBuilds["child"].EnvironmentDigest)
	require.Equal(t, result.Build.Platform, result.TargetBuilds["child"].Platform)
	require.Equal(t, record.TestSkip, result.TargetBuilds["child"].Tests)
	services.targetImages = map[string]string{"root": "xcode-image"}
	_, err = resolve(t.Context(), evaluation)
	require.ErrorContains(t, err, "use --image")
	services.targetImages = map[string]string{"child": "missing"}
	local.err = tart.ErrImageUnavailable
	_, err = resolve(t.Context(), evaluation)
	require.Error(t, err, "explicit image choices cannot become unspecified evidence requirements")
}
