package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
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
	return execute(context.Background(), "test", args, io.Discard, io.Discard)
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
	assert.Equal(t, exitcode.NoMacPorts, ExitCode(err))
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
	assert.Equal(t, exitcode.OK, code(t, "version"))
	assert.Equal(t, exitcode.OK, code(t, "--help"))
	assert.Equal(t, exitcode.Usage, code(t, "nonsense"))
	assert.Equal(t, exitcode.Usage, code(t, "classify", "--no-such-flag"))
	assert.Equal(t, exitcode.Usage, code(t, "doctor", "extra"))
	assert.Equal(t, exitcode.Usage, code(t, "classify", "-t", t.TempDir()))
	assert.Equal(t, exitcode.Usage, code(t, "classify", "someport"))
	assert.Equal(t, exitcode.NotPortsTree, code(t, "classify", "-at", t.TempDir()))
}

func TestExitCodeMapping(t *testing.T) {
	assert.Equal(t, exitcode.OK, ExitCode(nil))
	assert.Equal(t, exitcode.Failure, ExitCode(errors.New("boom")))
	assert.Equal(t, exitcode.Usage, ExitCode(usagef("bad invocation")))
	assert.Equal(t, exitcode.Usage, ExitCode(fmt.Errorf("outer: %w", usagef("inner"))))
	assert.Equal(t, exitcode.PortNotFound, ExitCode(fmt.Errorf("outer: %w", tree.ErrPortNotFound)))
	assert.Equal(t, exitcode.NotPortsTree, ExitCode(tree.ErrNotPortsTree))
	assert.Equal(t, exitcode.NoMacPorts, ExitCode(prefix.ErrNotInstalled))
	assert.Equal(t, exitcode.EvalStartup, ExitCode(fmt.Errorf("sweep: %w", eval.ErrStartup)))
	assert.Equal(t, exitcode.RootRefused, ExitCode(eval.ErrRootRefused))
	// A minted branch whose verification could not start: the branch
	// stands, and with no cause to read the deferral is a queued run
	// that `dockhand status` will come back for.
	assert.Equal(t, exitcode.VerifyQueued, ExitCode(&engine.VerifyDeferredError{Branch: "dockhand/jq-1.8.1", Reason: "slots full"}))
	assert.Equal(t, exitcode.VerifyFailed, ExitCode(&engine.VerifyFailedError{Port: "jq"}))
	// The two ways of having no verification are two codes in one
	// family, and they must stay distinguishable: a machine with no
	// tart narrows a bump's contract, where one with no base images
	// fails it. One is a tool to install, the other a base to build.
	assert.Equal(t, exitcode.ToolMissing, ExitCode(fmt.Errorf("wrapped: %w", verify.ErrNoProvider)))
	assert.Equal(t, exitcode.NoVerifyEnv, ExitCode(fmt.Errorf("wrapped: %w", verify.ErrNoEnvironment)))
	require.NotErrorIs(t, verify.ErrNoProvider, verify.ErrNoEnvironment)
	require.NotErrorIs(t, verify.ErrNoEnvironment, verify.ErrNoProvider)
	// An in-flight branch is a refusal with a remedy, and a tree that
	// is not a git checkout is a fact about the tree.
	assert.Equal(t, exitcode.BranchInFlight, ExitCode(&engine.BranchInFlightError{Branch: "dockhand/jq-1.8.1"}))
	assert.Equal(t, exitcode.NotARepo, ExitCode(fmt.Errorf("wrapped: %w", git.ErrNotARepo)))
}

// failWriter fails every write, standing in for the cases a redirected
// run actually meets: a full disk, a closed descriptor.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

// A command whose output is its product must not report success when
// that output did not land. doctor's report is hermetic — it probes
// PATH and nothing else — so this holds anywhere.
func TestDoctorFailsWhenItsReportCannotBeWritten(t *testing.T) {
	assert.NotEqual(t, 0, execute(context.Background(), "test",
		[]string{"doctor"}, failWriter{}, io.Discard))
}

