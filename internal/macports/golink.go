package macports

import (
	"regexp"
	"slices"
	"strings"
)

// GoLinkTarget is a Go linker -X argument a port's build passes: the
// argument as written, and the package and variable it sets, which Go's
// linker ignores where the package declares no such variable, so a port's
// version string goes silently wrong when upstream moves it (the docs
// dig's I18).
type GoLinkTarget struct {
	Argument, Package, Name string
}

// goLinkTarget is a -X argument: its import path, to the last dot before
// the variable's name, then the name and an equals sign.
var goLinkTarget = regexp.MustCompile(`-X[ =]['"]?([^\s'"=]+)\.([A-Za-z_][A-Za-z0-9_]*)=`)

// GoLinkTargets are the -X arguments the port's evaluated build arguments
// pass, each once, in order.
func (p PortInfo) GoLinkTargets() []GoLinkTarget {
	var targets []GoLinkTarget
	for _, option := range []string{"build.pre_args", "build.args"} {
		for _, match := range goLinkTarget.FindAllStringSubmatch(p.Options[option], -1) {
			target := GoLinkTarget{Argument: strings.Trim(strings.TrimPrefix(strings.TrimPrefix(match[0], "-X"), "="), ` '"`), Package: match[1], Name: match[2]}
			target.Argument = "-X " + strings.TrimSuffix(target.Argument, "=")
			if !slices.Contains(targets, target) {
				targets = append(targets, target)
			}
		}
	}
	return targets
}

// GoLinkPackages are the import paths of GoLinkTargets, each once, which a
// reading keeps the Go files of (project.Spec's GoPackages).
func (p PortInfo) GoLinkPackages() []string {
	var packages []string
	for _, target := range p.GoLinkTargets() {
		if !slices.Contains(packages, target.Package) {
			packages = append(packages, target.Package)
		}
	}
	return packages
}
