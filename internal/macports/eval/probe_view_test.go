package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// probedFact is one of a probe's answers: Base's value, or the error it
// raised.
type probedFact struct {
	Value json.RawMessage `json:"value"`
	Error *string         `json:"error"`
}

// probeDocument is what a probe of a real Mac recorded (tools/facts,
// probe.tcl): the image and what was selected, and Base's own view, its
// globals after mportinit and its toolchain answers.
type probeDocument struct {
	Image     string `json:"image"`
	Profile   string `json:"profile"`
	Developer string `json:"developer"`
	Facts     struct {
		Base      map[string]probedFact `json:"base"`
		BaseTools map[string]probedFact `json:"base_tools"`
	} `json:"facts"`
}

// probedGlobals are Base's globals a modelled context answers from the
// facts table, which the probe read after mportinit and a Portfile reads
// in the worker: the platform variables, and the toolchain's.
var probedGlobals = []string{
	"os_platform", "os_subplatform", "os_major", "os_arch", "os_version", "macos_version", "macos_version_major", "macosx_version",
	"build_arch", "universal_archs", "macosx_sdk_version", "macosx_deployment_target", "xcodeversion", "xcodecltversion", "developer_dir", "cxx_stdlib",
}

// probedTools are the compilers the probe asked Base's get_tool_path and
// get_compiler_version about.
var probedTools = []string{"clang", "clang++", "gcc", "g++", "cc", "c++", "llvm-gcc-4.2", "gcc-4.2"}

// answer is a probed fact as the worker's words: its value, a list's
// joined as Tcl joins it, or "error: " and Base's message.
func (f probedFact) answer() string {
	if f.Error != nil {
		return "error: " + *f.Error
	}
	var text string
	if json.Unmarshal(f.Value, &text) == nil {
		return text
	}
	var list []string
	if json.Unmarshal(f.Value, &list) == nil {
		return strings.Join(list, " ")
	}
	return string(f.Value)
}

// Each committed probe of a real Mac (tools/facts/probes) is the row of the
// facts table its release, architecture, and profile model, and the
// modelled context of that row answers as Base answered on that Mac: its
// platform variables, os_version and macos_version included, and its
// toolchain's paths and compiler versions. The minor-release gap #105
// found had been there since the model was built, and no test saw it,
// since the tests asserted what dockhand writes, not what Base computed on
// the release (#107). A probe whose row the table doesn't model, such as
// a second Xcode on one Mac, is left out, said in the log.
func TestTheModelAnswersAsEachProbedMacsBase(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "tools", "facts", "probes", "*", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "the committed probes")
	held := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		var probe probeDocument
		require.NoError(t, json.Unmarshal(data, &probe), path)
		if len(probe.Facts.Base) == 0 {
			continue
		}
		platform := model.Platform{OS: "darwin", Version: probe.Facts.Base["os_major"].answer(), Architecture: probe.Facts.Base["build_arch"].answer()}
		developer, selected := model.DeveloperToolsCommandLine, "Command Line Tools"
		if probe.Profile == "xcode" {
			developer = model.DeveloperToolsXcode
			selected = filepath.Base(strings.TrimSuffix(probe.Developer, "/Contents/Developer"))
		}
		tools, err := macports.Toolchain(platform, developer)
		require.NoError(t, err, path)
		if from := probe.Image + ", " + selected; tools.Source.From != from {
			t.Logf("%s: the table models %s %s %s from %s, not this probe's %s", filepath.Base(path), platform.Version, platform.Architecture, developer, tools.Source.From, from)
			continue
		}
		held++
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			var lines []string
			for _, name := range probedGlobals {
				lines = append(lines, fmt.Sprintf("lappend results [expr {[info exists %[1]s] ? [set %[1]s] : {unset}}]", name))
			}
			for _, tool := range probedTools {
				lines = append(lines, fmt.Sprintf("lappend results [get_tool_path %s]", tool))
				lines = append(lines, fmt.Sprintf("lappend results [if {[get_tool_path %[1]s] eq {}} {set _ {error: get_tool_path returned empty}} elseif {[catch {get_compiler_version [get_tool_path %[1]s] $developer_dir} v]} {set _ \"error: $v\"} else {set v}]", tool))
			}
			lines = append(lines, `lappend results [if {[catch {get_compiler_version /Library/Developer/CommandLineTools/usr/bin/clang /Library/Developer/CommandLineTools} v]} {set _ "error: $v"} else {set v}]`)
			got, _ := observedDescription(t, platform, developer, strings.Join(lines, "\n"))
			var want []string
			for _, name := range probedGlobals {
				want = append(want, probe.Facts.Base[name].answer())
			}
			for _, tool := range probedTools {
				want = append(want, probe.Facts.BaseTools["get_tool_path("+tool+")"].answer(), probe.Facts.BaseTools["get_compiler_version("+tool+")"].answer())
			}
			want = append(want, probe.Facts.BaseTools["get_compiler_version(CLT/clang)"].answer())
			labels := append(append([]string{}, probedGlobals...), "tools...")
			require.Equal(t, want, got, "the model's Base view, in the order %v, against %s's", labels, probe.Image)
		})
	}
	require.Positive(t, held, "at least one probe is the row its context models")
}

// The answers a probe recorded are read as the worker's words.
func TestAProbedFactReadsAsTheWorkersWords(t *testing.T) {
	t.Parallel()
	message := "no such compiler"
	require.Equal(t, "14.0", probedFact{Value: json.RawMessage(`"14.0"`)}.answer())
	require.Equal(t, "arm64 x86_64", probedFact{Value: json.RawMessage(`["arm64","x86_64"]`)}.answer())
	require.Equal(t, "error: no such compiler", probedFact{Error: &message}.answer())
	_, errs := syntax.ListValues("arm64 x86_64")
	require.Empty(t, errs)
}
