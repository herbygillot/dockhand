package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
)

// GH_TOKEN's token overrides GITHUB_TOKEN's, and either dockhand's login;
// the lock is beside the configuration, and gh is found on the command
// line's PATH, not the process's.
func TestSigninIsTheCommandLinesOwn(t *testing.T) {
	t.Parallel()
	home, bin := t.TempDir(), t.TempDir()
	gh := filepath.Join(bin, "gh")
	require.NoError(t, os.WriteFile(gh, []byte("#!/bin/sh\n"), 0o755))
	options := Options{Env: []string{"HOME=" + home, "PATH=/nowhere:" + bin, "GITHUB_TOKEN=second"}}
	signin := options.Signin(nil)
	require.Equal(t, github.Token{Secret: "second", Source: github.SourceGitHubEnvironment}, signin.Token)
	require.Equal(t, filepath.Join(home, ".dockhand", "github-login.lock"), signin.Lock)
	require.Equal(t, gh, signin.CLI)

	options.Env = append(options.Env, "GH_TOKEN= first ")
	require.Equal(t, github.Token{Secret: "first", Source: github.SourceGHEnvironment}, options.Signin(nil).Token, "GH_TOKEN first, trimmed")

	require.Empty(t, Options{Env: []string{"PATH=/nowhere"}}.Signin(nil).CLI)
	require.Empty(t, Options{}.Signin(nil).Lock, "no home, no lock")
}

// A database and tree named by relative paths are the command line's
// directory's, not the process's, which parallel tests share.
func TestRelativePathsAreTheCommandLinesDirectorys(t *testing.T) {
	t.Parallel()
	home, dir := t.TempDir(), t.TempDir()
	options, _, _, err := Settings(t.Context(), Options{Database: ".dockhand/dockhand.db", Tree: "ports", Dir: dir, Env: []string{"HOME=" + home}})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, ".dockhand", "dockhand.db"), options.Database)
	require.Equal(t, filepath.Join(dir, "ports"), options.Tree)
}

// The hold list names rules dockhand raises; one it doesn't is refused,
// with those it does.
func TestTheHoldListNamesRulesDockhandRaises(t *testing.T) {
	t.Parallel()
	home, dir := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, "config.toml")
	settings := func(allow string) (engine.Options, error) {
		require.NoError(t, os.WriteFile(path, []byte("[holds]\nallow = ["+allow+"]\n"), 0o644))
		options, _, _, err := Settings(t.Context(), Options{Dir: dir, Env: []string{"HOME=" + home, "DOCKHAND_CONFIG=" + path}})
		return options, err
	}
	options, err := settings(`"open", "build-file-changed"`)
	require.NoError(t, err)
	require.Equal(t, []string{"open", "build-file-changed"}, options.AllowHolds)
	_, err = settings(`"licence-changed"`)
	require.ErrorContains(t, err, `holds.allow: "licence-changed" is not a hold dockhand raises or lets go; the rules are`)
}
