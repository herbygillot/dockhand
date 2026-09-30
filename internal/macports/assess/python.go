package assess

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// A Python project's requires-python is its own line, as go.mod's go
// directive is a Go project's: the Pythons it runs on. sshuttle 2.0.0
// raised it from >=3.9 to >=3.10 (the sshuttle run). A move is said, and
// one that leaves out a Python the port builds for holds, since a build
// needn't enforce it: a wheel is installed without asking.

// pythonRequires is what a version's pyproject.toml says of the Pythons
// its project runs on, and the file; false where no archive read holds a
// pyproject.toml at its root that could be read, with a [project] table.
func pythonRequires(pairs []Pair, side func(Pair) project.Reading) (specifier, file string, ok bool) {
	for _, pair := range pairs {
		reading := side(pair)
		found, ok := reading.RootFile("pyproject.toml")
		if !ok || found.Truncated {
			continue
		}
		manifest, err := project.ReadPyproject(found.Data)
		if err != nil || manifest.Project == nil {
			continue
		}
		return manifest.Project.RequiresPython, path.Join(reading.Root, "pyproject.toml"), true
	}
	return "", "", false
}

// admits says whether a specifier admits a Python series as MacPorts
// builds it, in the base's tree or the candidate's. The series' first
// release and a late one agree for a specifier that says nothing of patch
// releases, which is most; where they differ, as for >=3.13.2, the release
// MacPorts has of it says, as observed (Wanted). Known is false where it
// wasn't, or the specifier can't be read.
func (a *assessment) admits(specifier, python string, base bool) (admitted, known bool) {
	first, err := project.Admits(specifier, python)
	if err != nil {
		return false, false
	}
	late, err := project.Admits(specifier, python+".99")
	if err != nil {
		return false, false
	}
	if first == late {
		return first, true
	}
	observation, ok := a.input.Observed[Provider{Port: macports.PythonPort(python), Base: base}]
	if !ok || observation.Problem != "" {
		return false, false
	}
	admitted, err = project.Admits(specifier, observation.Version)
	return admitted, err == nil
}

// ambiguous reports a specifier whose verdict on a Python series its
// patch release decides, which Wanted asks MacPorts' version of.
func ambiguous(specifier, python string) bool {
	first, err := project.Admits(specifier, python)
	if err != nil {
		return false
	}
	late, err := project.Admits(specifier, python+".99")
	return err == nil && first != late
}

// pythonsWanted are the Python ports whose versions judging requires-python
// needs: the candidate's, of each series it builds for, and the base's,
// where a specifier's verdict turns on the patch release.
func (a *assessment) pythonsWanted() []Provider {
	var wanted []Provider
	for side, input := range []struct {
		port macports.PortInfo
		read func(Pair) project.Reading
	}{{a.input.Port, after}, {a.input.Base, before}} {
		specifier, _, ok := pythonRequires(a.input.Pairs, input.read)
		if !ok || specifier == "" {
			continue
		}
		for _, python := range input.port.Pythons() {
			if ambiguous(specifier, python) {
				wanted = append(wanted, Provider{Port: macports.PythonPort(python), Base: side == 1})
			}
		}
	}
	return wanted
}

func after(p Pair) project.Reading  { return p.After }
func before(p Pair) project.Reading { return p.Before }

