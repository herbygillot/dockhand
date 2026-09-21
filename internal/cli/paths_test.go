package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestGlobalToolAndMacPortsPathsDefaultsAndPrecedence(t *testing.T) {
	working := t.TempDir()
	t.Chdir(working)
	working, err := os.Getwd()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, envTree, envPrefix, envGit    string
		configTree, configPrefix, configGit string
		wantTree, wantPrefix, wantGit       string
		flags                               []string
	}{
		{name: "defaults", wantTree: "."},
		{name: "bare executable", envGit: "custom-git", wantTree: ".", wantGit: "custom-git"},
		{name: "environment", envTree: "env tree", envPrefix: "env prefix", envGit: "env/git", wantTree: "env tree", wantPrefix: "env prefix", wantGit: "env/git"},
		{name: "configured caller", envTree: "env tree", envPrefix: "env prefix", envGit: "env/git", configTree: "configured tree", configPrefix: "configured prefix", configGit: "configured/git", wantTree: "configured tree", wantPrefix: "configured prefix", wantGit: "configured/git"},
		{name: "long flags", envTree: "env tree", envPrefix: "env prefix", envGit: "env/git", configTree: "configured tree", configPrefix: "configured prefix", configGit: "configured/git", flags: []string{"--tree", "flag tree", "--prefix", "flag prefix", "--git", "flag/git"}, wantTree: "flag tree", wantPrefix: "flag prefix", wantGit: "flag/git"},
		{name: "short flags", envTree: "env tree", envPrefix: "env prefix", envGit: "env/git", flags: []string{"-t", "flag tree", "-p", "flag prefix"}, wantTree: "flag tree", wantPrefix: "flag prefix", wantGit: "env/git"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MACPORTS_TREE", tc.envTree)
			t.Setenv("MACPORTS_PREFIX", tc.envPrefix)
			t.Setenv("GIT_BIN", tc.envGit)
			db := filepath.Join(t.TempDir(), "missing", "state.db")
			root, err := NewRoot(app.Config{DBPath: db, Repository: tc.configTree, MacPortsPrefix: tc.configPrefix, GitExecutable: tc.configGit})
			require.NoError(t, err)
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(append([]string{"bump", "--help"}, tc.flags...))
			require.NoError(t, root.ExecuteContext(t.Context()))
			wantPrefix := ""
			if tc.wantPrefix != "" {
				wantPrefix = filepath.Join(working, tc.wantPrefix)
			}
			wantGit := ""
			if tc.wantGit != "" {
				wantGit = tc.wantGit
				if strings.ContainsAny(wantGit, `/\`) {
					wantGit = filepath.Join(working, wantGit)
				}
			}
			require.Equal(t, filepath.Join(working, tc.wantTree), root.PersistentFlags().Lookup("tree").Value.String())
			require.Equal(t, wantPrefix, root.PersistentFlags().Lookup("prefix").Value.String())
			require.Equal(t, wantGit, root.PersistentFlags().Lookup("git").Value.String())
			for _, name := range []string{"tree", "prefix"} {
				require.Contains(t, root.PersistentFlags().Lookup(name).Annotations, cobra.BashCompSubdirsInDir)
			}
			require.Contains(t, out.String(), "-t, --tree")
			require.Contains(t, out.String(), "-p, --prefix")
			require.Contains(t, out.String(), "--git")
			bump, _, err := root.Find([]string{"bump"})
			require.NoError(t, err)
			require.Equal(t, "pr", bump.Flags().Lookup("to").DefValue, "the destination is named, and the PR is the default")
			require.NotNil(t, bump.Flags().Lookup("unverified"))
			for _, gone := range []string{"no-verify", "no-publish", "skip-verify"} {
				require.Nil(t, bump.Flags().Lookup(gone), "%s is gone, not aliased", gone)
			}
			require.Nil(t, bump.Flags().Lookup("publish"), "publication is the default, not a flag")
			require.Nil(t, bump.Flags().Lookup("wait"), "waiting is the default; --detach is the exception")
			require.NoDirExists(t, filepath.Dir(db))
		})
	}
}

func TestGlobalGitReachesRepositoryOperations(t *testing.T) {
	config, id := queuedJob(t)
	config.GitExecutable = ""
	t.Setenv("GIT_BIN", "/missing/environment/git")
	var output bytes.Buffer
	err := Run(t.Context(), []string{"status", "--job", string(id), "--json"}, Streams{Out: &output, Err: &output}, config)
	require.ErrorContains(t, err, "/missing/environment/git")

	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	working := t.TempDir()
	require.NoError(t, os.Symlink(gitPath, filepath.Join(working, "selected-git")))
	t.Chdir(working)
	output.Reset()
	require.NoError(t, Run(t.Context(), []string{"status", "--job", string(id), "--git", "./selected-git", "--json"}, Streams{Out: &output, Err: &output}, config))
	var result workflow.Status
	decodeResult(t, output.Bytes(), &result)
	require.Len(t, result.Jobs, 1)
}

func TestGlobalTartPathDefaultsAndPrecedence(t *testing.T) {
	working := t.TempDir()
	t.Chdir(working)
	t.Setenv("TART_BIN", "environment/tart")
	root, err := NewRoot(app.Config{DBPath: filepath.Join(t.TempDir(), "state.db"), Tart: tart.Config{Executable: "configured/tart"}})
	require.NoError(t, err)
	root.SetArgs([]string{"setup", "--tart", "flag/tart", "--help"})
	require.NoError(t, root.ExecuteContext(t.Context()))
	require.Equal(t, filepath.Join(working, "flag/tart"), root.PersistentFlags().Lookup("tart").Value.String())

	root, err = NewRoot(app.Config{DBPath: filepath.Join(t.TempDir(), "state.db")})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(working, "environment/tart"), root.PersistentFlags().Lookup("tart").Value.String())
}

func TestGlobalTreeSelectsRecordedWorkFromOutsideCheckout(t *testing.T) {
	config, id := queuedJob(t)
	tree := config.Repository
	config.Repository = ""
	t.Chdir(t.TempDir())
	t.Setenv("MACPORTS_PREFIX", "/missing/macports")
	for _, args := range [][]string{
		{"status", "--job", string(id), "--json"},
		{"--tree", tree, "status", "--job", string(id), "--json"},
		{"status", "--job", string(id), "-t", tree, "--json"},
	} {
		t.Setenv("MACPORTS_TREE", tree)
		if len(args) > 4 {
			t.Setenv("MACPORTS_TREE", "/missing/ports")
		}
		var stdout, stderr bytes.Buffer
		require.NoError(t, Run(t.Context(), args, Streams{Out: &stdout, Err: &stderr}, config))
		var result workflow.Status
		decodeResult(t, stdout.Bytes(), &result)
		require.Len(t, result.Jobs, 1)
		require.Equal(t, id, result.Jobs[0].Job.ID)
		require.Equal(t, record.JobQueued, result.Jobs[0].Job.State)
		require.Empty(t, stderr.String())
	}
}

func TestGlobalPrefixReachesPreviewAndDriverConstruction(t *testing.T) {
	config, repo, _ := preparationCLI(t)
	working := t.TempDir()
	prefix := filepath.Join(working, "MacPorts prefix")
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "bin"), 0700))
	require.NoError(t, os.Symlink(config.TclExecutable, filepath.Join(prefix, "bin", "port-tclsh")))
	indexer, err := exec.LookPath("portindex")
	require.NoError(t, err)
	require.NoError(t, os.Symlink(indexer, filepath.Join(prefix, "bin", "portindex")))
	config.Repository, config.TclExecutable = "", ""
	t.Chdir(working)
	t.Setenv("MACPORTS_TREE", repo.Root)
	t.Setenv("MACPORTS_PREFIX", "MacPorts prefix")
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump-revision", "fixture", "--subject", "rebuild", "--dry-run"}, Streams{Out: &stdout, Err: &stderr}, config))
	require.Contains(t, stdout.String(), "+revision 1")
	require.NoDirExists(t, filepath.Dir(config.DBPath))

	stdout.Reset()
	stderr.Reset()
	err = Run(t.Context(), []string{"bump-revision", "fixture", "--subject", "rebuild", "--dry-run", "--prefix", "missing prefix"}, Streams{Out: &stdout, Err: &stderr}, config)
	require.ErrorContains(t, err, filepath.Join(working, "missing prefix", "bin"))
	require.NoDirExists(t, filepath.Dir(config.DBPath))

	stdout.Reset()
	stderr.Reset()
	t.Setenv("MACPORTS_TREE", "/missing/ports")
	t.Setenv("MACPORTS_PREFIX", "/missing/macports")
	require.NoError(t, Run(t.Context(), []string{"-t", repo.Root, "-p", "MacPorts prefix", "bump-revision", "fixture", "--subject", "rebuild", "--to", "branch", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var result ActionResult
	decodeResult(t, stdout.Bytes(), &result)
	require.Len(t, result.Status.Jobs, 1)
	require.Equal(t, record.JobCompleted, result.Status.Jobs[0].Job.State)
	require.Equal(t, record.BranchReady, result.Status.Jobs[0].Job.Spec.Destination)
}

func TestGlobalPathsRejectExplicitEmptyValues(t *testing.T) {
	t.Setenv("MACPORTS_TREE", "")
	t.Setenv("MACPORTS_PREFIX", "")
	config := app.Config{DBPath: filepath.Join(t.TempDir(), "absent", "state.db"), Repository: "/missing/ports"}
	for _, args := range [][]string{{"--tree=", "status"}, {"status", "--prefix="}, {"status", "-t", ""}, {"status", "-p", ""}} {
		var out bytes.Buffer
		require.ErrorContains(t, Run(t.Context(), args, Streams{Out: &out, Err: &out}, config), "directory path must not be empty")
	}
	var out bytes.Buffer
	require.ErrorContains(t, Run(t.Context(), []string{"status", "--git="}, Streams{Out: &out, Err: &out}, config), "executable path must not be empty")
	out.Reset()
	require.ErrorContains(t, Run(t.Context(), []string{"status", "--tart="}, Streams{Out: &out, Err: &out}, config), "executable path must not be empty")
	require.NoDirExists(t, filepath.Dir(config.DBPath))
}

func TestOptionalDependencyToolPathPrecedence(t *testing.T) {
	for _, tool := range []struct{ name, env string }{{"go2port", "GO2PORT_BIN"}, {"cargo2port", "CARGO2PORT_BIN"}} {
		t.Run(tool.name, func(t *testing.T) {
			t.Setenv(tool.env, "environment-helper")
			config := app.Config{DBPath: filepath.Join(t.TempDir(), "absent", "state.db")}
			root, err := NewRoot(config)
			require.NoError(t, err)
			require.Equal(t, "environment-helper", root.PersistentFlags().Lookup(tool.name).Value.String())
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{"setup", "--help", "--" + tool.name, "./tool with spaces"})
			require.NoError(t, root.ExecuteContext(t.Context()))
			expected, err := filepath.Abs("./tool with spaces")
			require.NoError(t, err)
			require.Equal(t, expected, root.PersistentFlags().Lookup(tool.name).Value.String())
			require.NoFileExists(t, config.DBPath)
		})
	}
}
