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

func verificationFlags(command *cobra.Command, options *Options) {
	command.Flags().BoolVar(&options.Wait, "wait", false, "Stay until the requested work completes")
	command.Flags().BoolVar(&options.Trace, "trace", false, "Follow build logs and wait for completion")
}

func changeFlags(command *cobra.Command, options *Options) {
	verificationFlags(command, options)
	command.Flags().BoolVarP(&options.NoVerify, "no-verify", "N", false, "Explicitly skip verification")
	command.Flags().BoolVarP(&options.Publish, "publish", "P", false, "Request publication after preparing the change")
	command.Flags().BoolVar(&options.Diff, "diff", false, "Preview source changes without submitting work")
	command.MarkFlagsMutuallyExclusive("diff", "publish")
	command.MarkFlagsMutuallyExclusive("diff", "wait")
	command.MarkFlagsMutuallyExclusive("diff", "trace")
	command.MarkFlagsMutuallyExclusive("trace", "no-verify")
}
