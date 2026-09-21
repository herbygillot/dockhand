package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type dbPathValue struct{ target *string }

func (v dbPathValue) String() string { return *v.target }
func (v dbPathValue) Type() string   { return "path" }
func (v dbPathValue) Set(value string) error {
	if value == ":memory:" || strings.HasPrefix(value, "file:") {
		return errors.New("database must be a filesystem path")
	}
	if value == "" {
		return errors.New("database path must not be empty")
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return err
	}
	*v.target = path
	return nil
}

type directoryPathValue struct{ target *string }

func (v directoryPathValue) String() string { return *v.target }
func (v directoryPathValue) Type() string   { return "path" }
func (v directoryPathValue) Set(value string) error {
	if value == "" {
		return errors.New("directory path must not be empty")
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return err
	}
	*v.target = path
	return nil
}

type executablePathValue struct{ target *string }

func (v executablePathValue) String() string { return *v.target }
func (v executablePathValue) Type() string   { return "path" }
func (v executablePathValue) Set(value string) error {
	if value == "" {
		return errors.New("executable path must not be empty")
	}
	if !filepath.IsAbs(value) && !strings.ContainsAny(value, `/\`) {
		*v.target = value
		return nil
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return err
	}
	*v.target = path
	return nil
}

func verificationFlags(command *cobra.Command, options *Options) {
	command.Flags().BoolVar(&options.Detach, "detach", false, "Return once the work is accepted and admitted; wait or start finishes it")
	command.Flags().BoolVar(&options.Trace, "trace", false, "Follow build logs on stderr through completion; implies --debug")
	command.MarkFlagsMutuallyExclusive("detach", "trace")
}

// The places a preparation or correction can stop.
const (
	toBranch   = "branch"
	toVerified = "verified"
	toPR       = "pr"
)

// destinationFlags say where a preparation or correction stops, as one
// destination and one modifier: --to names the stop, and --unverified opens
// or updates the PR without a build.
type destinationFlags struct {
	to         string
	unverified bool
}

func (d *destinationFlags) add(command *cobra.Command, prVerb string) {
	command.Flags().StringVar(&d.to, "to", toPR, "Where to stop: branch prepares only; verified builds and stops before the PR; pr builds and "+prVerb+" the PR")
	command.Flags().BoolVar(&d.unverified, "unverified", false, "With --to pr, "+prVerb+" the PR without a build; the PR body discloses it")
}

// resolve maps the flags onto the destination: whether the PR is opened or
// updated, and whether the build is skipped.
func (d *destinationFlags) resolve() (to string, publish, skipVerify bool, err error) {
	to, unverified := d.to, d.unverified
	switch to {
	case toBranch, toVerified, toPR:
	default:
		return "", false, false, fmt.Errorf("--to must be branch, verified, or pr")
	}
	if unverified && to != toPR {
		return "", false, false, fmt.Errorf("--unverified opens the PR without a build, so it needs --to pr; --to branch already builds nothing")
	}
	return to, to == toPR, to == toBranch || unverified, nil
}

func changeFlags(command *cobra.Command, options *Options, destination *destinationFlags) {
	verificationFlags(command, options)
	destination.add(command, "opens or updates")
	command.Flags().BoolVar(&options.Diff, "diff", false, "Preview source changes without submitting work")
	command.Flags().BoolVar(&options.AllSubports, "all-subports", false, "Verify every subport of a shared release locally, not only the newest")
	for _, name := range []string{"to", "unverified", "detach", "trace"} {
		command.MarkFlagsMutuallyExclusive("diff", name)
	}
	command.MarkFlagsMutuallyExclusive("trace", "unverified")
}
