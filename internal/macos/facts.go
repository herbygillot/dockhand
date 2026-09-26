package macos

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The facts table (decisions 9-13 of the contracts direction): what each
// macOS release's developer tools are, as MacPorts Base sees them, for each
// architecture and profile. A modelled context answers Base's toolchain
// questions from it rather than from the host dockhand runs on. It is
// harvested, never edited by hand: tools/facts probes dockhand's Tart images
// and reads MacPorts' buildbot logs, and generates facts.json from what it
// finds (tools/facts/README.md).

// Profile is which developer tools a Mac has: the Command Line Tools alone,
// as dockhand's base images do, or full Xcode beside them, as its Xcode
// images and MacPorts' buildbots do.
type Profile string

const (
	ProfileTools Profile = "clt"
	ProfileXcode Profile = "xcode"
)

// Facts is one release's developer tools on one architecture and profile.
// An empty field is one the source doesn't tell.
type Facts struct {
	Darwin       int     `json:"darwin"`
	Architecture string  `json:"architecture"`
	Profile      Profile `json:"profile"`
	// MacOS is the release's product version the source ran, 26.6.2.
	MacOS string `json:"macos,omitempty"`
	// Xcode is Base's xcodeversion: Xcode's version, or none.
	Xcode string `json:"xcode"`
	// Tools is Base's xcodecltversion: the Command Line Tools package's
	// version, or none.
	Tools string `json:"tools"`
	// DeveloperDir is Base's developer_dir, what xcode-select chose.
	DeveloperDir string `json:"developer_dir,omitempty"`
	// SDKs are the SDKs in the Command Line Tools, by name, a link shown
	// as "MacOSX.sdk -> MacOSX26.5.sdk"; Base picks MacOSX<major>.sdk.
	SDKs []string `json:"sdks,omitempty"`
	// SDK is Base's macosx_sdk_version, the SDK it asks for: the release's
	// major version, 26, from macOS 11 on.
	SDK string `json:"sdk,omitempty"`
	// Clang is the build number of the tools' clang, 2100.1.1.101: what
	// Base's compiler selection and blacklists compare with.
	Clang string `json:"clang,omitempty"`
	// XCSelect is whether /usr/lib/libxcselect.dylib exists, which Base
	// asks to decide whether /usr/bin's tools are xcode-select's shims.
	XCSelect *bool  `json:"xcselect,omitempty"`
	Source   Source `json:"source"`
}

// Source is where a row of facts came from, and when.
type Source struct {
	// Kind is tart or buildbot.
	Kind string `json:"kind"`
	// From is the Tart image probed, or the buildbot builder and build.
	From string `json:"from"`
	Date string `json:"date"`
	// MacPorts is the Base version the facts were read with.
	MacPorts string `json:"macports,omitempty"`
}

// Source kinds, in order of preference where both describe the same
// release, architecture, and profile (decision 12).
const (
	SourceTart     = "tart"
	SourceBuildbot = "buildbot"
)

// Generation is the Command Line Tools generation, the major version,
// setup installs for a release (decision 13): what MacPorts' arm64 builder
// for the release runs, with its source. Where MacPorts' GitHub CI pins
// Xcode for a release, the pin is checked against it by hand when the
// table is regenerated (tools/facts/README.md); dockhand doesn't parse the
// CI script.
type Generation struct {
	Darwin int    `json:"darwin"`
	Tools  int    `json:"tools"`
	Source Source `json:"source"`
}

// FactsTable is the whole table, as facts.json holds it.
type FactsTable struct {
	Generated   string       `json:"generated"`
	Facts       []Facts      `json:"facts"`
	Generations []Generation `json:"generations"`
}

// Generation is the tools generation setup installs for a release, and
// whether the table has one.
func (t FactsTable) Generation(darwin int) (int, bool) {
	for _, generation := range t.Generations {
		if generation.Darwin == darwin {
			return generation.Tools, true
		}
	}
	return 0, false
}

//go:embed facts.json
var factsJSON []byte

var table = func() FactsTable {
	var t FactsTable
	if err := json.Unmarshal(factsJSON, &t); err != nil {
		panic(fmt.Sprintf("macos: facts.json: %v", err))
	}
	return t
}()

// Table is the checked-in facts table.
func Table() FactsTable { return table }

// Lookup is the preferred row for a release, architecture, and profile:
// a Tart image's where there is one, else a buildbot's.
func (t FactsTable) Lookup(darwin int, architecture string, profile Profile) (Facts, bool) {
	var found []Facts
	for _, facts := range t.Facts {
		if facts.Darwin == darwin && facts.Architecture == architecture && facts.Profile == profile {
			found = append(found, facts)
		}
	}
	if len(found) == 0 {
		return Facts{}, false
	}
	slices.SortStableFunc(found, func(a, b Facts) int { return sourceRank(a.Source.Kind) - sourceRank(b.Source.Kind) })
	return found[0], true
}

func sourceRank(kind string) int {
	switch kind {
	case SourceTart:
		return 0
	case SourceBuildbot:
		return 1
	}
	return 2
}

// Check reports the table's first unsound row: one without its key, its
// versions, or its source.
func (t FactsTable) Check() error {
	for i, facts := range t.Facts {
		where := fmt.Sprintf("facts[%d] (darwin %d %s %s)", i, facts.Darwin, facts.Architecture, facts.Profile)
		switch {
		case facts.Darwin < 8 || facts.Architecture == "":
			return fmt.Errorf("macos: %s: no release or architecture", where)
		case facts.Profile != ProfileTools && facts.Profile != ProfileXcode:
			return fmt.Errorf("macos: %s: unknown profile", where)
		case facts.Xcode == "" || facts.Tools == "":
			return fmt.Errorf("macos: %s: Xcode and tools versions are required, none where absent", where)
		case facts.Profile == ProfileTools && facts.Xcode != "none":
			return fmt.Errorf("macos: %s: the tools profile has no Xcode", where)
		case facts.Source.Kind != SourceTart && facts.Source.Kind != SourceBuildbot:
			return fmt.Errorf("macos: %s: unknown source %q", where, facts.Source.Kind)
		case facts.Source.From == "" || facts.Source.Date == "":
			return fmt.Errorf("macos: %s: the source and its date are required", where)
		case facts.Clang != "" && strings.Trim(facts.Clang, "0123456789.") != "":
			return fmt.Errorf("macos: %s: clang %q is not a build number", where, facts.Clang)
		}
	}
	seen := map[int]bool{}
	for _, generation := range t.Generations {
		switch {
		case generation.Tools <= 0 || generation.Darwin < 8:
			return fmt.Errorf("macos: generation for darwin %d: no release or tools", generation.Darwin)
		case seen[generation.Darwin]:
			return fmt.Errorf("macos: generation for darwin %d: given twice", generation.Darwin)
		case generation.Source.From == "" || generation.Source.Date == "":
			return fmt.Errorf("macos: generation for darwin %d: the source and its date are required", generation.Darwin)
		}
		seen[generation.Darwin] = true
	}
	return nil
}
