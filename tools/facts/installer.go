package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
)

// installerDoc is a reading of Xcode and Command Line Tools installers,
// tools/facts/installers: what each one installs, read from the .dmg or
// .xip without installing it or running anything in it. It has no live
// Base view, so it tells versions' facts, never a release's: its rows take
// the release, the macOS, and Base's versions from the builder whose Xcode
// and tools they are (installerRows).
type installerDoc struct {
	Date       string      `json:"date"`
	Installers []installer `json:"installers"`
}

type installer struct {
	Installer string `json:"installer"`
	// Kind is xcode or clt.
	Kind string `json:"kind"`
	// XcodeVersion and Layout are an Xcode's version and where it puts its
	// developer directory: /Developer through Xcode 4.2, else
	// Xcode.app/Contents/Developer.
	XcodeVersion string `json:"xcode_version"`
	Layout       string `json:"developer_dir_layout"`
	XcodeSDKs    []sdk  `json:"xcode_sdks"`
	// ToolsVersion and DeveloperDir are the Command Line Tools'.
	ToolsVersion string `json:"tools_version"`
	DeveloperDir string `json:"developer_dir"`
	SDKs         []sdk  `json:"sdks"`
	Clang        struct {
		Clang string `json:"clang"`
	} `json:"clang"`
}

// sdk is one entry of an SDKs directory, "MacOSX10.13.sdk -> MacOSX.sdk"
// for a link, as the table writes it.
type sdk struct {
	Entry string `json:"entry"`
}

// entries are an SDKs directory's entries, sorted as a probe's listing is.
func entries(sdks []sdk) []string {
	var names []string
	for _, s := range sdks {
		names = append(names, s.Entry)
	}
	slices.Sort(names)
	return names
}

func readInstallers(paths []string) ([]installer, string, error) {
	var all []installer
	date := ""
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
		var doc installerDoc
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, "", fmt.Errorf("%s: %w", path, err)
		}
		if doc.Date == "" {
			return nil, "", fmt.Errorf("%s: the reading's date is required", path)
		}
		all = append(all, doc.Installers...)
		date = max(date, doc.Date)
	}
	return all, date, nil
}

// installerRows are the rows the installers give each buildbot's
// toolchain: its Xcode's SDKs and, where the builder runs the Command Line
// Tools, theirs, with the tools' clang, or Xcode's where there are none,
// which is the clang Base's compiler selection reads
// (compiler.command_line_tools_version); and the tools alone as the
// Command Line Tools profile. The tools are matched by version
// (toolsVersion), as a runner's are. Where two installers of one release
// of the tools are for two macOS releases, the one named for the
// builder's is taken; their SDKs and clang are the same. An Xcode for
// Apple silicon alone stands for no Intel builder.
func installerRows(installers []installer, date string, others []macos.Facts, report io.Writer) []macos.Facts {
	var rows []macos.Facts
	for _, builder := range others {
		if builder.Source.Kind != macos.SourceBuildbot || builder.Profile != macos.ProfileXcode {
			continue
		}
		var xcode, tools *installer
		for i := range installers {
			found := &installers[i]
			switch {
			case strings.Contains(found.Installer, "Apple_silicon") && builder.Architecture != "arm64":
			case found.Kind == "xcode" && builder.Xcode != "none" && found.XcodeVersion == builder.Xcode:
				xcode = cmp.Or(xcode, found)
			case found.Kind == "clt" && builder.Tools != "none" && toolsVersion(found.ToolsVersion) == toolsVersion(builder.Tools):
				if tools == nil || forRelease(found, builder.MacOS) && !forRelease(tools, builder.MacOS) {
					tools = found
				}
			}
		}
		said := fmt.Sprintf("darwin %d %s: Xcode %s, tools %s", builder.Darwin, builder.Architecture, builder.Xcode, builder.Tools)
		if xcode == nil && tools == nil {
			fmt.Fprintf(report, "reported: %s: no installer read\n", said)
			continue
		}
		row := macos.Facts{Darwin: builder.Darwin, Architecture: builder.Architecture, MacOS: builder.MacOS, DarwinVersion: builder.DarwinVersion, Xcode: builder.Xcode, Tools: builder.Tools, SDK: builder.SDK,
			Source: macos.Source{Kind: macos.SourceInstaller, Date: date}}
		var from []string
		if tools != nil {
			row.SDKs, row.Clang = entries(tools.SDKs), tools.Clang.Clang
			from = append(from, tools.Installer)
		}
		if xcode != nil {
			xrow := row
			xrow.Profile, xrow.XcodeSDKs, xrow.XcodeClang = macos.ProfileXcode, entries(xcode.XcodeSDKs), xcode.Clang.Clang
			xrow.DeveloperDir = "/Applications/Xcode.app/Contents/Developer"
			if xcode.Layout == "/Developer" {
				xrow.DeveloperDir = "/Developer"
			}
			if builder.Tools == "none" {
				xrow.Clang = xcode.Clang.Clang
			}
			xrow.Source.From = strings.Join(append([]string{xcode.Installer}, from...), ", ")
			rows = append(rows, xrow)
			fmt.Fprintf(report, "kept:     %s xcode (%s)\n", said, xrow.Source.From)
		}
		if tools != nil {
			trow := row
			trow.Profile, trow.Xcode, trow.DeveloperDir = macos.ProfileTools, "none", cmp.Or(tools.DeveloperDir, "/Library/Developer/CommandLineTools")
			trow.Source.From = tools.Installer
			rows = append(rows, trow)
			fmt.Fprintf(report, "kept:     %s clt (%s)\n", said, trow.Source.From)
		}
	}
	return rows
}

// forRelease is whether an installer of the Command Line Tools is named for
// a macOS release, Command_Line_Tools_macOS_10.13_for_Xcode_9.2.dmg for
// 10.13.6.
func forRelease(tools *installer, macOS string) bool {
	parts := strings.SplitN(macOS, ".", 3)
	if len(parts) < 2 {
		return false
	}
	return strings.Contains(tools.Installer, "_"+parts[0]+"."+parts[1]+"_")
}
