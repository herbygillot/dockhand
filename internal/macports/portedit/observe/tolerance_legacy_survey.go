//go:build surveymeasure

package observe

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"strings"
)

// legacyTolerate is the unmodified decision routine at a0298da0, retained
// only in the survey build as the live corpus parity oracle. Its predicates
// retain the normal follower policy because this routine receives no off context.
func legacyTolerate(ctx context.Context, port macports.PortObservation, contents []byte, root string) (macports.PortObservation, bool) {
	if !(port.HostAccess || port.ModeledHostAccess) {
		return port, false
	}
	toolchainBenign := false
	toolchainChecked := false
	groups := map[string][]byte{}
	base := newBaseCode(port.BaseLibrary)
	explained := map[string]bool{}
	for _, declaration := range port.Declarations {
		if declaration.Command != "dockhand.host-access" || len(declaration.Values) == 0 {
			continue
		}
		var where []string
		for _, frame := range declaration.Frames {
			where = append(where, fmt.Sprintf("%s:%d %s", frame.File, frame.Line, sourceLine(frame.Command)))
		}
		switch {
		case buildDefaultReadBenign(root, contents, declaration, groups, base):
			progress.DebugReport(ctx, "Host access explained as a build-only option's default: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		case baseLookupBenign(root, contents, declaration, groups, base):
			progress.DebugReport(ctx, "Host access explained as Base's own lookup for an option no fetch or checksum value reads: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		case sourceDateEpochLookup(declaration, base):
			progress.DebugReport(ctx, "Host access explained as Base's source_date_epoch lookup of git: the snapshot has no .git, so the answer doesn't depend on whether git is found: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		case callbackExplained(declaration, contents):
			progress.DebugReport(ctx, "Host access explained as a PortGroup callback's build-only read: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		case toolchainProbe(declaration, base):
			if !toolchainChecked {
				toolchainBenign, toolchainChecked = toolchainReadsBenign(contents), true
			}
			if !toolchainBenign {
				progress.DebugReport(ctx, "Host access is a compiler probe but the Portfile reads toolchain options outside build positions: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
				return port, true
			}
		case portGroupReadBenign(root, declaration.Frames, groups):
			progress.DebugReport(ctx, "Host access explained as a PortGroup's build-only read: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		default:
			progress.DebugReport(ctx, "Host access not explained: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
			return port, true
		}
		explained[declaration.Values[0]] = true
	}
	if len(explained) == 0 {
		return port, true
	}
	var remaining []string
	for _, problem := range port.Problems {
		if explained[problem] {
			continue
		}
		// A modelled context's host problem no declaration explains, as
		// the master's closed answers to Base's developer-tools questions
		// before the plan, leaves the context inconclusive, never a
		// refusal of the port (#110, #125).
		if strings.HasPrefix(problem, "modeled context ") {
			return port, true
		}
		remaining = append(remaining, problem)
	}
	port.Problems = remaining
	port.HostAccess, port.ModeledHostAccess = false, false
	return port, false
}
