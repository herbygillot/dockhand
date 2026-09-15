package cli

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/spf13/cobra"
)

type buildOptions struct {
	dependents bool
	provider   string
	image      string
	capacity   int
	tests      string
	fromSource bool
}

func (o *buildOptions) flags(cmd *cobra.Command, config app.Config) {
	cmd.Flags().BoolVar(&o.dependents, "dependents", false, "Verify direct dependents in isolated Tart guests")
	provider := config.VerificationProvider
	if provider == "" {
		provider = "tart"
		if cmd.Name() == "bump" || cmd.Name() == "bump-revision" || cmd.Name() == "refresh-checksums" || cmd.Name() == "amend" || cmd.Name() == "rebase" {
			provider = "auto"
		}
	}
	providerHelp := "Verification provider: tart or github (pushes to your fork)"
	if cmd.Name() == "bump" || cmd.Name() == "bump-revision" || cmd.Name() == "refresh-checksums" || cmd.Name() == "amend" || cmd.Name() == "rebase" {
		providerHelp = "Verification provider: auto (prefers prepared Tart), tart, or github (pushes to your fork)"
	}
	cmd.Flags().StringVar(&o.provider, "provider", provider, providerHelp)
	cmd.Flags().StringVar(&o.image, "image", config.Tart.Image, "Prepared local Tart image")
	cmd.Flags().IntVar(&o.capacity, "capacity", config.Tart.Capacity, "Shared Tart capacity (uses the recorded pool limit, initially 2)")
	testDefault := string(record.TestDeclared)
	if provider == "auto" {
		testDefault = ""
	} else if provider == "github" {
		testDefault = string(record.TestWorkflow)
	}
	cmd.Flags().StringVar(&o.tests, "tests", testDefault, "Test policy override: declared or skip for Tart; workflow for GitHub")
	cmd.Flags().BoolVar(&o.fromSource, "from-source", false, "Build the target and needed dependencies from source instead of using binary archives")
}

func (o *buildOptions) config(cmd *cobra.Command, config app.Config) (app.Config, error) {
	if o.provider != "auto" && o.provider != "tart" && o.provider != "github" {
		return config, fmt.Errorf("provider must be auto, tart, or github")
	}
	if o.provider == "auto" && cmd.Name() != "bump" && cmd.Name() != "bump-revision" && cmd.Name() != "refresh-checksums" && cmd.Name() != "amend" && cmd.Name() != "rebase" {
		return config, fmt.Errorf("automatic provider selection is supported for preparation commands; choose --provider tart or github")
	}
	if o.dependents {
		if o.provider == "github" {
			return config, fmt.Errorf("dependent verification requires Tart")
		}
		if o.provider == "auto" {
			o.provider = "tart"
		}
	}
	if o.provider == "auto" {
		if cmd.Flags().Changed("image") || config.Tart.Image != "" || cmd.Flags().Changed("capacity") || cmd.Flags().Changed("from-source") || cmd.Flags().Changed("variant") {
			o.provider = "tart"
		}
		if cmd.Flags().Changed("tests") {
			if o.tests == string(record.TestWorkflow) && o.provider == "auto" {
				o.provider = "github"
			} else if o.provider == "auto" {
				o.provider = "tart"
			}
		}
	}
	config.VerificationProvider = o.provider
	if o.provider == "github" || o.provider == "auto" {
		if cmd.Flags().Changed("image") || cmd.Flags().Changed("capacity") || cmd.Flags().Changed("from-source") {
			return config, fmt.Errorf("GitHub verification uses the workflow's runner matrix and dependency policy; --image, --capacity, and --from-source are Tart options")
		}
		if !cmd.Flags().Changed("tests") {
			if o.provider == "auto" {
				o.tests = ""
			} else {
				o.tests = string(record.TestWorkflow)
			}
		}
		if o.provider == "github" && o.tests != string(record.TestWorkflow) {
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
	if !cmd.Flags().Changed("tests") && o.tests == "" {
		o.tests = string(record.TestDeclared)
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
