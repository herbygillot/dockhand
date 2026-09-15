package cli

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/spf13/cobra"
)

type buildOptions struct {
	provider   string
	image      string
	capacity   int
	tests      string
	fromSource bool
}

func (o *buildOptions) flags(cmd *cobra.Command, config app.Config) {
	provider := config.VerificationProvider
	if provider == "" {
		provider = "tart"
	}
	cmd.Flags().StringVar(&o.provider, "provider", provider, "Verification provider: tart or github (pushes to your fork)")
	cmd.Flags().StringVar(&o.image, "image", config.Tart.Image, "Prepared local Tart image")
	cmd.Flags().IntVar(&o.capacity, "capacity", config.Tart.Capacity, "Shared Tart capacity (uses the recorded pool limit, initially 2)")
	cmd.Flags().StringVar(&o.tests, "tests", string(record.TestDeclared), "Test policy: declared or skip for Tart; workflow for GitHub")
	cmd.Flags().BoolVar(&o.fromSource, "from-source", false, "Build the target and needed dependencies from source instead of using binary archives")
}

func (o *buildOptions) config(cmd *cobra.Command, config app.Config) (app.Config, error) {
	if o.provider != "tart" && o.provider != "github" {
		return config, fmt.Errorf("provider must be tart or github")
	}
	config.VerificationProvider = o.provider
	if o.provider == "github" {
		if cmd.Flags().Changed("image") || cmd.Flags().Changed("capacity") || cmd.Flags().Changed("from-source") {
			return config, fmt.Errorf("GitHub verification uses the workflow's runner matrix and dependency policy; --image, --capacity, and --from-source are Tart options")
		}
		if !cmd.Flags().Changed("tests") {
			o.tests = string(record.TestWorkflow)
		}
		if o.tests != string(record.TestWorkflow) {
			return config, fmt.Errorf("GitHub verification requires --tests workflow; its workflow may tolerate test failures")
		}
		if cmd.Flags().Lookup("remote") != nil {
			config.VerificationDestination.Remote, _ = cmd.Flags().GetString("remote")
		}
		if cmd.Flags().Lookup("upstream") != nil {
			config.VerificationDestination.Upstream, _ = cmd.Flags().GetString("upstream")
		}
		if cmd.Flags().Lookup("base") != nil {
			config.VerificationDestination.Base, _ = cmd.Flags().GetString("base")
		}
		return config, nil
	}
	if o.capacity < 0 || cmd.Flags().Changed("capacity") && o.capacity == 0 {
		return config, fmt.Errorf("capacity must be positive")
	}
	if o.tests != string(record.TestDeclared) && o.tests != string(record.TestSkip) {
		return config, fmt.Errorf("tests must be declared or skip")
	}
	if cmd.Flags().Changed("image") {
		config.Tart.Image = o.image
	}
	if cmd.Flags().Changed("capacity") {
		config.Tart.Capacity = o.capacity
	}
	return config, nil
}
