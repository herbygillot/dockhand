package assess

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
)

// pythonLines are an assessment's findings of the Pythons a port runs on,
// marked, with their class.
func pythonLines(comparison model.UpstreamComparison) []string {
	var lines []string
	for _, change := range comparison.Changes {
		if change.Rule == RequiresPythonRule || change.Rule == PythonPinBehind {
			lines = append(lines, messages([]model.UpstreamChange{change})[0]+" ["+string(change.Class)+"]")
		}
	}
	return lines
}

// pyproject is a reading of a project whose pyproject.toml requires the
// Pythons given, or none where requires is empty and the file is absent.
func pyproject(t *testing.T, top, requires string) project.Reading {
	t.Helper()
	files := map[string]string{"LICENSE": "MIT\n"}
	if requires != "-" {
		files["pyproject.toml"] = "[project]\nname = \"demo\"\nrequires-python = \"" + requires + "\"\n"
	}
	return read(t, top, files, project.Spec{})
}

// pythonApp is a port of the python PortGroup that pins a Python, as an
// application's does, where the PortGroup's default is another. As MacPorts
// evaluates one, its python.versions are the pin, so the default computed
// in the port is the pin too, and the PortGroup's own is apart.
func pythonApp(name, pinned, standard string) macports.PortInfo {
	port := pythonPort(name)
	for option, value := range map[string]string{"python.version": pinned, "python.versions": pinned, "python.default_version": pinned, "dockhand.python_default": pinned, "dockhand.python_group_default": standard} {
		port.Options[option] = value
	}
	return port
}

// A Python project's requires-python is said where it moves, as sshuttle
// 2.0.0 raised it from >=3.9 to >=3.10, with whether it admits the Python
// the port builds with; and a pin behind the python PortGroup's default is
// a quiet note, where the new version admits the default (the sshuttle
// run).
func TestAPythonProjectsRequiresPythonIsSaid(t *testing.T) {
	sshuttle := pythonApp("sshuttle", "313", "314")
	input := Input{Port: sshuttle, Base: sshuttle, Pairs: []Pair{{Archive: "sshuttle-2.0.0.tar.gz", Before: pyproject(t, "sshuttle-1.3.2", ">=3.9"), After: pyproject(t, "sshuttle-2.0.0", ">=3.10")}}}
	comparison := Assess(input)
	require.Equal(t, []string{
		"· upstream: pyproject.toml's requires-python moves from >=3.9 to >=3.10, which admits Python 3.13, which the port builds with [introduced]",
		"· the Portfile pins Python 3.13 with python.default_version, behind the python PortGroup's default, 3.14, which pyproject.toml's requires-python admits [present]",
	}, pythonLines(comparison))

	// A pin below the default that the new version's ceiling leaves out
	// is upstream's own, and nothing's said of it.
	input.Pairs[0].After = pyproject(t, "sshuttle-2.0.0", ">=3.10,<3.14")
	require.Equal(t, []string{"· upstream: pyproject.toml's requires-python moves from >=3.9 to >=3.10,<3.14, which admits Python 3.13, which the port builds with [introduced]"}, pythonLines(Assess(input)))

	// Nothing moved and nothing's left out: nothing to say of it.
	input.Pairs[0].After = pyproject(t, "sshuttle-2.0.0", ">=3.9")
	input.Port = pythonApp("sshuttle", "314", "314")
	require.Empty(t, pythonLines(Assess(input)))

	// A port of another kind hears the move alone.
	input.Port, input.Base = macports.PortInfo{Name: "tool"}, macports.PortInfo{Name: "tool"}
	input.Pairs[0].After = pyproject(t, "tool-2", ">=3.10")
	require.Equal(t, []string{"· upstream: pyproject.toml's requires-python moves from >=3.9 to >=3.10 [introduced]"}, pythonLines(Assess(input)))
}

