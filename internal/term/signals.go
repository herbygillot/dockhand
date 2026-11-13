package term

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// StopSignals are the signals that stop a command, each cancelling its
// context so it unwinds: an interrupt, a terminate, and a hangup, which a
// closed terminal window or a dropped SSH session sends. Under Go's default
// a hangup ended the process on the spot, leaving a Tart check's VM
// running and its clone on disk (#240).
var StopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// UntilStopped is parent until one of StopSignals arrives, as
// signal.NotifyContext makes it, and the function to call once the command
// is done. A stop signal the process started with ignored stays ignored:
// nohup starts dockhand with hangups ignored so that it outlives its
// terminal, and a non-interactive shell starts a background job with
// interrupts ignored, where asking to be told of one would heed it. Once stopped, writes to standard output or error no longer end
// the process: the terminal or pipe they went to may be gone with the
// hangup, and the unwind is worth more than its words, so a write there
// fails as any write does, with its error, which teardown doesn't wait on,
// where Go's default for a broken pipe on those two is to end the process
// as SIGPIPE does. Until then a pipe closed by its reader, as by head,
// still ends dockhand quietly.
func UntilStopped(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 1)
	var heeded []os.Signal
	for _, stop := range StopSignals {
		if !signal.Ignored(stop) {
			heeded = append(heeded, stop)
		}
	}
	signal.Notify(signals, heeded...)
	go func() {
		select {
		case <-signals:
			signal.Ignore(syscall.SIGPIPE)
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(signals)
		cancel()
	}
}
