package project

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A pyproject.toml's [project], build system, and Poetry table are read.
// One of the project's own requirements that isn't one fails the reading;
// one of the build system's or an optional group's is kept as unparsed.
func TestAPyprojectIsReadWithItsBuildSystem(t *testing.T) {
	manifest, err := ReadPyproject([]byte(`[project]
name = "demo"
license = { text = "MIT" }
description = "A demo"
requires-python = ">=3.10"
dependencies = ["requests[socks] (>=2); sys_platform != 'win32'"]
optional-dependencies = { cli = ["rich>=13", "!!"] }

[build-system]
requires = ["hatchling>=1", "@@"]
build-backend = "hatchling.build"

[tool.poetry.dependencies]
python = "^3.10"
Some_Package = { git = "https://example.org/p" }
`))
	require.NoError(t, err)
	require.Equal(t, &PythonProject{Name: "demo", Description: "A demo", RequiresPython: ">=3.10",
		Dependencies: []Declaration{{Requirement: Requirement{Name: "requests", Specifier: ">=2", Marker: "sys_platform != 'win32'"}, Written: "[socks] (>=2)"}},
		Optional:     map[string][]Declaration{"cli": {{Requirement: Requirement{Name: "rich", Specifier: ">=13"}, Written: ">=13"}}},
	}, manifest.Project)
	require.Equal(t, []Declaration{{Requirement: Requirement{Name: "hatchling", Specifier: ">=1"}, Written: ">=1"}}, manifest.BuildRequires)
	require.Equal(t, "hatchling.build", manifest.BuildBackend)
	require.Equal(t, []string{"!!", "@@"}, manifest.Unparsed)
	require.Equal(t, map[string]string{"some-package": "git https://example.org/p"}, manifest.Poetry)

	_, err = ReadPyproject([]byte("[project]\ndependencies = [\"@@\"]\n"))
	require.ErrorContains(t, err, `"@@" isn't a requirement`)
}
