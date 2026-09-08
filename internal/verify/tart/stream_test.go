package tart

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/verify"
)

// THE TWO OPTIONAL INTERFACES THIS PROVIDER ANSWERS THAT NOTHING
// IMPLEMENTED. Both were ported as interfaces and left without a
// producer, and the cost was not theoretical: `--trace` took
// run.Follow's type assertion, missed, and told a person the only
// provider dockhand ships "does not stream a live log" — while the
// ruled surface says of that flag that it is the only way to watch a
// build happen. `status`'s vacancy line had no producer either, so the
// machine's free room was reported unknown on a machine that could
// answer it in one `tart list`.
//
// Asserted at runtime rather than left to the compile-time `var _`
// declarations beside each implementation, so that a build which
// silently lost one names it here.
func TestTheProviderAnswersTheOptionalStreamAndVacancyInterfaces(t *testing.T) {
	var p any = Provider{}
	_, streams := p.(verify.Streamer)
	assert.True(t, streams, "--trace has nothing to watch a build with without this")
	_, reports := p.(verify.VacancyReporter)
	assert.True(t, reports, "status cannot report the machine's free room without this")
}

// guestDir is a scratch state directory shaped like the one the runner
// writes in a guest: the follow script is run for real against it, the
// way runnerAt is, because what is being proven is a property of
// /bin/sh and tail reading these files.
type guestDir struct {
	t   *testing.T
	dir string
}

func newGuestDir(t *testing.T) *guestDir {
	t.Helper()
	return &guestDir{t: t, dir: t.TempDir()}
}

func (g *guestDir) put(name, content string) {
	g.t.Helper()
	require.NoError(g.t, os.WriteFile(filepath.Join(g.dir, name), []byte(content), 0o644))
}

func (g *guestDir) append(name, content string) {
	g.t.Helper()
	f, err := os.OpenFile(filepath.Join(g.dir, name), os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(g.t, err)
	_, werr := f.WriteString(content)
	require.NoError(g.t, werr)
	require.NoError(g.t, f.Close())
}

// follow starts the guest-side follower and returns what it printed once
// it has stopped. The context is the test's deadline and not the
// script's: a follower that did not stop is this test's whole failure
// mode, so it must fail rather than hang the package.
func (g *guestDir) follow(done func()) string {
	g.t.Helper()
	ctx, cancel := context.WithTimeout(g.t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", followScript(g.dir))
	var out bytes.Buffer
	cmd.Stdout = &out
	require.NoError(g.t, cmd.Start())
	if done != nil {
		done()
	}
	require.NoError(g.t, cmd.Wait(), "the follower stopped on its own")
	return out.String()
}

// THE STREAM'S OWN EOF IS WHEN TO STOP, which is verify.Streamer's
// contract and the reason the following happens in the guest: the
// runner's state file is sitting beside the log, so the follower knows
// the build ended without anybody polling the provider. It prints the
// log from its first byte — a person who typed --trace after the build
// started still sees what they missed — and keeps printing until the
// verdict is stamped.
func TestTheFollowScriptStreamsTheLogAndStopsWhenTheBuildDoes(t *testing.T) {
	g := newGuestDir(t)
	g.put("state", "running\n")
	g.put("log", "--->  Fetching jq\n")

	out := g.follow(func() {
		// The build writes on while it is followed, then stamps its
		// verdict. Nothing here tells the follower to stop; the state
		// file does.
		time.Sleep(300 * time.Millisecond)
		g.append("log", "--->  Building jq\n")
		time.Sleep(300 * time.Millisecond)
		g.put("state", "passed\n")
	})

	assert.Contains(t, out, "--->  Fetching jq", "the log is followed from its first byte")
	assert.Contains(t, out, "--->  Building jq", "and while it is still being written")
}

// A build that already ended is not a hang. `--trace` on a settled job
// prints what is there and returns, which is what makes the flag safe to
// pass on a road that may have raced the verdict.
func TestTheFollowScriptReturnsAtOnceOnAFinishedBuild(t *testing.T) {
	g := newGuestDir(t)
	g.put("state", "failed\n")
	g.put("log", "Error: jq did not build\n")

	assert.Contains(t, g.follow(nil), "Error: jq did not build")
}

// AN EMPTY STATE FILE IS THIS PROTOCOL'S SPELLING OF "the runner never
// started", and a follower that waited for a word that is never coming
// would hold a person's terminal until they typed ^C. It is an end of
// the stream like any other, and so is a state directory with no log in
// it at all.
func TestTheFollowScriptDoesNotWaitForARunnerThatNeverStarted(t *testing.T) {
	g := newGuestDir(t)
	g.put("state", "")
	g.put("log", "")
	assert.Empty(t, g.follow(nil))

	bare := newGuestDir(t)
	assert.Empty(t, bare.follow(nil), "no log is nothing to follow, not something to wait for")
}
