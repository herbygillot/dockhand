package compose

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// GH_TOKEN's token overrides GITHUB_TOKEN's, and either dockhand's login;
// the lock is beside the configuration, and gh is found on the command
// line's PATH, not the process's.
func TestSigninIsTheCommandLinesOwn(t *testing.T) {
	t.Parallel()
	home, bin := t.TempDir(), t.TempDir()
	gh := filepath.Join(bin, "gh")
	testsupport.WriteExecutable(t, gh, "#!/bin/sh\n")
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
	options, _, _, err := Settings(t.Context(), Options{Database: ".dockhand/dockhand.db", Tree: "ports", Dir: dir, Env: []string{"HOME=" + home}}, Dependencies{})
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
		options, _, _, err := Settings(t.Context(), Options{Dir: dir, Env: []string{"HOME=" + home, "DOCKHAND_CONFIG=" + path}}, Dependencies{})
		return options, err
	}
	options, err := settings(`"open", "build-file-changed"`)
	require.NoError(t, err)
	require.Equal(t, []string{"open", "build-file-changed"}, options.AllowHolds)
	_, err = settings(`"licence-changed"`)
	require.ErrorContains(t, err, `holds.allow: "licence-changed" is not a hold dockhand raises or lets go; the rules are`)
}

// A port's own holds reach the engine as it composes them, its allow list
// checked as the global one is (Then's item 2).
func TestAPortsOwnHoldsReachTheEngine(t *testing.T) {
	t.Parallel()
	home, dir := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, "config.toml")
	settings := func(holds string) (engine.Options, error) {
		require.NoError(t, os.WriteFile(path, []byte(holds), 0o644))
		options, _, _, err := Settings(t.Context(), Options{Dir: dir, Env: []string{"HOME=" + home, "DOCKHAND_CONFIG=" + path}}, Dependencies{})
		return options, err
	}
	options, err := settings("[holds.ports.\"harfbuzz\"]\nmajors = \"allow\"\nmoves = \"follow\"\nafter = [\"harfbuzz-devel\"]\nallow = [\"open\"]\n")
	require.NoError(t, err)
	require.Equal(t, map[string]engine.PortHolds{"harfbuzz": {AllowMajors: new(true), Allow: []string{"open"}, FollowMoves: true, After: []string{"harfbuzz-devel"}}}, options.PortHolds)
	_, err = settings("[holds.ports.\"harfbuzz\"]\nallow = [\"licence-changed\"]\n")
	require.ErrorContains(t, err, `holds.ports."harfbuzz".allow: "licence-changed" is not a hold dockhand raises or lets go`)
}

// The programs the engine runs are found on the command line's PATH, not
// the process's, portindex as port-tclsh is; one that isn't there is
// empty. A stand-in Look finds them in its place.
func TestTheEnginesProgramsAreFoundOnTheCommandLinesPath(t *testing.T) {
	t.Parallel()
	home, dir, bin := t.TempDir(), t.TempDir(), t.TempDir()
	for _, name := range []string{"diff", "patch", "go2port", "portindex"} {
		testsupport.WriteExecutable(t, filepath.Join(bin, name), "#!/bin/sh\n")
	}
	options, _, _, err := Settings(t.Context(), Options{Dir: dir, Env: []string{"HOME=" + home, "PATH=/nowhere:" + bin}}, Dependencies{})
	require.NoError(t, err)
	programs := options.Programs
	require.Equal(t, filepath.Join(bin, "diff"), programs.Patches.Diff)
	require.Equal(t, filepath.Join(bin, "patch"), programs.Patches.Patch)
	require.Equal(t, filepath.Join(bin, "go2port"), programs.Vendoring.Go2Port)
	require.Empty(t, programs.Vendoring.Cargo2Port)
	require.Equal(t, filepath.Join(bin, "portindex"), programs.Portindex)
	_, err = programs.Vendoring.Resolve(depblock.Cargo)
	require.ErrorContains(t, err, "missing executable cargo2port; install it, as sudo port install cargo2port")

	var looked []string
	look := func(name string) (string, error) {
		looked = append(looked, name)
		return "", errors.New("not here")
	}
	options, _, _, err = Settings(t.Context(), Options{Dir: dir, Env: []string{"HOME=" + home, "PATH=" + bin}}, Dependencies{Look: look})
	require.NoError(t, err)
	require.Empty(t, options.Programs.Patches.Patch)
	require.Empty(t, options.Tclsh)
	require.Subset(t, looked, []string{"port-tclsh", "portindex", "/opt/local/bin/portindex", "diff", "patch", "go2port", "cargo2port"})
}
