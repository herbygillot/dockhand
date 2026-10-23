package view

import (
	"fmt"
	"io"

	"github.com/herbygillot/dockhand/internal/engine"
)

// WriteRefreshed says what a refresh of the pull requests found, what
// changed of each and what couldn't be read. GitHub refusing the read
// altogether is its error, worded once for every command that reads them
// (status, clean), as its caller goes on by what was last read.
func WriteRefreshed(out io.Writer, refreshed engine.RefreshResult) error {
	WriteChanges(out, refreshed)
	for _, r := range refreshed.Branches {
		if r.Err != nil {
			fmt.Fprintf(out, "%s: could not read #%d: %v\n", r.Branch.ShortName(), r.Branch.PullRequest.Number, r.Err)
		}
	}
	if words := refreshed.UnreadWords(); words != "" {
		fmt.Fprintln(out, words)
	}
	if refreshed.Refused != nil {
		return refusedRead{refreshed.Refused}
	}
	return nil
}

// WriteChanges says what a refresh of the pull requests found changed of
// each, a line each; its callers say what couldn't be read as they go on
// (the architecture review of 5897a82, R3-39).
func WriteChanges(out io.Writer, refreshed engine.RefreshResult) {
	for _, r := range refreshed.Branches {
		for _, change := range r.Changes {
			fmt.Fprintf(out, "%s: %s\n", r.Branch.ShortName(), change)
		}
	}
}

// refusedRead is GitHub refusing to let the pull requests be read at
// all, as a revoked login: one error for every pull request.
type refusedRead struct{ err error }

func (r refusedRead) Error() string { return "could not read pull requests: " + r.err.Error() }

func (r refusedRead) Unwrap() error { return r.err }
