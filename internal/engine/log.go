package engine

import (
	"context"
	"fmt"
	"regexp"

	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verify"
)

// mainLogRE finds the guest-side main.log path MacPorts names in its
// own failure output.
var mainLogRE = regexp.MustCompile(`See (/\S+/main\.log)`)

// ErrorLog digs the actual failure out of the environment. The console
// log ends with "Error: See .../main.log for details" — the error
// itself lives in that file, inside the guest, and the field pattern
// was a human sshing in to grep it. This does the dig: the last lines
// of context before the first :error: line, then the :error: lines
// themselves.
//
// Guest-side extraction is provider-specific by nature — it execs into
// the environment — which is the same standing shell already has.
func (e *Engine) ErrorLog(ctx context.Context, prov verify.Verifier, job verify.Job) error {
	console, err := prov.Log(ctx, job)
	if err != nil {
		return err
	}
	m := mainLogRE.FindStringSubmatch(console)
	if m == nil {
		fmt.Fprintln(e.Err, "the console log names no main.log; showing its tail instead")
		tail := console
		if len(tail) > render.LogTail {
			tail = tail[len(tail)-render.LogTail:]
		}
		fmt.Fprint(e.Out, tail)
		return nil
	}
	ex, ok := prov.(verify.Executor)
	if !ok {
		fmt.Fprintln(e.Err, "this provider's environments cannot be reached from here; showing the console tail instead")
		tail := console
		if len(tail) > render.LogTail {
			tail = tail[len(tail)-render.LogTail:]
		}
		fmt.Fprint(e.Out, tail)
		return nil
	}
	fmt.Fprintf(e.Err, "errors from %s in %s:\n", m[1], job.ID)
	script := `log="$1"
first=$(grep -n -m1 ':error:' "$log" | cut -d: -f1)
if [ -z "$first" ]; then
  echo "no :error: lines in $log"
  exit 0
fi
start=$((first > 25 ? first - 25 : 1))
sed -n "${start},$((first - 1))p" "$log"
grep ':error:' "$log" | head -40`
	out, err := ex.Exec(ctx, job, "/bin/sh", "-c", script, "sh", m[1])
	if err != nil {
		return fmt.Errorf("reading %s from %s: %w", m[1], job.ID, err)
	}
	fmt.Fprint(e.Out, out)
	return nil
}
