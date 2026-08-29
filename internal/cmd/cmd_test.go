package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// run executes the command tree with output discarded, returning the
// execution error.
func run(t *testing.T, args ...string) error {
	t.Helper()
	root := Root("test")
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.Execute()
}

// code executes the command tree the way main does, returning the
// process exit code.
func code(t *testing.T, args ...string) int {
	t.Helper()
	root := Root("test")
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return execute(root, args)
}

func TestDoctorRejectsArguments(t *testing.T) {
	require.Error(t, run(t, "doctor", "extra"))
}

func TestClassifyArgumentErrors(t *testing.T) {
	// No targets and no --all.
	require.Error(t, run(t, "classify", "-t", t.TempDir()))
	// --all with arguments; the tempdir is not a ports tree, so the
	// tree error surfaces first.
	err := run(t, "classify", "-a", "-t", t.TempDir(), "foo")
	require.Error(t, err)
	// Named target with no tree available.
	t.Setenv("DOCKHAND_TREE", "")
	err = run(t, "classify", "someport")
	require.ErrorContains(t, err, "ports tree is needed")
}

func TestClassifyBadPrefix(t *testing.T) {
	// A portdir path needs no tree, so resolution succeeds and the
	// stated prefix is what fails: no installation there classifies as
	// environment.
	portdir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "Portfile"), []byte("PortSystem 1.0\n"), 0o644))
	err := run(t, "classify", "-p", t.TempDir(), portdir)
	require.ErrorIs(t, err, prefix.ErrNotInstalled)
	assert.Equal(t, ExitEnvironment, ExitCode(err))
}

func TestClassifyClusteredShortFlags(t *testing.T) {
	// -at clusters --all with -t consuming the next argument, per the
	// POSIX/GNU surface pflag provides underneath cobra. The tempdir is
	// not a ports tree, so reaching ErrNotPortsTree proves both flags
	// parsed.
	err := run(t, "classify", "-at", t.TempDir())
	require.ErrorIs(t, err, tree.ErrNotPortsTree)
}

func TestHelpIsNotAnError(t *testing.T) {
	require.NoError(t, run(t, "--help"))
	require.NoError(t, run(t, "classify", "--help"))
	require.NoError(t, run(t, "doctor", "--help"))
}

func TestVersionForms(t *testing.T) {
	require.NoError(t, run(t, "version"))
	require.NoError(t, run(t, "--version"))
	require.NoError(t, run(t, "-V"))
}

func TestExitCodesThroughExecution(t *testing.T) {
	t.Setenv("DOCKHAND_TREE", "")
	assert.Equal(t, ExitOK, code(t, "version"))
	assert.Equal(t, ExitOK, code(t, "--help"))
	assert.Equal(t, ExitUsage, code(t, "nonsense"))
	assert.Equal(t, ExitUsage, code(t, "classify", "--no-such-flag"))
	assert.Equal(t, ExitUsage, code(t, "doctor", "extra"))
	assert.Equal(t, ExitUsage, code(t, "classify", "-t", t.TempDir()))
	assert.Equal(t, ExitUsage, code(t, "classify", "someport"))
	assert.Equal(t, ExitTree, code(t, "classify", "-at", t.TempDir()))
}

func TestExitCodeMapping(t *testing.T) {
	assert.Equal(t, ExitOK, ExitCode(nil))
	assert.Equal(t, ExitFailure, ExitCode(errors.New("boom")))
	assert.Equal(t, ExitUsage, ExitCode(usagef("bad invocation")))
	assert.Equal(t, ExitUsage, ExitCode(fmt.Errorf("outer: %w", usagef("inner"))))
	assert.Equal(t, ExitTree, ExitCode(fmt.Errorf("outer: %w", tree.ErrPortNotFound)))
	assert.Equal(t, ExitTree, ExitCode(tree.ErrNotPortsTree))
	assert.Equal(t, ExitEnvironment, ExitCode(prefix.ErrNotInstalled))
	assert.Equal(t, ExitEnvironment, ExitCode(fmt.Errorf("sweep: %w", eval.ErrStartup)))
	assert.Equal(t, ExitEnvironment, ExitCode(eval.ErrRootRefused))
}
