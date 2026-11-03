package compose

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A program dockhand runs is named by --tool, then its variable, then
// [tools], ahead of the search, each checked as it's resolved, a command
// refused where one names a program that isn't there (the person's ask,
// 2026-11-03, and architecture's design).
func TestTheProgramsDockhandRunsCanBeNamed(t *testing.T) {
	t.Parallel()
	home, dir, bin := t.TempDir(), t.TempDir(), t.TempDir()
	program := func(dir, name string) string {
		path := filepath.Join(dir, name)
		testsupport.WriteExecutable(t, path, "#!/bin/sh\n")
		return path
	}
	flagGit, envGit, fileGit := program(dir, "git-flag"), program(dir, "git-env"), program(home, "git-file")
	onPath := program(bin, "gpatch")
	look := func(name string) (string, error) {
		if name == "gpatch" {
			return onPath, nil
		}
		return "", errors.New("not on PATH")
	}
	file := config.File{Tools: config.Tools{Git: fileGit, Go2Port: filepath.Join(home, "go2port-file")}}
	program(home, "go2port-file")
	named := func(options Options) map[string]NamedProgram {
		t.Helper()
		found, err := options.NamedPrograms(file, "/c.toml", look)
		require.NoError(t, err)
		return found
	}
	env := []string{"HOME=" + home, "GIT_BIN=" + envGit, "PATCH_BIN=gpatch"}

	found := named(Options{Tools: map[string]string{"git": flagGit}, Env: env, Dir: dir})
	require.Equal(t, NamedProgram{Program: config.Programs[0], Path: flagGit, Source: "--tool git"}, found["git"], "--tool over the variable and the file")
	require.Equal(t, NamedProgram{Program: config.Programs[3], Path: filepath.Join(home, "go2port-file"), Source: "[tools] go2port"}, found["go2port"])
	require.Equal(t, onPath, found["patch"].Path, "a bare name is found on PATH")
	require.Equal(t, "PATCH_BIN", found["patch"].Source)
	require.NotContains(t, found, "tart", "nothing names it, so it's searched for as before")
	require.Equal(t, envGit, named(Options{Env: env, Dir: dir})["git"].Path, "the variable over the file")
	require.Equal(t, fileGit, named(Options{Env: []string{"HOME=" + home}, Dir: dir})["git"].Path, "the file over PATH")
	require.Equal(t, flagGit, named(Options{Tools: map[string]string{"git": "./git-flag"}, Env: []string{"HOME=" + home}, Dir: dir})["git"].Path, "a relative path read against the command line's directory")

	// Each refusal names where the program was named.
	refused := func(options Options, file config.File) error {
		_, err := options.NamedPrograms(file, "/c.toml", look)
		return err
	}
	require.EqualError(t, refused(Options{Tools: map[string]string{"go2port": "/Users/ada/bin/go2port"}, Env: []string{"HOME=" + home}}, config.File{}), "--tool go2port: /Users/ada/bin/go2port doesn't exist")
	plain := filepath.Join(dir, "plain")
	require.NoError(t, os.WriteFile(plain, nil, 0o644))
	require.EqualError(t, refused(Options{Env: []string{"HOME=" + home, "GO2PORT_BIN=" + plain}}, config.File{}), "GO2PORT_BIN: "+plain+" isn't executable")
	require.EqualError(t, refused(Options{Env: []string{"HOME=" + home}}, config.File{Tools: config.Tools{PortTclsh: home}}), "/c.toml: tools.port_tclsh: "+home+" is a directory")
	require.EqualError(t, refused(Options{Env: []string{"HOME=" + home, "PATCH_BIN=nosuch"}}, config.File{}), "PATCH_BIN: nosuch isn't on PATH")

	// A named port-tclsh's portindex is the one beside it.
	prefix := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.MkdirAll(prefix, 0o755))
	tclsh, portindex := program(prefix, "port-tclsh"), program(prefix, "portindex")
	wrapped := lookNamed(map[string]NamedProgram{"port-tclsh": {Path: tclsh}}, look)
	got, err := wrapped("portindex")
	require.NoError(t, err)
	require.Equal(t, portindex, got)
	got, err = wrapped("gpatch")
	require.NoError(t, err)
	require.Equal(t, onPath, got, "the rest as look finds them")
}

// --tool's value is name=path, for a program dockhand runs.
func TestAToolFlagIsNameAndPath(t *testing.T) {
	t.Parallel()
	name, path, err := ParseTool("git=/opt/homebrew/bin/git")
	require.NoError(t, err)
	require.Equal(t, "git", name)
	require.Equal(t, "/opt/homebrew/bin/git", path)
	_, _, err = ParseTool("git")
	require.EqualError(t, err, "--tool git: name=path, as --tool git=/opt/homebrew/bin/git")
	_, _, err = ParseTool("gitx=/x")
	require.EqualError(t, err, "--tool gitx=/x: gitx isn't a program dockhand runs; the names are git, port-tclsh, tart, go2port, cargo2port, xcodes, gh, patch, and diff")
}
