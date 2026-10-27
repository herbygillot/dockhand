package assess

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// cxxLines are an assessment's findings of the C++ standard.
func cxxLines(comparison model.UpstreamComparison) []string {
	var lines []string
	for _, change := range comparison.Changes {
		if change.Rule == CXXStandardRule {
			lines = append(lines, change.Message)
		}
	}
	return lines
}

// A C++ standard upstream's CMake files raised above the Portfile's
// compiler.cxx_standard is said, with the file and line: i2pd 3.0.0's
// build/CMakeLists.txt probes -std=c++23 and -std=c++20 where 2.61.0 set
// C++17, and field testing raised the Portfile's 2017 to 2020 by hand. A
// floor that didn't move, or a Portfile already there, says nothing (the
// docs dig's I20).
func TestARaisedCXXStandardIsSaid(t *testing.T) {
	old := "cmake_minimum_required(VERSION 3.10)\nproject(i2pd)\nset_target_properties(i2pd PROPERTIES CXX_STANDARD 17)\n"
	raised := "cmake_minimum_required(VERSION 3.10)\nproject(i2pd)\ninclude(CheckCXXCompilerFlag)\nCHECK_CXX_COMPILER_FLAG(-std=c++23 CXX23_SUPPORTED)\nCHECK_CXX_COMPILER_FLAG(-std=c++20 CXX20_SUPPORTED)\nset_target_properties(i2pd PROPERTIES CXX_STANDARD ${CXX_STD})\n"
	reading := func(top, cmake string) project.Reading {
		return read(t, top, map[string]string{"LICENSE": "BSD\n", "build/CMakeLists.txt": cmake}, project.Spec{Directories: []string{"build"}})
	}
	port := func(standard string) macports.PortInfo {
		return macports.PortInfo{Name: "i2pd", Options: map[string]string{"compiler.cxx_standard": standard}}
	}
	input := Input{Port: port("2017"), Base: port("2017"), Pairs: []Pair{{Archive: "i2pd-3.0.0.tar.gz", Before: reading("i2pd-2.61.0", old), After: reading("i2pd-3.0.0", raised)}}}
	require.Equal(t, []string{"upstream: build/CMakeLists.txt:5 asks for C++20, above the Portfile's compiler.cxx_standard 2017; MacPorts picks the compiler from it, so the older releases may need 2020"}, cxxLines(Assess(input)))

	input.Port = port("2020")
	require.Empty(t, cxxLines(Assess(input)), "the Portfile is there already")

	input.Port = port("2017")
	input.Pairs[0].After = reading("i2pd-2.62.0", old)
	require.Empty(t, cxxLines(Assess(input)), "the floor didn't move")
}

// A CMake document's C++ floor is the lowest standard it names, however
// it names one.
func TestACMakeDocumentsCXXFloor(t *testing.T) {
	for cmake, year := range map[string]int{
		"set(CMAKE_CXX_STANDARD 14)\n":                           2014,
		"target_compile_features(t PUBLIC cxx_std_17)\n":         2017,
		"add_compile_options(-std=gnu++2a)\n":                    2020,
		"set(CMAKE_CXX_STANDARD 20)\nset(CMAKE_CXX_STANDARD 17)": 2017,
	} {
		document := read(t, "demo-1", map[string]string{"CMakeLists.txt": cmake}, project.Spec{}).CMakeDocument("CMakeLists.txt")
		floor, ok := document.CXXFloor()
		require.True(t, ok, cmake)
		require.Equal(t, year, floor.Year, cmake)
	}
	_, ok := read(t, "demo-1", map[string]string{"CMakeLists.txt": "project(demo)\n"}, project.Spec{}).CMakeDocument("CMakeLists.txt").CXXFloor()
	require.False(t, ok)
}
