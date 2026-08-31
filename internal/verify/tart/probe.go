package tart

import (
	"context"
	"fmt"
	"strings"
)

// ProbePrefix names the short-lived guests RunOnBase creates. Distinct
// from WorkerPrefix on purpose: a probe holds no verdict and owes no
// note, so anything under this prefix is always safe to delete.
const ProbePrefix = "dockhand-probe-"

// RunOnBase boots a throwaway clone of a base, runs one command in it
// through the guest agent, and tears the clone down whatever happens.
// This is the cheap question the full verification pipeline is too
// heavy for — "does this OS have the thing?" — a few dozen seconds of
// boot against ten minutes of build. The command's combined output is
// returned; its own failure is returned as an error alongside whatever
// output there was.
func RunOnBase(ctx context.Context, baseVM string, argv []string) (string, error) {
	name := ProbePrefix + stamp()
	if out, err := CLI(ctx, nil, "clone", baseVM, name); err != nil {
		return "", fmt.Errorf("cloning %s: %s", baseVM, strings.TrimSpace(out))
	}
	// Stop before delete, always: tart refuses to remove a running VM,
	// with an error that misleads ("does not exist").
	defer func() {
		cctx := context.WithoutCancel(ctx)
		_, _ = CLI(cctx, nil, "stop", name)
		_, _ = CLI(cctx, nil, "delete", name)
	}()
	//nolint:errcheck // the guest is detached by design
	go CLI(context.WithoutCancel(ctx), nil, "run", "--no-graphics", name)
	if err := WaitAgent(ctx, name); err != nil {
		return "", err
	}
	return Exec(ctx, name, argv...)
}
