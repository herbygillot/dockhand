package tart

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/tool"
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
func RunOnBase(ctx context.Context, tools *tool.Finder, baseVM string, argv []string) (string, error) {
	name := ProbePrefix + stamp()
	if out, err := CLI(ctx, tools, nil, "clone", baseVM, name); err != nil {
		return "", fmt.Errorf("cloning %s: %s", baseVM, strings.TrimSpace(out))
	}
	// Stop before delete, always: tart refuses to remove a running VM,
	// with an error that misleads ("does not exist").
	defer func() {
		cctx := context.WithoutCancel(ctx)
		_, _ = CLI(cctx, tools, nil, "stop", name)
		_, _ = CLI(cctx, tools, nil, "delete", name)
	}()
	unlockAdmission, err := Admit(ctx, tools, concurrent)
	if err != nil {
		return "", err
	}
	runErr := make(chan error, 1)
	go func() {
		_, err := CLI(context.WithoutCancel(ctx), tools, nil, "run", "--no-graphics", name)
		runErr <- err
	}()
	if err := WaitRunning(ctx, tools, name, runErr); err != nil {
		unlockAdmission()
		return "", err
	}
	unlockAdmission()
	if err := WaitAgent(ctx, tools, name); err != nil {
		return "", err
	}
	return Exec(ctx, tools, name, argv...)
}
