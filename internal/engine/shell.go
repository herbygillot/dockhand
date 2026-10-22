package engine

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

// ShellRequest is a command in a clone a check kept (model.Plan.Keep): the
// check's, on Release where it kept several, in Port's work directory,
// the first port it changed where Port is empty. Command is what to run, a
// shell where it's empty, with Input and Output its streams, on a terminal
// where Terminal.
type ShellRequest struct {
	Run      model.Run
	Release  string
	Port     string
	Command  []string
	Input    io.Reader
	Output   io.Writer
	Terminal bool
}

// Shell runs a command in a clone a check kept, and returns its exit
// status: the clone is started for it and stopped after, and stays kept.
func (e *Engine) Shell(ctx context.Context, request ShellRequest) (int, error) {
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return -1, err
	}
	defer session.End(context.WithoutCancel(ctx))
	leftovers, err := e.PlanLeftovers(ctx, session)
	if err != nil {
		return -1, err
	}
	var kept []Leftover
	for _, leftover := range leftovers {
		if leftover.KeptAt.IsZero() || leftover.Run == nil || leftover.Run.ID != request.Run.ID || leftover.Execution == nil {
			continue
		}
		if request.Release != "" {
			release, err := macos.ParseRelease(request.Release)
			if err != nil {
				return -1, err
			}
			if leftover.Execution.Environment.Platform.Version != strconv.Itoa(release.Darwin) {
				continue
			}
		}
		kept = append(kept, leftover)
	}
	name := request.Run.Name()
	switch {
	case len(kept) == 0 && request.Release != "":
		return -1, fmt.Errorf("%s kept no clone on %s", name, request.Release)
	case len(kept) == 0:
		return -1, fmt.Errorf("%s kept no clone; dockhand check --keep keeps each environment's", name)
	case len(kept) > 1:
		var releases []string
		for _, leftover := range kept {
			darwin, _ := strconv.Atoi(leftover.Execution.Environment.Platform.Version)
			if release, err := macos.ReleaseForDarwin(darwin); err == nil {
				releases = append(releases, "--on "+release.Slug)
			}
		}
		return -1, fmt.Errorf("%s kept a clone on each of several releases; choose one with %s", name, strings.Join(releases, " or "))
	}
	clone := kept[0]
	provider, ok := e.Providers[clone.Provider].(buildenv.ShellProvider)
	if !ok {
		return -1, fmt.Errorf("%s can't be started for a shell", clone.What)
	}
	plan, err := e.Plan(ctx, request.Run.Plan)
	if err != nil {
		return -1, err
	}
	portfile := ""
	for _, target := range plan.Targets {
		named := request.Port == target.Target.Name || (target.Target.Subport != "" && request.Port == target.Target.Subport)
		if named || (request.Port == "" && target.Role == model.Changed) {
			portfile = target.Target.Portfile
			break
		}
	}
	if portfile == "" {
		return -1, fmt.Errorf("%s didn't build %s", name, request.Port)
	}
	return provider.Shell(ctx, buildenv.ShellJob{Ref: clone.Ref, Portfile: portfile, Command: request.Command, Input: request.Input, Output: request.Output, Terminal: request.Terminal})
}
