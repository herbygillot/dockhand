package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
)

// xcodeApp is the Xcode a probed image's developer_dir names.
const xcodeApp = "/Applications/Xcode.app"

// sourceRunner is how a runner's probe names its source.
const sourceRunner = "runner"

// generate builds the facts table from Tart probes, runner probes, and a
// buildbot harvest. Each source row is kept, so the buildbot's stay as a
// cross-check where a Tart image or a runner covers the same release; where
// one source gives a release, architecture, and profile more than once, the
// newest wins. A runner probes every Xcode its image carries, and the table
// keeps one Xcode for each release, architecture, and profile, the
// builder's (runnerRows).
func generate(probeDirs []string, buildbot, out string) error {
	var rows, runners []macos.Facts
	for _, dir := range probeDirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			return err
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			row, ok, err := probeRow(data)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			switch {
			case !ok:
			case row.Source.Kind == macos.SourceRunner:
				runners = append(runners, row)
			default:
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
	rows = append(rows, runnerRows(runners, rows, os.Stderr)...)
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

// runnerRows are the runner rows the table keeps: of each release and
// architecture's Xcodes, the one its buildbot runs, and the Command Line
// Tools selected, which Base reads as no Xcode though Xcode stays on disk.
// Every other Xcode is reported, not kept, until the table's lookup can
// choose among several (plan/facts-table-xcode-key-2026-11-05.md).
func runnerRows(runners, others []macos.Facts, report io.Writer) []macos.Facts {
	builders := map[string]string{}
	for _, row := range others {
		if row.Source.Kind == macos.SourceBuildbot && row.Profile == macos.ProfileXcode {
			builders[fmt.Sprintf("%d %s", row.Darwin, row.Architecture)] = row.Xcode
		}
	}
	var kept []macos.Facts
	for _, row := range runners {
		builder, known := builders[fmt.Sprintf("%d %s", row.Darwin, row.Architecture)]
		switch {
		case row.Profile == macos.ProfileTools && row.Xcode == "none", row.Profile == macos.ProfileXcode && row.Xcode == builder:
			kept = append(kept, row)
			fmt.Fprintf(report, "kept:     darwin %d %s %s: Xcode %s, tools %s, clang %s (%s)\n", row.Darwin, row.Architecture, row.Profile, row.Xcode, row.Tools, row.Clang, row.Source.From)
		case !known:
			fmt.Fprintf(report, "reported: darwin %d %s %s: Xcode %s, tools %s, clang %s (%s): no builder row to choose by\n", row.Darwin, row.Architecture, row.Profile, row.Xcode, row.Tools, row.Clang, row.Source.From)
		default:
			fmt.Fprintf(report, "reported: darwin %d %s %s: Xcode %s, tools %s, clang %s (%s): the builder runs Xcode %s\n", row.Darwin, row.Architecture, row.Profile, row.Xcode, row.Tools, row.Clang, row.Source.From, builder)
		}
	}
	return kept
}

// probeRow is a Tart image's or a runner's probe as a row of the table.
func probeRow(data []byte) (macos.Facts, bool, error) {
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
	if doc.Source == sourceRunner {
		row.Source.Kind, row.Source.From = macos.SourceRunner, runnerFrom(doc)
	}
	row.Darwin, _ = strconv.Atoi(text("base.os_major"))
	switch doc.Profile {
	case "base", "clt":
		row.Profile = macos.ProfileTools
	case "xcode":
		row.Profile = macos.ProfileXcode
	default:
		return macos.Facts{}, false, fmt.Errorf("unknown profile %q", doc.Profile)
	}
	if f, ok := facts["shell.clt_sdks"]; ok && f.Error == nil {
		_ = json.Unmarshal(f.Value, &row.SDKs)
	}
	// Xcode's own SDKs, the probe's listing of each Xcode*.app, for the
	// one Base's developer_dir names: /Applications/Xcode.app on an image,
	// the Xcode selected on a runner.
	if f, ok := facts["shell.xcode_sdks"]; ok && f.Error == nil && row.Profile == macos.ProfileXcode {
		app := xcodeApp
		if dir, ok := strings.CutSuffix(row.DeveloperDir, "/Contents/Developer"); ok {
			app = dir
		}
		var listed []string
		_ = json.Unmarshal(f.Value, &listed)
		for _, entry := range listed {
			if sdk, ok := strings.CutPrefix(entry, app+": "); ok {
				row.XcodeSDKs = append(row.XcodeSDKs, sdk)
			}
		}
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

// runnerFrom names a runner's row: its image, the run, and what was
// selected, "macos-15-intel run 123, Xcode_16.4.app".
func runnerFrom(doc probed) string {
	from := doc.Image
	if run, ok := doc.Host["run"].(string); ok {
		from += " run " + run[strings.LastIndex(run, "/")+1:]
	}
	selected := "Command Line Tools"
	if app, ok := strings.CutSuffix(doc.Developer, "/Contents/Developer"); ok {
		selected = filepath.Base(app)
	}
	return from + ", " + selected
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
