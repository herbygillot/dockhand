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

// sourceRunner and sourceHost are how a runner's probe and a host's name
// their source.
const (
	sourceRunner = "runner"
	sourceHost   = "host"
)

// generate builds the facts table from Tart probes, runner and host
// probes, and a buildbot harvest. Each source row is kept, so the
// buildbot's stay as a cross-check where a Tart image, a runner, or a host
// covers the same release; where one source gives a release, architecture,
// and profile more than once, the newest wins. A runner or a host probes
// every Xcode it carries, and the table keeps the one with the builder's
// toolchain, if any (runnerRows).
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
			case row.Source.Kind == macos.SourceRunner || row.Source.Kind == macos.SourceHost:
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

// runnerRows are the runner and host rows the table keeps: those with the
// toolchain the release and architecture's buildbot runs, its Xcode and its
// tools, or its tools alone with the Command Line Tools selected, which
// Base reads as no Xcode though Xcode stays on disk. The tools are matched
// by version (toolsVersion): two builds of one release of the Command Line
// Tools carry the same clang and SDKs. A runner's other Xcodes, and its own
// tools where they aren't the builder's, are reported, not kept, until the
// table's lookup can choose among toolchains
// (plan/facts-table-xcode-key-2026-11-05.md): kept, they would stand for
// the builder with tools it doesn't run.
func runnerRows(runners, others []macos.Facts, report io.Writer) []macos.Facts {
	builders := map[string]macos.Facts{}
	for _, row := range others {
		if row.Source.Kind == macos.SourceBuildbot && row.Profile == macos.ProfileXcode {
			builders[fmt.Sprintf("%d %s", row.Darwin, row.Architecture)] = row
		}
	}
	var kept []macos.Facts
	for _, row := range runners {
		builder, known := builders[fmt.Sprintf("%d %s", row.Darwin, row.Architecture)]
		said := fmt.Sprintf("darwin %d %s %s: Xcode %s, tools %s, clang %s (%s)", row.Darwin, row.Architecture, row.Profile, row.Xcode, row.Tools, row.Clang, row.Source.From)
		switch {
		case !known:
			fmt.Fprintf(report, "reported: %s: no builder row to choose by\n", said)
		case toolsVersion(row.Tools) == toolsVersion(builder.Tools) && (row.Xcode == builder.Xcode || row.Profile == macos.ProfileTools && row.Xcode == "none"):
			kept = append(kept, row)
			fmt.Fprintf(report, "kept:     %s\n", said)
		default:
			fmt.Fprintf(report, "reported: %s: the builder runs Xcode %s, tools %s\n", said, builder.Xcode, builder.Tools)
		}
	}
	return kept
}

// toolsVersion is the Command Line Tools' version without the build stamp
// that ends it: 26.0.0.0.1 for 26.0.0.0.1.1757719676. Apple has shipped one
// release in builds that differ only there, the developer download and
// Software Update's (Sonoma's 15.3, Sequoia's 26.0).
func toolsVersion(tools string) string {
	if strings.Count(tools, ".") < 5 {
		return tools
	}
	return tools[:strings.LastIndex(tools, ".")]
}

// probeRow is a Tart image's, a runner's, or a host's probe as a row of
// the table.
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
	switch doc.Source {
	case sourceRunner:
		row.Source.Kind, row.Source.From = macos.SourceRunner, runnerFrom(doc)
	case sourceHost:
		row.Source.Kind, row.Source.From = macos.SourceHost, runnerFrom(doc)
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

// runnerFrom names a runner's or a host's row: its image, the run, and
// what was selected, "macos-15-intel run 123, Xcode_16.4.app", or
// "aws-mac1-ventura, Xcode.app" for a host, which has no run.
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
