package cli

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/spf13/cobra"
)

type buildOptions struct {
	image      string
	capacity   int
	tests      string
	fromSource bool
}

func (o *buildOptions) flags(cmd *cobra.Command, config app.Config) {
	cmd.Flags().StringVar(&o.image, "image", config.Tart.Image, "Prepared local Tart image")
	cmd.Flags().IntVar(&o.capacity, "capacity", config.Tart.Capacity, "Shared Tart capacity (uses the recorded pool limit, initially 2)")
	cmd.Flags().StringVar(&o.tests, "tests", string(record.TestDeclared), "Test policy: declared or skip")
	cmd.Flags().BoolVar(&o.fromSource, "from-source", false, "Build the target and needed dependencies from source instead of using binary archives")
}

func (o buildOptions) config(cmd *cobra.Command, config app.Config) (app.Config, error) {
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
