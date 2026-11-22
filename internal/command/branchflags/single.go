package branchflags

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type singleBranchValue struct {
	value   pflag.Value
	command *cobra.Command
	seen    bool
}

func (v *singleBranchValue) Set(value string) error {
	if v.seen {
		return fmt.Errorf("--branch takes one branch; %s", v.advice())
	}
	if err := v.value.Set(value); err != nil {
		return err
	}
	v.seen = true
	return nil
}

func (v *singleBranchValue) String() string { return v.value.String() }
func (v *singleBranchValue) Type() string   { return v.value.Type() }

func (v *singleBranchValue) advice() string {
	if v.command.Name() == "ship" {
		return "pass the ports as operands to ship"
	}
	return "run the command once per branch"
}

// SingleBranch refuses a second value for a command's --branch flag.
func SingleBranch(cmd *cobra.Command) {
	flag := cmd.Flags().Lookup("branch")
	if flag != nil {
		flag.Value = &singleBranchValue{value: flag.Value, command: cmd}
	}
}
