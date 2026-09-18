package cli

import (
	"errors"
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
	command.Flags().BoolVar(&options.Trace, "trace", false, "Follow build logs on stderr through completion")
	command.MarkFlagsMutuallyExclusive("detach", "trace")
}

func changeFlags(command *cobra.Command, options *Options) {
	verificationFlags(command, options)
	command.Flags().BoolVarP(&options.SkipVerify, "skip-verify", "V", false, "Publish the prepared branch without a local build; the PR discloses it")
	command.Flags().BoolVarP(&options.NoPublish, "no-publish", "P", false, "Do not open or update a PR; with --skip-verify, stop at the prepared branch")
	command.Flags().BoolVar(&options.Diff, "diff", false, "Preview source changes without submitting work")
	command.Flags().BoolVar(&options.AllSubports, "all-subports", false, "Verify every subport of a shared release locally, not only the newest")
	command.MarkFlagsMutuallyExclusive("diff", "no-publish")
	command.MarkFlagsMutuallyExclusive("diff", "detach")
	command.MarkFlagsMutuallyExclusive("diff", "trace")
	command.MarkFlagsMutuallyExclusive("trace", "skip-verify")
}
