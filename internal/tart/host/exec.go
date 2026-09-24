package host

import (
	"context"
	"os"

	"github.com/herbygillot/dockhand/internal/tart"
)

// ExecScript closes guest-agent descriptors before executing the requested argv.
const ExecScript = `set -eu
limit=$(ulimit -S -n)
ulimit -S -n "$(ulimit -H -n)"
for fd in /dev/fd/*; do
    fd=${fd##*/}
    if [ "$fd" -gt 2 ]; then eval "exec $fd>&-"; fi
done
ulimit -S -n "$limit"
exec "$@"
`

// Exec invokes an argv in the guest without interpolating arguments into a shell.
func (n Machine) Exec(ctx context.Context, vm string, options tart.RunOptions, args ...string) ([]byte, error) {
	command := []string{"exec"}
	if options.Input != nil {
		command = append(command, "-i")
	}
	command = append(command, vm, "/bin/sh", "-c", ExecScript, "dockhand")
	command = append(command, args...)
	options.ExtraFiles = append(append([]*os.File{}, options.ExtraFiles...), n.Guard)
	return n.Client.Run(ctx, options, command...)
}
