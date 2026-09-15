package provision

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/host"
)

type native struct {
	config   Config
	progress io.Writer
	mu       sync.Mutex
	runs     map[string]<-chan error
}

func newNative(config Config, progress io.Writer) *native {
	if progress != nil {
		progress = &progressWriter{writer: progress}
	}
	return &native{config: config, progress: progress, runs: map[string]<-chan error{}}
}

func (n *native) command(ctx context.Context, input io.Reader, stream bool, args ...string) ([]byte, error) {
	return n.commandWithGuard(ctx, input, stream, nil, args...)
}

func (n *native) commandWithGuard(ctx context.Context, input io.Reader, stream bool, guard *os.File, args ...string) ([]byte, error) {
	var output io.Writer
	if stream && n.progress != nil {
		output = n.progress
	}
	client := tart.Client{Executable: n.config.Executable, Home: n.config.Home}
	return client.Run(ctx, tart.RunOptions{Input: input, Output: output, Combined: true, ExtraFiles: []*os.File{guard}}, args...)
}

func (n *native) vm() host.Machine {
	return host.Machine{Client: tart.Client{Executable: n.config.Executable, Home: n.config.Home}}
}