// requiresPython is the new version's requires-python as a finding: said
// where it moved, and held where it leaves out a Python the port builds
// for, or can't be judged against one. One the base left out as well, with
// the Python built there too, is said and holds nothing (D14); where the
// base's project said nothing of it, its baseline is unknown.
func (a *assessment) requiresPython() (model.UpstreamChange, bool) {
	now, file, ok := pythonRequires(a.input.Pairs, after)
	if !ok || now == "" {
		return model.UpstreamChange{}, false
	}
	was, _, hadBefore := pythonRequires(a.input.Pairs, before)
	hadBase := a.input.Base.Name != ""
	pythons := a.input.Port.Pythons()
	var excluded, unknown []string
	for _, python := range pythons {
		switch admitted, known := a.admits(now, python, false); {
		case !known:
			unknown = append(unknown, python)
		case !admitted:
			excluded = append(excluded, python)
		}
	}
	moved := hadBefore && was != now || !hadBefore && hadBase
	if !moved && len(excluded) == 0 && len(unknown) == 0 {
		return model.UpstreamChange{}, false
	}
	found := model.UpstreamChange{Kind: "python", Path: file, Rule: RequiresPythonRule, Subject: now, Class: model.Introduced}
	switch {
	case hadBefore && was != "" && was != now:
		found.Message = fmt.Sprintf("upstream: %s's requires-python moves from %s to %s", file, was, now)
	case moved:
		found.Message = fmt.Sprintf("upstream: %s now requires Python %s", file, now)
	default:
		found.Message = fmt.Sprintf("upstream: %s requires Python %s", file, now)
	}
	// A py- port builds for its python.versions; any other's follow its
	// pin, which is what a person moves.
	option := "python.default_version"
	if _, subport := macports.PythonPackage(a.input.Port.Name); subport || strings.HasPrefix(a.input.Port.Name, "py-") {
		option = "python.versions"
	}
	switch {
	case len(excluded) > 0:
		found.Hold = true
		found.Message += fmt.Sprintf(", which leaves out Python %s, which the port builds for: its %s may need to follow", series(excluded), option)
		already := hadBefore && was != ""
		for _, python := range excluded {
			admitted, known := a.admits(was, python, true)
			already = already && slices.Contains(a.input.Base.Pythons(), python) && known && !admitted
		}
		switch {
		case hadBase && !hadBefore:
			found.Class = model.UnknownBaseline
		case already:
			found.Class, found.Hold = model.Present, false
			found.Message += "; the base's left it out too"
		}
	case len(unknown) > 0:
		found.Hold = true
		found.Message += fmt.Sprintf(", which couldn't be judged against Python %s as MacPorts has it", series(unknown))
	case len(pythons) == 1:
		found.Message += fmt.Sprintf(", which admits Python %s, which the port builds with", pythons[0])
	case len(pythons) > 1:
		found.Message += fmt.Sprintf(", which admits each Python the port builds for, %s", series(pythons))
	}
	return found, true
}

// pythonPin is a quiet note where a port pins an older Python than the
// python PortGroup's default, as sshuttle pins 3.13 where the default is
// 3.14 (the sshuttle run): nothing an update does, but what a person
// updating it may want to move. It holds nothing, and isn't said where the
// new version's requires-python leaves the default out, the pin then being
// upstream's own ceiling, nor of a Python version's subport, whose stub
// carries the pin.
func (a *assessment) pythonPin() (model.UpstreamChange, bool) {
	port := a.input.Port
	if _, subport := macports.PythonPackage(port.Name); subport {
		return model.UpstreamChange{}, false
	}
	pinned, standard, ok := port.PythonPinned()
	if !ok {
		return model.UpstreamChange{}, false
	}
	if older, err := project.Admits("<"+standard, pinned); err != nil || !older {
		return model.UpstreamChange{}, false
	}
	said := ""
	if now, file, ok := pythonRequires(a.input.Pairs, after); ok && now != "" {
		admitted, err := project.Admits(now, standard+".99")
		if err != nil || !admitted {
			return model.UpstreamChange{}, false
		}
		said = fmt.Sprintf(", which %s's requires-python admits", file)
	}
	found := model.UpstreamChange{Kind: "python", Path: "Portfile", Rule: PythonPinBehind, Subject: pinned, Class: model.Introduced,
		Message: fmt.Sprintf("the Portfile pins Python %s with python.default_version, behind the python PortGroup's default, %s%s", pinned, standard, said)}
	if was, _, ok := a.input.Base.PythonPinned(); ok && was == pinned {
		found.Class = model.Present
	}
	return found, true
}

// series says Python versions for a person: 3.9, or 3.9 and 3.10, or 3.9,
// 3.10, and 3.11.
func series(pythons []string) string {
	switch len(pythons) {
	case 1:
		return pythons[0]
	case 2:
		return pythons[0] + " and " + pythons[1]
	}
	return strings.Join(pythons[:len(pythons)-1], ", ") + ", and " + pythons[len(pythons)-1]
}