// The same run with a working stream still succeeds, so the check above
// is testing the write and not the command.
func TestDoctorSucceedsWhenItsReportLands(t *testing.T) {
	var buf bytes.Buffer
	assert.Equal(t, 0, execute(context.Background(), "test",
		[]string{"doctor"}, &buf, io.Discard))
	assert.Contains(t, buf.String(), "capabilities:")
}

// bump's flag contradictions are caught before anything is resolved or
// evaluated, so these hold on a machine with no MacPorts at all.
func TestBumpToAndLatestAreExclusive(t *testing.T) {
	assert.Equal(t, exitcode.Usage, code(t, "bump", "--to", "1.0", "--latest", "someport"))
}

func TestBumpRejectsToLatestAsAVersion(t *testing.T) {
	assert.Equal(t, exitcode.Usage, code(t, "bump", "--to", "latest", "someport"))
}

func TestBumpTakesExactlyOnePort(t *testing.T) {
	assert.Equal(t, exitcode.Usage, code(t, "bump"))
	assert.Equal(t, exitcode.Usage, code(t, "bump", "a", "b"))
}

func TestRefreshChecksumsTakesExactlyOnePort(t *testing.T) {
	assert.Equal(t, exitcode.Usage, code(t, "refresh-checksums"))
	assert.Equal(t, exitcode.Usage, code(t, "refresh-checksums", "a", "b"))
}

// "refresh" is the short form a hand types; the canonical name matches
// the intent catalogue.
func TestRefreshAliasResolves(t *testing.T) {
	assert.Equal(t, exitcode.Usage, code(t, "refresh"),
		"the alias reaches the same command, whose arity check fires")
}

// provision's usage errors are caught before anything touches tart, so
// these hold on a machine with nothing installed.
func TestProvisionTartRequiresARelease(t *testing.T) {
	assert.Equal(t, exitcode.Usage, code(t, "provision", "tart"))
	assert.Equal(t, exitcode.Usage, code(t, "provision", "tart", "--macos", "cheetah"),
		"an unknown release is a usage error naming the input, not a guess")
	assert.Equal(t, exitcode.Usage, code(t, "provision", "tart", "extra-arg"))
}

// The reason is the human half of a revbump; without it the command is
// a usage error before anything is resolved.
func TestBumpRevisionRequiresAReason(t *testing.T) {
	assert.Equal(t, exitcode.Usage, code(t, "bump-revision", "someport"))
	assert.Equal(t, exitcode.Usage, code(t, "revbump", "someport"), "the alias shares the check")
	assert.Equal(t, exitcode.Usage, code(t, "bump-revision"))
}

// The tart-less refusal, pinned to the byte. It is the sentence a user
// meets on a machine that cannot verify at all, and no golden reaches
// it, because every golden run wires a provider.
//
// The opening noun moved with the bands: it read "no environment
// available", borrowed from ErrNoEnvironment back when both refusals
// exited alike. They do not any more — this one is 33, a tool to
// install, and the borrowed noun is 34's, a base to provision — so a
// script and the person reading over its shoulder were being told
// different things. The remedy clause is untouched; only the head of
// the sentence moved, and it moved once, here.
func TestTartLessRefusalKeepsItsSentence(t *testing.T) {
	tools := tool.NewFinder(func(string) (string, error) {
		return "", errors.New("executable file not found in $PATH")
	})

	_, err := realVMProvider(tools)(context.Background())

	require.Error(t, err)
	assert.Equal(t,
		"verify: no verify provider: tart is not installed (`port install tart`); --no-verify skips verification",
		err.Error())
	require.ErrorIs(t, err, verify.ErrNoProvider, "the sentinel is what a caller branches on")
	require.NotErrorIs(t, err, verify.ErrNoEnvironment,
		"and it is not the refusal whose remedy is provisioning")
	assert.Equal(t, exitcode.ToolMissing, ExitCode(err), "a tool to install, not a base to provision")
}
