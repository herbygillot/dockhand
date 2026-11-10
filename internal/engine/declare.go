package engine

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// Declaration is a dependency tidy writes from a passed check's link
// finding: the port the target linked and didn't declare, the spec
// written for it, and the evidence, as the commit's body says it.
type Declaration struct {
	Directory, Target, Port, Spec string
	Evidence                      string
}

// declareTidy adds to a tidy plan the commit that declares what the
// branch's passed check found its builds link and not declare,
// "<port>: declare libiconv", after the groups and before --restyle's
// (docs/designs/declare-linked-libraries.md, part 2): a line each,
// written as the tree writes a dependency on that port most, after the
// Portfile's last depends_lib. A finding it can't write is said in
// NotDeclared: a subport's of a Portfile with several ports, a variant
// build's, and libiconv where the port sets rust.system_libiconv yes. A
// finding of unread links, or of a library no port provides, names no
// line to write. Only the check of the files as they are counts.
func (e *Engine) declareTidy(ctx context.Context, plan *TidyPlan) error {
	checked, ok, err := e.EvidenceFor(ctx, plan.Branch.ID, model.ObjectID(plan.Final))
	if err != nil || !ok {
		return err
	}
	findings, err := e.linkFindings(ctx, checked)
	if err != nil || len(findings) == 0 {
		return err
	}
	worktree, err := e.worktree(ctx, plan.Branch)
	if err != nil {
		return err
	}
	targets := map[model.TargetID]model.PlanTarget{}
	for _, target := range checked.Targets {
		targets[target.Target.ID] = target.Target
	}
	texts := map[string][]byte{}
	var order []string
	var declared []Declaration
	for _, finding := range findings {
		if finding.Port == "" || finding.Unread || finding.Clean || finding.Unlinked {
			continue
		}
		target, ok := targets[finding.Target]
		if !ok {
			continue
		}
		name := target.Directory + "/Portfile"
		text, read := texts[name]
		if !read {
			state, data, err := worktree.File(ctx, plan.Final, name)
			if err != nil {
				return err
			}
			if !state.Exists {
				continue
			}
			text = data
		}
		if why := undeclarable(text, target.Target, finding); why != "" {
			plan.NotDeclared = append(plan.NotDeclared, why)
			continue
		}
		spec := e.declarationSpec(ctx, plan.Final, finding)
		next, ok := portfile.DeclareLibrary(text, spec)
		if !ok {
			plan.NotDeclared = append(plan.NotDeclared, fmt.Sprintf("%s links %s from %s, which it doesn't declare; its Portfile has no depends_ or checksums line at its top to write depends_lib-append %s after", builtPort(target.Target), path.Base(finding.Library), finding.Port, spec))
			continue
		}
		if !read {
			order = append(order, name)
		}
		texts[name] = next
		declared = append(declared, Declaration{Directory: target.Directory, Target: builtPort(target.Target), Port: finding.Port, Spec: spec,
			Evidence: fmt.Sprintf("%s's builds of %s on %s link %s from %s, which the Portfile didn't declare", checked.Run.Name(), builtPort(target.Target), environmentsWords(finding.Environments), path.Base(finding.Library), finding.Port)})
	}
	if len(declared) == 0 {
		return nil
	}
	var edits []git.FileEdit
	for _, name := range order {
		state, _, err := worktree.File(ctx, plan.Final, name)
		if err != nil {
			return err
		}
		edits = append(edits, git.FileEdit{Path: name, Before: state, After: texts[name], Mode: state.Mode})
	}
	if plan.Declared, err = worktree.EditTree(ctx, plan.Final, edits); err != nil {
		return err
	}
	author, err := worktree.Author(ctx)
	if err != nil {
		return err
	}
	author.When = e.now()
	var ports, libraries, lines []string
	for _, d := range declared {
		if !slices.Contains(ports, d.Target) {
			ports = append(ports, d.Target)
		}
		if !slices.Contains(libraries, d.Port) {
			libraries = append(libraries, d.Port)
		}
		lines = append(lines, d.Evidence+".")
	}
	group := TidyGroup{Ports: ports, Message: commitmsg.Compose(strings.Join(ports, ", ")+": declare "+prose.And(libraries), strings.Join(lines, "\n"), nil, nil, true),
		Author: author, Working: true, Notes: lines}
	for _, edit := range edits {
		group.Paths = append(group.Paths, edit.Path)
	}
	plan.Declare, plan.declared, plan.Declarations, plan.Keep = &group, edits, declared, false
	return nil
}

