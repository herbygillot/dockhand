package cli

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/verify"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/spf13/cobra"
)

type buildOptions struct {
	keepFailed   bool
	targetImages []string
	dependents   bool
	provider     string
	image        string
	tests        string
	testTimeout  time.Duration
	fromSource   bool
}

func (o *buildOptions) flags(cmd *cobra.Command, config app.Config) {
	cmd.Flags().BoolVar(&o.keepFailed, "keep-failed", false, "Keep failed local verification VMs for investigation; logs survive normal VM cleanup")
	cmd.Flags().BoolVar(&o.dependents, "dependents", false, "Verify direct dependents in isolated Tart guests")
	cmd.Flags().StringArrayVar(&o.targetImages, "target-image", nil, "Dependent port=image override (repeatable; requires --dependents, same platform)")
	provider := config.VerificationProvider
	if provider == "" {
		provider = "auto"
	}
	cmd.Flags().StringVar(&o.provider, "provider", provider, "Verification provider: auto builds on a prepared Tart image and otherwise on GitHub; tart; or github (pushes to your fork). Any Tart option selects tart")
	cmd.Flags().StringVar(&o.image, "image", config.Tart.Image, "Prepared local Tart image")
	cmd.Flags().StringVar(&o.tests, "tests", string(record.TestDeclared), "Test policy for a Tart build: declared runs the port's tests as advisory, required fails the build on them, skip omits them. GitHub's workflow decides its own")
	cmd.Flags().DurationVar(&o.testTimeout, "test-timeout", config.Tart.TestTimeout, fmt.Sprintf("Stop the port's tests after this long; a timeout counts as a test failure (Tart; default %s)", config.TestTimeout()))
	cmd.Flags().BoolVar(&o.fromSource, "from-source", false, "Build the target and needed dependencies from source instead of using binary archives")
	section(cmd.Flags(), sectionBuild, "keep-failed", "dependents", "target-image", "provider", "image", "tests", "test-timeout", "from-source")
}

func (o *buildOptions) config(cmd *cobra.Command, config app.Config) (app.Config, error) {
	if o.provider != "auto" && o.provider != verify.ProviderTart && o.provider != verify.ProviderGitHub {
		return config, fmt.Errorf("provider must be auto, tart, or github")
	}
	if len(o.targetImages) > 0 {
		if !o.dependents {
			return config, fmt.Errorf("--target-image requires --dependents")
		}
		config.TargetImages = map[string]string{}
		for _, value := range o.targetImages {
			name, image, ok := strings.Cut(value, "=")
			if !ok || name == "" || image == "" || strings.ContainsAny(name, " /\\\t\n\r") || strings.TrimSpace(image) != image {
				return config, fmt.Errorf("--target-image requires port=image")
			}
			if _, exists := config.TargetImages[name]; exists {
				return config, fmt.Errorf("duplicate image override for %s", name)
			}
			config.TargetImages[name] = image
		}
	}
	if o.keepFailed && o.provider == verify.ProviderGitHub {
		return config, fmt.Errorf("--keep-failed requires local verification")
	}
	if o.keepFailed && o.provider == "auto" {
		o.provider = verify.ProviderTart
	}
	if o.dependents {
		if o.provider == verify.ProviderGitHub {
			return config, fmt.Errorf("dependent verification requires Tart")
		}
		if o.provider == "auto" {
			o.provider = verify.ProviderTart
		}
	}
	// A Tart option is a choice of Tart: auto resolves to it rather than
	// refusing the option, and never resolves to GitHub from any option.
	if o.provider == "auto" {
		for _, name := range []string{"image", "from-source", "variant", "test-timeout", "tests", "working-tree", "fresh", "os"} {
			if cmd.Flags().Lookup(name) != nil && cmd.Flags().Changed(name) {
				o.provider = verify.ProviderTart
			}
		}
		if config.Tart.Image != "" {
			o.provider = verify.ProviderTart
		}
	}
	config.VerificationProvider = o.provider
	if o.provider == verify.ProviderGitHub || o.provider == "auto" {
		if cmd.Flags().Changed("image") || cmd.Flags().Changed("from-source") || cmd.Flags().Changed("test-timeout") {
			return config, fmt.Errorf("GitHub verification uses the workflow's runner matrix and dependency policy; --image, --from-source, and --test-timeout are Tart options")
		}
		if cmd.Flags().Changed("tests") {
			return config, fmt.Errorf("GitHub's workflow decides its test policy; --tests is a Tart option")
		}
		// GitHub's policy is the provider's fact, recorded as such; under auto
		// the resolver fills it in for whichever provider it picks.
		o.tests = ""
		if o.provider == verify.ProviderGitHub {
			o.tests = string(record.TestWorkflow)
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
	if !cmd.Flags().Changed("tests") && o.tests == "" {
		o.tests = string(record.TestDeclared)
	}
	if o.tests != string(record.TestDeclared) && o.tests != string(record.TestRequired) && o.tests != string(record.TestSkip) {
		return config, fmt.Errorf("tests must be declared, required, or skip")
	}
	if cmd.Flags().Changed("test-timeout") {
		if o.testTimeout <= 0 {
			return config, fmt.Errorf("test-timeout must be positive")
		}
		config.Tart.TestTimeout = o.testTimeout
	}
	if cmd.Flags().Changed("image") {
		config.Tart.Image = o.image
	}
	return config, nil
}

func verificationSettingsChanged(cmd *cobra.Command) bool {
	for _, name := range []string{"provider", "image", "tests", "test-timeout", "from-source", "dependents", "target-image", "remote", "os"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}
