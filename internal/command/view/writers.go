package view

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/ghactions"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// writePushes says where a check pushes: "only checking" never hides a
// write to your fork (Design v3 §9).
func WritePushes(out io.Writer, plan model.Plan) {
	if slices.ContainsFunc(plan.Environments, func(e model.Environment) bool { return e.Provider == buildenv.GitHub }) {
		fmt.Fprintf(out, "Pushes      this revision to a %s branch in your fork for the MacPorts workflow; removes that branch after reading the run's results\n", ghactions.BranchPrefix)
	}
	if plan.Keep {
		fmt.Fprintln(out, "Keeps       each Tart environment's clone, stopped, where its guest finishes, for dockhand shell; clean or the branch's next check removes it")
	}
}

// writePlan shows a plan, with remedy saying how to give an environment
// what a target it can't build needs.
func WritePlan(out io.Writer, plan model.Plan, notes []string, remedy func(model.Unmet) string) {
	var changed, extra []string
	for _, target := range plan.Targets {
		name := string(target.ID)
		switch target.Role {
		case model.Also:
			extra = append(extra, name)
		case model.Prerequisite:
			changed = append(changed, name+" (needed by --only)")
		default:
			if target.Kind == model.RevisionOnly {
				name += " (revision only)"
			}
			changed = append(changed, name)
		}
	}
	if len(changed) > 0 {
		fmt.Fprintf(out, "Changed     %s\n", prose.Few(changed, listShown, "port"))
	}
	if len(extra) > 0 {
		fmt.Fprintf(out, "Also        %s\n", prose.Few(extra, listShown, "port"))
	}
	if len(plan.Omitted) > 0 {
		var omitted []string
		for _, target := range plan.Omitted {
			omitted = append(omitted, string(target.ID))
		}
		fmt.Fprintf(out, "Left out    %s, by --only; submit still needs them checked\n", strings.Join(omitted, ", "))
	}
	var on []string
	for _, environment := range plan.Environments {
		on = append(on, engine.DescribeEnvironment(environment))
	}
	profile := ""
	// The command profile is its provider, said already.
	if plan.Profile != "" && plan.Profile != "command" {
		profile = " · build profile " + plan.Profile
	}
	fmt.Fprintf(out, "Provider    %s%s · tests %s\n", strings.Join(on, "; "), profile, plan.Tests)
	WriteVariants(out, plan)
	// Tests required ask nothing of a port that declares none, which
	// passes under any policy: said, so its ✓ isn't read as tests passed.
	if plan.Tests == model.TestsRequired {
		tested, untested := plan.Untested()
		// Which do declare them is said too, beside those that don't, so
		// "asks nothing" of a stub isn't read as of the port (field
		// testing's py-mlx-vlm, 2026-10-04).
		if len(untested) > 0 && len(tested) > 0 {
			fmt.Fprintf(out, "Tests       %s, which must pass\n", prose.And(tested))
		}
		switch len(untested) {
		case 0:
		case 1:
			fmt.Fprintf(out, "No tests    %s declares no tests; --tests required adds no test requirement for this port\n", untested[0])
		default:
			fmt.Fprintf(out, "No tests    %s declare no tests; --tests required adds no test requirement for these ports\n", strings.Join(untested, ", "))
		}
	}
	for _, note := range notes {
		fmt.Fprintf(out, "            %s\n", note)
	}
	writeOrder(out, plan)
	writeGitSources(out, plan)
	writeExclusions(out, plan)
	// Each environment's remedy follows its last target it can't build.
	for _, planned := range plan.Builds {
		for i, unmet := range planned.Unmet {
			fmt.Fprintf(out, "Not built   %s on %s: %s\n", unmet.Target, engine.DescribeEnvironment(unmet.Environment), engine.UnmetWords(unmet))
			if i == len(planned.Unmet)-1 && remedy != nil {
				if words := remedy(unmet); words != "" {
					fmt.Fprintf(out, "            %s\n", words)
				}
			}
		}
	}
	for _, unresolved := range plan.Unresolved {
		fmt.Fprintf(out, "✗ %s can't be planned: %s\n", unresolved.Target.Name, unresolved.Reason)
	}
}

