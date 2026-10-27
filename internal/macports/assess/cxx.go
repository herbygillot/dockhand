package assess

import (
	"fmt"
	"path"
	"strconv"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// MacPorts picks a compiler from compiler.cxx_standard, so a C++ standard
// upstream raised that the Portfile misses builds on the host's release,
// with a recent clang, and fails on the older releases the buildbots build:
// field testing raised i2pd 3.0.0's to 2020 by hand, where its
// build/CMakeLists.txt probes -std=c++23 and -std=c++20 (the docs dig's
// I20). It's said, and holds nothing until its false positives are
// measured; a floor that didn't move says nothing, since the port built
// with it before.

// cxxFloor is the C++ standard a version's CMake files need, the highest
// of each CMakeLists.txt's lowest (project.CMakeDocument.CXXFloor), with
// the file and line that names it; false where none names one.
func cxxFloor(pairs []Pair, side func(Pair) project.Reading) (project.CXXStandard, bool) {
	var floor project.CXXStandard
	found := false
	for _, pair := range pairs {
		reading := side(pair)
		for name := range reading.Files {
			if path.Base(name) != "CMakeLists.txt" {
				continue
			}
			standard, ok := reading.CMakeDocument(name).CXXFloor()
			if !ok || found && standard.Year <= floor.Year {
				continue
			}
			if standard.File == "" {
				standard.File = name
			}
			standard.File = path.Join(reading.Root, standard.File)
			floor, found = standard, true
		}
	}
	return floor, found
}

// cxxStandard says a C++ standard the new version's CMake files raised above
// both the old version's and the Portfile's compiler.cxx_standard.
func (a *assessment) cxxStandard() (model.UpstreamChange, bool) {
	now, ok := cxxFloor(a.input.Pairs, after)
	if !ok {
		return model.UpstreamChange{}, false
	}
	if was, ok := cxxFloor(a.input.Pairs, before); !ok || was.Year >= now.Year {
		return model.UpstreamChange{}, false
	}
	declared, err := strconv.Atoi(a.input.Port.Options["compiler.cxx_standard"])
	if err != nil || declared >= now.Year {
		return model.UpstreamChange{}, false
	}
	return model.UpstreamChange{Kind: "cxx-standard", Path: now.File, Rule: CXXStandardRule, Subject: strconv.Itoa(now.Year), Class: model.Introduced,
		Message: fmt.Sprintf("upstream: %s:%d asks for C++%02d, above the Portfile's compiler.cxx_standard %d; MacPorts picks the compiler from it, so the older releases may need %d",
			now.File, now.Line, now.Year%100, declared, now.Year)}, true
}