// A requires-python that leaves out a Python the port builds for holds, as
// a build needn't enforce it, unless the base's left it out too; where the
// base's project said nothing of it, what the base did is unknown.
func TestARequiresPythonThatLeavesOutAPythonTheBuildsForHolds(t *testing.T) {
	stub := func(versions string) macports.PortInfo {
		port := pythonPort("py-demo")
		port.Options["python.versions"] = versions
		return port
	}
	input := Input{Port: stub("39 310 311"), Base: stub("39 310 311"), Pairs: []Pair{{Archive: "demo-2.tar.gz", Before: pyproject(t, "demo-1", ">=3.8"), After: pyproject(t, "demo-2", ">=3.10")}}}
	comparison := Assess(input)
	require.Equal(t, []string{"! upstream: pyproject.toml's requires-python moves from >=3.8 to >=3.10, which leaves out Python 3.9, which the port builds for: its python.versions may need to follow [introduced]"}, pythonLines(comparison))
	require.Equal(t, RequiresPythonRule, comparison.Changes[len(comparison.Changes)-1].Rule)

	input.Pairs[0].Before = pyproject(t, "demo-1", ">=3.10")
	require.Equal(t, []string{"· upstream: pyproject.toml requires Python >=3.10, which leaves out Python 3.9, which the port builds for: its python.versions may need to follow; the base's left it out too [present]"}, pythonLines(Assess(input)))

	input.Base = stub("310 311")
	require.Equal(t, []string{"! upstream: pyproject.toml requires Python >=3.10, which leaves out Python 3.9, which the port builds for: its python.versions may need to follow [introduced]"}, pythonLines(Assess(input)), "the candidate added 3.9")

	input.Base, input.Pairs[0].Before = stub("39 310 311"), pyproject(t, "demo-1", "-")
	require.Equal(t, []string{"! upstream: pyproject.toml now requires Python >=3.10, which leaves out Python 3.9, which the port builds for: its python.versions may need to follow [unknown-baseline]"}, pythonLines(Assess(input)))

	// A subport is judged for its own Python, and says nothing of a pin.
	input.Port, input.Base = pythonApp("py39-demo", "311", "314"), pythonApp("py39-demo", "311", "314")
	input.Pairs[0].Before = pyproject(t, "demo-1", ">=3.8")
	require.Equal(t, []string{"! upstream: pyproject.toml's requires-python moves from >=3.8 to >=3.10, which leaves out Python 3.9, which the port builds for: its python.versions may need to follow [introduced]"}, pythonLines(Assess(input)))

	// An application's port is told of its pin.
	input.Port, input.Base = pythonApp("demo", "39", "314"), pythonApp("demo", "39", "314")
	require.Equal(t, []string{
		"! upstream: pyproject.toml's requires-python moves from >=3.8 to >=3.10, which leaves out Python 3.9, which the port builds for: its python.default_version may need to follow [introduced]",
		"· the Portfile pins Python 3.9 with python.default_version, behind the python PortGroup's default, 3.14, which pyproject.toml's requires-python admits [present]",
	}, pythonLines(Assess(input)))
}

// A requires-python whose verdict on a series its patch release decides,
// as >=3.13.2's on 3.13 is, is judged by the release MacPorts has, which
// Wanted asks for; one that can't be observed holds, as not judged.
func TestARequiresPythonOfAPatchReleaseIsJudgedByMacPortsRelease(t *testing.T) {
	port := pythonPort("py313-demo")
	input := Input{Port: port, Base: port, Pairs: []Pair{{Archive: "demo-2.tar.gz", Before: pyproject(t, "demo-1", ">=3.10"), After: pyproject(t, "demo-2", ">=3.13.2")}}}
	require.Equal(t, []Provider{{Port: "python313"}}, Wanted(input))
	moved := "upstream: pyproject.toml's requires-python moves from >=3.10 to >=3.13.2, which "
	require.Equal(t, []string{"· " + moved + "admits Python 3.13, which the port builds with [introduced]"},
		pythonLines(observed(t, input, map[Provider]Observation{{Port: "python313"}: {Version: "3.13.7"}})))
	require.Equal(t, []string{"! " + moved + "leaves out Python 3.13, which the port builds for: its python.versions may need to follow [introduced]"},
		pythonLines(observed(t, input, map[Provider]Observation{{Port: "python313"}: {Version: "3.13.1"}})))
	require.Equal(t, []string{"! " + moved + "couldn't be judged against Python 3.13 as MacPorts has it [introduced]"},
		pythonLines(observed(t, input, nil)))
}

// A build file the project's backend doesn't read holds nothing: sshuttle
// builds with hatchling, which never reads setup.cfg, where bumpversion
// keeps its version, and "setup.cfg changed" held its update (the sshuttle
// run with f075232d). setuptools reads it, and a backend of the project's
// own may, so there it holds; and bumpversion's current_version is the
// project's own version, as setup.cfg's version is.
func TestASetupFileTheBackendDoesntReadHoldsNothing(t *testing.T) {
	project := func(top, backend, cfg string) project.Reading {
		return read(t, top, map[string]string{"pyproject.toml": "[build-system]\nrequires = [\"x\"]\nbuild-backend = \"" + backend + "\"\n", "setup.cfg": cfg}, project.Spec{})
	}
	port := pythonApp("sshuttle", "313", "313")
	compare := func(backend, before, after string) []string {
		return messages(Assess(Input{Port: port, Base: port, Versions: sourcecompare.Versions{Old: "1.3.2", New: "2.0.0"},
			Pairs: []Pair{{Archive: "sshuttle.tar.gz", Before: project("sshuttle-1.3.2", backend, before), After: project("sshuttle-2.0.0", backend, after)}}}).Changes)
	}
	bumped := [2]string{"[bumpversion]\ncurrent_version = 1.3.2\n", "[bumpversion]\ncurrent_version = 2.0.0\n"}
	flake := [2]string{"[flake8]\nmax-line-length = 80\n", "[flake8]\nmax-line-length = 100\n"}
	require.Equal(t, []string{"· upstream's setup.cfg changed only the version it names: \"current_version = 2.0.0\""}, compare("hatchling.build", bumped[0], bumped[1]))
	require.Equal(t, []string{"· upstream's setup.cfg changed; the project builds with hatchling.build, which doesn't read setup.cfg, so it holds nothing"}, compare("hatchling.build", flake[0], flake[1]))
	require.Equal(t, []string{"! upstream's setup.cfg changed; the build may need the Portfile to follow"}, compare("setuptools.build_meta", flake[0], flake[1]))
	require.Equal(t, []string{"! upstream's setup.cfg changed; the build may need the Portfile to follow"}, compare("_custom_build", flake[0], flake[1]), "a backend of the project's own may read it")
}