// writeGitSources says what each Git-fetched target's build must fetch:
// the commit its git.branch names as the check is planned, which its build
// is checked against. A target is said once where its environments expect
// the same, as they do but where one evaluates another git.branch. Those
// --only left out follow, marked so: the check doesn't build them, but an
// earlier check's result of one stands only where it fetched that commit.
func writeGitSources(out io.Writer, plan model.Plan) {
	var lines []string
	add := func(line string) {
		if !slices.Contains(lines, line) {
			lines = append(lines, line)
		}
	}
	for _, planned := range plan.Builds {
		for _, id := range planned.Order {
			if source, ok := planned.Git[id]; ok {
				add(fmt.Sprintf("%s: %s", id, engine.GitSourceWords(source)))
			}
		}
	}
	for _, planned := range plan.Builds {
		for _, target := range plan.Omitted {
			if source, ok := planned.Git[target.ID]; ok {
				add(fmt.Sprintf("%s (left out): %s", target.ID, engine.LeftOutSourceWords(source)))
			}
		}
	}
	for i, line := range lines {
		label := ""
		if i == 0 {
			label = "Git"
		}
		fmt.Fprintf(out, "%-12s%s\n", label, line)
	}
}

// listShown is how many of a long list a line names before it counts the
// rest; --json carries every one (the command-line UX review's §10).
const listShown = 8

// writeOrder shows the order the plan builds in: one line where every
// environment builds in the plan's order, and a line for each where
// their dependencies put them in different orders.
func writeOrder(out io.Writer, plan model.Plan) {
	if len(plan.Targets) < 2 {
		return
	}
	names := func(ids []model.TargetID) string {
		var words []string
		for _, id := range ids {
			words = append(words, string(id))
		}
		return prose.FewJoined(words, listShown, "port", " → ")
	}
	if plan.SameOrder() {
		var order []model.TargetID
		for _, target := range plan.Targets {
			order = append(order, target.ID)
		}
		fmt.Fprintf(out, "Order       %s\n", names(order))
		return
	}
	for i, planned := range plan.Builds {
		label := "Order      "
		if i > 0 {
			label = "           "
		}
		fmt.Fprintf(out, "%s on %s: %s\n", label, engine.DescribeEnvironment(planned.Environment), names(planned.Order))
	}
}

// writeExclusions shows what the plan doesn't build where, and why: once
// for a port every environment excludes for the same reason, and with the
// environments where only some do.
func writeExclusions(out io.Writer, plan model.Plan) {
	for _, x := range plan.GroupedExclusions() {
		if x.Everywhere(plan) {
			fmt.Fprintf(out, "Excluded    %s: %s\n", x.Target, x.Reason)
			continue
		}
		var where []string
		for _, environment := range x.Environments {
			where = append(where, engine.DescribeEnvironment(environment))
		}
		fmt.Fprintf(out, "Excluded    %s on %s: %s\n", x.Target, strings.Join(where, "; "), x.Reason)
	}
}

// writeResults shows each target's result, indented. On one environment
// it is a line each, "jq  ✓", the environment named when where says so.
// On several it is a grid, a row per target and a column per environment
// headed by its release, macOS 26, since the plan named the environments
// in full.
func WriteResults(out io.Writer, indent string, evidence engine.Evidence, where bool) {
	environments := evidence.Plan.Environments
	// The releases a profile chose not to build on are left out, as the
	// pull request leaves them out, and so are those build.default
	// requires that no check planned: a check on 12 and 27 is of those
	// two, and which releases nothing covered is submit's to say.
	shown := evidence.Checked()
	if len(shown) <= 1 {
		width := 0
		for _, target := range evidence.Targets {
			width = max(width, len(target.Target.ID))
		}
		for _, target := range evidence.Targets {
			var cells []string
			for _, i := range shown {
				cell := resultWords(evidence, target, i)
				if where {
					cell = engine.DescribeEnvironment(environments[i]) + " " + cell
				}
				cells = append(cells, cell)
			}
			fmt.Fprintf(out, "%s%-*s  %s\n", indent, width, target.Target.ID, strings.Join(cells, "   "))
		}
		return
	}
	grid := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintf(grid, "%sPORT", indent)
	for _, i := range shown {
		fmt.Fprintf(grid, "\t%s", engine.EnvironmentHeading(environments[i], environments))
	}
	fmt.Fprintln(grid)
	for _, target := range evidence.Targets {
		fmt.Fprintf(grid, "%s%s", indent, target.Target.ID)
		for _, i := range shown {
			fmt.Fprintf(grid, "\t%s", resultWords(evidence, target, i))
		}
		fmt.Fprintln(grid)
	}
	_ = grid.Flush()
}

