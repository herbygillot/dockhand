package cli

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
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
	command.Flags().BoolVar(&options.Detach, "detach", false, "Return once the work is accepted and admitted; wait or serve finishes it")
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
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "Print the proposed diff and submit nothing")
	command.Flags().BoolVar(&options.AllSubports, "all-subports", false, "Verify every subport of a shared release locally, not only the newest")
	for _, name := range []string{"to", "unverified", "detach", "trace"} {
		command.MarkFlagsMutuallyExclusive("dry-run", name)
	}
	command.MarkFlagsMutuallyExclusive("trace", "unverified")
}

// referenceFlags collect the tickets a contribution cites: --closes for
// the ones it resolves and --see for the ones worth reading beside it,
// each a Trac ticket number or a URL, each written as its own trailer.
type referenceFlags struct{ closes, see []string }

func (f *referenceFlags) add(command *cobra.Command) {
	command.Flags().StringArrayVar(&f.closes, "closes", nil, "Trac ticket this change closes, as a number or URL; written as a Closes: trailer (repeatable)")
	command.Flags().StringArrayVar(&f.see, "see", nil, "Related Trac ticket, as a number or URL; written as a See: trailer (repeatable)")
}

func (f *referenceFlags) resolve() ([]record.Reference, error) {
	var references []record.Reference
	for _, group := range []struct {
		relation record.ReferenceRelation
		values   []string
	}{{record.ReferenceCloses, f.closes}, {record.ReferenceSee, f.see}} {
		for _, value := range group.values {
			cited, err := ticketURL(value)
			if err != nil {
				return nil, fmt.Errorf("--%s %w", group.relation, err)
			}
			references = append(references, record.Reference{Relation: group.relation, URL: cited})
		}
	}
	return references, nil
}

// ticketURL turns a Trac ticket number, with or without #, into the full
// URL MacPorts asks for in commit messages; a URL passes through as given.
func ticketURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if number := strings.TrimPrefix(value, "#"); number != "" && strings.Trim(number, "0123456789") == "" {
		return macports.TicketURL(number), nil
	}
	parsed, err := url.Parse(value)
	if err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && !strings.ContainsAny(value, " \t\r\n") {
		return value, nil
	}
	return "", fmt.Errorf("takes a Trac ticket number or a URL, not %q", value)
}
