package term

import (
	"encoding/json"
	"io"
	"strings"
)

// JSONVersion is the version of the --json envelope and the results in it.
const JSONVersion = 1

// outputMode is how one command line reports: as text, or as one JSON
// envelope written to stdout when it ends.
type outputMode struct {
	json    bool
	command string
	result  any
	// whole, while a command goes on from one step to the next, is its one
	// result, which each step's result is gathered into (LinkSteps).
	whole Gatherer
	// stdout is standard output as the command line was given it, which
	// the envelope goes to.
	stdout io.Writer
	// verbose is -v's count (SetVerbosity).
	verbose int
}

// A Gatherer is a command's result that the steps it goes on to report
// into, so that the command has one shape wherever it stops: what a step
// didn't reach is left out.
type Gatherer interface {
	Gather(step any)
	Result() any
}

// switchWriter is standard output, silenced while --json holds the
// output for the envelope.
type switchWriter struct {
	w      io.Writer
	silent bool
}

func (s *switchWriter) Write(p []byte) (int, error) {
	if s.silent {
		return len(p), nil
	}
	return s.w.Write(p)
}

// JSON reports whether the command line asked for --json.
func (s Streams) JSON() bool { return s.mode != nil && s.mode.json }

// Emit keeps a command's result for the --json envelope.
func (s Streams) Emit(result any) {
	if !s.JSON() {
		return
	}
	if whole := s.mode.whole; whole != nil {
		whole.Gather(result)
		s.mode.result = whole.Result()
		return
	}
	s.mode.result = result
}

// LinkSteps makes whole the command's one result, with each later step's
// inside it, so a script reads one shape wherever the command stopped:
// ship's and bump's are the batch's, each change's update with its later
// steps inside, and check's the check's. A command already gathering
// keeps its own, as ship keeps its batch through each change's submission.
func (s Streams) LinkSteps(whole Gatherer) {
	if s.JSON() && s.mode.whole == nil {
		s.mode.whole = whole
	}
}

// Reports names the command the line runs, as its envelope says, and
// whether it reports as JSON, which silences standard output until
// Finish writes the envelope. Begin's streams.
func (s Streams) Reports(command string, asJSON bool) {
	s.mode.command, s.mode.json = command, asJSON
	if out, ok := s.Out.(*switchWriter); ok {
		out.silent = asJSON
	}
}

// Command is the command the line runs (Reports), or empty before it's
// known.
func (s Streams) Command() string {
	if s.mode == nil {
		return ""
	}
	return s.mode.command
}

// Finish ends a command line of args: where it reports as JSON, its
// envelope, which carries err and the GitHub requests it sent, is written,
// and an error is returned that carries only err's exit code; otherwise
// err as it is. Begin's streams.
func (s Streams) Finish(args []string, requests int64, err error) error {
	if !s.JSON() {
		return err
	}
	if out, ok := s.Out.(*switchWriter); ok {
		out.silent = false
	}
	command := s.mode.command
	if command == "" {
		command = strings.Join(args, " ")
	}
	value := envelope{Version: JSONVersion, Command: command, Result: s.mode.result, GitHubRequests: requests}
	if err != nil {
		value.ExitCode = ExitCode(err)
		if message := err.Error(); message != "" {
			value.Error = &message
		}
	}
	data, marshalErr := json.MarshalIndent(value, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	if _, writeErr := s.mode.stdout.Write(append(data, '\n')); writeErr != nil {
		return writeErr
	}
	// The envelope carries the error; the exit code is still the outcome's.
	if err != nil {
		return &ExitError{Code: ExitCode(err)}
	}
	return nil
}

// envelope is what --json writes (Design v3 §12).
type envelope struct {
	Version  int     `json:"version"`
	Command  string  `json:"command"`
	ExitCode int     `json:"exit_code"`
	Error    *string `json:"error"`
	Result   any     `json:"result"`
	// GitHubRequests is how many requests the command sent GitHub's API.
	GitHubRequests int64 `json:"github_requests"`
}

// asksForJSON reports whether a command line asks for --json, before its
// flags are parsed, so a line that fails to parse still gets its envelope.
func asksForJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" || arg == "--json=true" {
			return true
		}
	}
	return false
}
