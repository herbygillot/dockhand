package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
)

// generate builds the facts table from Tart probes and a buildbot harvest.
// Each source row is kept, so the buildbot's stay as a cross-check where a
// Tart image covers the same release; where one source gives a release,
// architecture, and profile more than once, the newest wins.
func generate(tartDirs []string, buildbot, out string) error {
	var rows []macos.Facts
	for _, dir := range tartDirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			row, ok, err := tartRow(data)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if ok {
				rows = append(rows, row)
			}
		}
	}
	if buildbot != "" {
		data, err := os.ReadFile(buildbot)
		if err != nil {
			return err
		}
		var harvest []builderFacts
		if err := json.Unmarshal(data, &harvest); err != nil {
			return fmt.Errorf("%s: %w", buildbot, err)
		}
		for _, builder := range harvest {
			rows = append(rows, buildbotRow(builder))
		}
	}
	table := macos.FactsTable{Generated: time.Now().UTC().Format(time.DateOnly), Facts: newest(rows), Generations: generations(rows)}
	if err := table.Check(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(table, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, append(data, '\n'), 0o644)
}

// newest keeps one row per release, architecture, profile, and source,
// the latest, sorted by those.
func newest(rows []macos.Facts) []macos.Facts {
	key := func(f macos.Facts) string {
		return fmt.Sprintf("%03d %s %s %s %s", f.Darwin, f.Architecture, f.Profile, f.Source.Kind, f.Source.From)
	}
	slices.SortStableFunc(rows, func(a, b macos.Facts) int {
		return cmp.Or(strings.Compare(key(a), key(b)), strings.Compare(b.Source.Date, a.Source.Date))
	})
	return slices.CompactFunc(rows, func(a, b macos.Facts) bool { return key(a) == key(b) })
}

// fact is one probed fact: its value, or the error asking for it gave.
type fact struct {
	Value json.RawMessage `json:"value"`
	Error *string         `json:"error"`
}

func tartRow(data []byte) (macos.Facts, bool, error) {
	var doc probed
	if err := json.Unmarshal(data, &doc); err != nil {
		return macos.Facts{}, false, err
	}
	if len(doc.Facts) == 0 || string(doc.Facts) == "null" {
		return macos.Facts{}, false, nil
	}
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(doc.Facts, &sections); err != nil {
		return macos.Facts{}, false, err
	}
	facts := map[string]fact{}
	for name, section := range sections {
		var values map[string]fact
		if json.Unmarshal(section, &values) != nil {
			continue
		}
		for key, value := range values {
			facts[name+"."+key] = value
		}
	}
	text := func(name string) string {
		f, ok := facts[name]
		if !ok || f.Error != nil {
			return ""
		}
		var s string
		if json.Unmarshal(f.Value, &s) != nil {
			return ""
		}
		return s
	}
	row := macos.Facts{
		Architecture: text("base.build_arch"),
		MacOS:        text("shell.sw_vers_productVersion"),
		Xcode:        text("base.xcodeversion"),
		Tools:        cmp.Or(text("base.xcodecltversion"), "none"),
		DeveloperDir: text("base.developer_dir"),
		SDK:          text("base.macosx_sdk_version"),
		Clang:        text("worker.compiler.command_line_tools_version(clang)"),
		Source:       macos.Source{Kind: macos.SourceTart, From: doc.Image, MacPorts: text("base.macports_version")},
	}
	row.Darwin, _ = strconv.Atoi(text("base.os_major"))
	switch doc.Profile {
	case "base":
		row.Profile = macos.ProfileTools
	case "xcode":
		row.Profile = macos.ProfileXcode
	default:
		return macos.Facts{}, false, fmt.Errorf("unknown profile %q", doc.Profile)
	}
	if f, ok := facts["shell.clt_sdks"]; ok && f.Error == nil {
		_ = json.Unmarshal(f.Value, &row.SDKs)
	}
	if f, ok := facts["host_checks.file_exists_usr_lib_libxcselect_dylib"]; ok && f.Error == nil {
		var exists bool
		if json.Unmarshal(f.Value, &exists) == nil {
			row.XCSelect = &exists
		}
	}
	if date, err := time.Parse(time.RFC3339, doc.Date); err == nil {
		row.Source.Date = date.UTC().Format(time.DateOnly)
	}
	return row, true, nil
}

func buildbotRow(builder builderFacts) macos.Facts {
	h := builder.Header
	return macos.Facts{
		Darwin:       h.Darwin,
		Architecture: builder.Architecture,
		// The buildbots carry full Xcode, with the tools on most releases.
		Profile: macos.ProfileXcode,
		MacOS:   h.MacOS,
		Xcode:   h.Xcode,
		Tools:   h.Tools,
		SDK:     h.SDK,
		Source:  macos.Source{Kind: macos.SourceBuildbot, From: fmt.Sprintf("%s build %d", builder.Builder, builder.Build), Date: builder.Started, MacPorts: h.MacPorts},
	}
}

// generations are the tools generation setup installs for each release
// (decision 13): the major version of the tools MacPorts' arm64 builder
// for the release runs, since setup builds arm64 images.
func generations(rows []macos.Facts) []macos.Generation {
	var found []macos.Generation
	for _, row := range rows {
		if row.Source.Kind != macos.SourceBuildbot || row.Architecture != "arm64" || row.Tools == "none" {
			continue
		}
		major, err := strconv.Atoi(strings.SplitN(row.Tools, ".", 2)[0])
		if err != nil {
			continue
		}
		found = append(found, macos.Generation{Darwin: row.Darwin, Tools: major, Source: row.Source})
	}
	slices.SortFunc(found, func(a, b macos.Generation) int { return a.Darwin - b.Darwin })
	return slices.CompactFunc(found, func(a, b macos.Generation) bool { return a.Darwin == b.Darwin })
}