// undeclarable says why a link finding's line isn't written, as tidy's
// notes say it; empty where it is.
func undeclarable(text []byte, target model.Target, finding evidence.LinkFinding) string {
	links := fmt.Sprintf("%s links %s from %s, which it doesn't declare", builtPort(target), path.Base(finding.Library), finding.Port)
	switch {
	case len(target.Variants) > 0:
		return links + " in its " + target.VariantSpec() + " build alone; the line belongs in that variant's body, yours to write"
	case len(portfile.TopWords(text, "subport")) > 0 || target.Subport != "" || hasSubports(text):
		return links + "; its Portfile has several ports, and the line belongs in the port's own block, yours to write"
	case strings.EqualFold(finding.Port, "libiconv") && slices.Equal(portfile.TopWords(text, "rust.system_libiconv"), []string{"yes"}):
		// The rust PortGroup declares port:libiconv unless the port
		// links the system's; a build that links MacPorts' anyway has C
		// code using iconv (macports/macports-ports#36385).
		return fmt.Sprintf("%s sets rust.system_libiconv yes, but its build links MacPorts' libiconv (C code it builds uses iconv): drop the option, and the rust PortGroup declares port:libiconv", builtPort(target))
	}
	return ""
}

// subportCommand is a subport block at a Portfile's top.
var subportCommand = regexp.MustCompile(`(?m)^\s*subport\s`)

// hasSubports reports whether a Portfile declares a subport anywhere.
func hasSubports(text []byte) bool { return subportCommand.Match(text) }

// declarationSpec is the dependency tidy writes for a port a build links:
// the path-style one the tree writes most for the library linked, where
// the tree writes one more often than port:<port>, as openssl's
// path:lib/libssl.dylib:openssl for openssl3's libssl, else port:<port>.
func (e *Engine) declarationSpec(ctx context.Context, tree string, finding evidence.LinkFinding) string {
	plain := "port:" + finding.Port
	stem, _, _ := strings.Cut(path.Base(finding.Library), ".")
	if stem == "" {
		return plain
	}
	portFiles, err := e.Repo.GrepTree(ctx, tree, regexp.QuoteMeta(plain)+`([[:space:]]|$)`, "*/*/Portfile")
	if err != nil {
		return plain
	}
	pattern := `path:lib/` + regexp.QuoteMeta(stem) + `\.dylib:[^[:space:]]+`
	paths, err := e.Repo.GrepTree(ctx, tree, pattern, "*/*/Portfile")
	if err != nil || len(paths) <= len(portFiles) {
		return plain
	}
	files, err := e.Repo.ReadFiles(ctx, tree, paths[:min(len(paths), 50)])
	if err != nil {
		return plain
	}
	spec := regexp.MustCompile(pattern)
	counts := map[string]int{}
	for _, data := range files {
		for _, found := range slices.Compact(slices.Sorted(slices.Values(spec.FindAllString(string(data), -1)))) {
			counts[found]++
		}
	}
	most := ""
	for found, n := range counts {
		if n > counts[most] || n == counts[most] && found < most {
			most = found
		}
	}
	if most == "" || counts[most] <= len(portFiles) {
		return plain
	}
	return most
}

// environmentsWords name builds' environments by their releases, "macOS
// 12 (Monterey) arm64 and macOS 26 (Tahoe) arm64".
func environmentsWords(environments []model.Environment) string {
	var words []string
	for _, environment := range environments {
		words = append(words, macos.Describe(environment.Platform))
	}
	return prose.And(words)
}