// resultWords are a target's result in one environment, as the pull
// request words it, and on the terminal, why no later check can reuse a
// passed one, where its rebuild was silent (the architecture review's
// L3a).
func resultWords(evidence engine.Evidence, target engine.TargetEvidence, environment int) string {
	words := engine.EvidenceWords(evidence, target, environment, engine.AcceptedFailure{})
	// A cell another check gave names it, unless its tests' reading did.
	if check := evidence.CellCheck(target.Outcomes[environment]); check != "" && !strings.Contains(words, check) {
		words += " (" + check + ")"
	}
	if reason := target.Outcomes[environment].NotReusable; reason != "" {
		words += "; not reusable: " + reason
	}
	return words
}

// writeVariants says what --variants builds: the one port with its
// variants in place of its defaults, or, with each, its defaults and then
// each variant it declares.
func WriteVariants(out io.Writer, plan model.Plan) {
	var builds []string
	var port string
	for _, target := range plan.Targets {
		if spec := target.Target.VariantSpec(); spec != "" {
			port = target.Target.Name
			builds = append(builds, spec)
		}
	}
	switch {
	case plan.EachVariant && len(builds) > 0:
		fmt.Fprintf(out, "Variants    %s with default variants, then each of %s added to the defaults (excluding universal)\n", port, strings.Join(builds, ", "))
	case plan.Variants != "" && len(builds) > 0:
		fmt.Fprintf(out, "Variants    %s %s, in place of its defaults\n", port, plan.Variants)
	}
}

// writeQueue is status's list of the checks queued and running, oldest
// first, where queue listed them: each with its branch, what it checks,
// where, and its state, a run whose process ended said to be stopped.
func WriteQueue(out io.Writer, queue []engine.QueuedCheck) {
	if len(queue) == 0 {
		return
	}
	fmt.Fprintln(out, "\nChecks queued and running")
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "  RUN\tBRANCH\tSOURCE\tON\tSTATE\tDETAIL")
	for _, queued := range queue {
		run := queued.Run
		var on []string
		for _, environment := range queued.Plan.Environments {
			on = append(on, engine.DescribeEnvironment(environment))
		}
		state, detail := string(run.State), run.Detail
		if run.Origin == model.OriginServe && detail == "" {
			detail = "started by serve"
		}
		// A running check says where it is, as the guest reports it.
		if queued.Progress != "" {
			detail = queued.Progress
		}
		if queued.Stopped {
			state, detail = "stopped", "the process running it ended; dockhand wait "+run.Name()+" resumes it, cancel ends it"
		}
		fmt.Fprintf(table, "  %s\t%s\t%s\t%s\t%s\t%s\n", run.Name(), queued.Branch.ShortName(), engine.Describe(queued.Revision), strings.Join(on, ", "), state, detail)
	}
	_ = table.Flush()
}

// queueView is the queue for status's JSON.
func QueueView(queue []engine.QueuedCheck) *[]queuedRunJSON {
	rows := []queuedRunJSON{}
	for _, queued := range queue {
		row := queuedRunJSON{runJSON: RunView(queued.Run), Branch: queued.Branch.ShortName(), Revision: engine.Describe(queued.Revision), Environments: []environmentJSON{}}
		row.Stopped, row.Progress = queued.Stopped, queued.Progress
		for _, environment := range queued.Plan.Environments {
			row.Environments = append(row.Environments, EnvironmentView(environment))
		}
		rows = append(rows, row)
	}
	return &rows
}
