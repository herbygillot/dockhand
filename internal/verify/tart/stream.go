package tart

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The capability is the contract, provably: a Streamer that drifts
// fails to build. It is the ninth optional interface, and this provider
// is the one that can answer it — `tart exec` proxies the guest's stdout
// straight through, so a follower in the guest is a stream on this side
// with no polling anywhere.
var _ verify.Streamer = Provider{}

// Stream copies the guest's build log to w as it is written, and stops
// when the build stops. It is `--trace`'s whole implementation on this
// provider, and it JUDGES NOTHING: the verdict arrives through the
// record, written by whoever is the judge by residency (R10).
//
// WHY THE FOLLOWING HAPPENS IN THE GUEST. verify.Streamer's contract is
// that the stream's own EOF is when to stop — a draft that polled Status
// between chunks made every tracing client a second observer of a job
// somebody else was judging. Log's `cat` cannot do that: it answers with
// what was there when it ran. So the guest runs a follower that already
// knows when the build ended, because the runner's own state file is
// sitting beside the log it is following, and this side does nothing but
// copy bytes until the child closes its stdout. dockhand asks the
// provider nothing between the first byte and the last.
//
// It replaces the baseline's stream, which polled Poll and Log on a
// timer and printed the delta: that fetched the whole log on every tick
// (a long build's log is megabytes), and it was a second poller of a job
// the judge was already polling.
//
// The one shell this package runs is here and in memberStatesScript, for
// the same reason: the script is a CONSTANT — the state directory is
// this package's own constant and nothing of the request reaches it — so
// nothing a port or a person named is ever syntax. The argv rule stands
// where argv is what is being passed.
//
// A CANCELLED CONTEXT IS NOT AN ERROR. `--trace` under a --wait that
// expired, or a person's ^C, is the caller's own expiry: the build
// outlives it by design and the verdict still arrives in the record.
// The child dies with the context (exec.CommandContext) and this returns
// nil, so a follower's shutdown never reads as a failed stream.
func (p Provider) Stream(ctx context.Context, job verify.Job, w io.Writer) error {
	if err := p.owns(ctx, job); err != nil {
		return err
	}
	bin, err := p.Tools.Find(tool.Tart)
	if err != nil {
		return fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
	}
	// The child's own stdout, not a transcript: this is a stream, and a
	// tool.Run that buffered it into a string would hand the caller the
	// whole log at the end, which is the contract Log already has.
	// REACHABILITY FIRST, the same question Log and Shell ask. A stopped
	// guest cannot be exec'd into, and this road used to answer that with
	// exit 0 and an empty stream — which a person reads as "the build is
	// fine" or "there is nothing to show" rather than "I could not look".
	// Measured in the field: `log --trace` returned 0 and printed nothing
	// while plain `log` on the same worker reported the guest stopped and
	// named the command to restart it.
	if err := p.awake(ctx, job.ID); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "exec", job.ID, "/bin/sh", "-c", followScript(stateDir))
	cmd.Stdout = w
	// tart's own words, kept: without this a failure to attach had no
	// account at all, and the error below could only say that something
	// went wrong.
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil // the caller stopped following; that is not a failure
	}
	if err != nil {
		// EVERY EXIT THE FOLLOWER HAS IS ZERO — see followScript, whose
		// four endings are `exit 0` to a line — so a non-zero status is
		// TART's and never the script's. This used to convert every
		// ExitError to success on the reasoning that "the follower ends by
		// killing its own tail", which is true of the tail and not of the
		// exec that could never reach it.
		if said := strings.TrimSpace(errOut.String()); said != "" {
			return fmt.Errorf("%w: streaming the build log from %s: %w: %s", verify.ErrNoEnvironment, job.ID, err, said)
		}
		return fmt.Errorf("%w: streaming the build log from %s: %w", verify.ErrNoEnvironment, job.ID, err)
	}
	return nil
}

// followScript is what the guest runs: print the log from its first byte
// and keep printing as it grows, until the runner's state file stops
// saying `running`.
//
// EVERY EXIT IT HAS IS A REAL END OF THE BUILD, which is what makes the
// stream's EOF an honest signal:
//
//	running     followed, until the state file says something else
//	passed      the runner finished; the tail is flushed and closed
//	failed      the same
//	empty       "the runner never started" in this protocol's own
//	            spelling (runnerAt writes `echo running` before it writes
//	            anything else, and the cohort runner renames rather than
//	            truncates for exactly this reason) — so there is nothing
//	            coming, and a follower that waited for it would hang a
//	            person's terminal until they typed ^C
//	missing     the same answer for the same reason: nothing here will
//	            ever write into that log
//
// The extra second after the loop is the tail's, not a poll: the last
// lines the runner wrote before it stamped its verdict have to reach the
// pipe before the tail is killed. The loop's own sleep is the guest's
// wait on its own file and never dockhand asking the provider anything.
//
// It is written against /bin/sh and the tail every macOS carries, and it
// is runnable for real against a scratch directory, which is why the
// directory is a parameter — the same seam runnerAt has, and the same
// claim: at stateDir it is the script Stream ships.
func followScript(dir string) string {
	return `d=` + dir + `
[ -f "$d/log" ] || exit 0
tail -n +1 -f "$d/log" &
t=$!
while [ "$(cat "$d/state" 2>/dev/null)" = running ]; do sleep 1; done
sleep 1
kill "$t" 2>/dev/null
wait "$t" 2>/dev/null
exit 0
`
}
