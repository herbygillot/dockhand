package macports

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

// PlatformVariables is the list of MacPorts variables and values that make
// an interpreter on this host describe another platform, as a Tcl list of
// pairs, the form portindex -p file: reads and override_vars takes. It is
// what a modeled observation and a generated index both hand to MacPorts,
// so one table decides what a platform looks like from inside a Portfile.
//
// The values follow MacPorts base: os_arch is what `uname -p` reports, arm
// on Apple silicon and i386 on every Intel machine, while build_arch is
// the build architecture; universal_archs and cxx_stdlib follow the
// release, and the deployment target is the product version, with a minor
// of zero from macOS 11 on.
func PlatformVariables(platform model.Platform) (string, error) {
	major, err := strconv.Atoi(platform.Version)
	if platform.OS != "darwin" || err != nil || major < 8 {
		return "", fmt.Errorf("macports: unsupported modeled platform %s %s %s", platform.OS, platform.Version, platform.Architecture)
	}
	var osArch string
	switch platform.Architecture {
	case "arm64":
		osArch = "arm"
	case "ppc", "ppc64":
		osArch = "powerpc"
	case "x86_64", "i386":
		osArch = "i386"
	default:
		return "", fmt.Errorf("macports: unsupported modeled platform %s %s %s", platform.OS, platform.Version, platform.Architecture)
	}
	product, err := macos.ProductForDarwin(major)
	if err != nil {
		return "", err
	}
	deployment := product
	if major >= 20 {
		deployment += ".0"
	}
	universal := "{i386 ppc}"
	switch {
	case major >= 20:
		universal = "{arm64 x86_64}"
	case major == 19:
		universal = "x86_64"
	case major >= 10:
		universal = "{x86_64 i386}"
	}
	stdlib := "libstdc++"
	if major >= 10 {
		stdlib = "libc++"
	}
	pairs := []string{
		"os_platform", "darwin",
		"os_subplatform", "macosx",
		"os_major", strconv.Itoa(major),
		"os_minor", "0",
		"os_version", strconv.Itoa(major) + ".0.0",
		"os_arch", osArch,
		"build_arch", platform.Architecture,
		"macos_version", product,
		"macos_version_major", product,
		"macosx_version", product,
		"macosx_deployment_target", deployment,
		"universal_archs", universal,
		"cxx_stdlib", stdlib,
	}
	return strings.Join(pairs, " "), nil
}

// CommandLineTools is where the Command Line Tools live on a Mac, the
// developer directory a modelled Mac reports.
const CommandLineTools = "/Library/Developer/CommandLineTools"

// XcodeDeveloper is Xcode's developer directory, where xcode-select points
// on a Mac with Xcode and no Command Line Tools.
const XcodeDeveloper = "/Applications/Xcode.app/Contents/Developer"

// ModelledToolchain is what a modelled context's developer tools are
// (docs/oracle.md, phase 5): the facts table's row for its release and
// architecture in the Command Line Tools profile, dockhand's base images'.
// Where the table has only a buildbot's row, which is Xcode's, it is that
// builder's tools without Xcode, and a release whose builders carry no
// tools is modelled as they are, with Xcode. What a row doesn't tell, an
// image with the same tools package does. A field still empty is one no
// source tells, which the host answers, as it did for everything before
// the table.
type ModelledToolchain struct {
	Darwin int
	// DarwinVersion and MacOS are the release's kernel and product
	// versions as the row's source ran them, 22.6.0 and 13.7.8, Base's
	// os_version and macos_version; empty where the row doesn't say.
	DarwinVersion, MacOS string
	// Xcode, Tools, DeveloperDir, and SDK are Base's xcodeversion,
	// xcodecltversion, developer_dir, and macosx_sdk_version.
	Xcode, Tools, DeveloperDir, SDK string
	// Clang is the tools' clang's build number, and XcodeClang Xcode's
	// own, empty where no source tells.
	Clang, XcodeClang string
	// SDKs are the SDKs in the tools, by name, and XcodeSDKs those in
	// Xcode's macOS platform, empty where no source lists them.
	SDKs, XcodeSDKs []string
	// XCSelect is whether /usr/lib/libxcselect.dylib exists.
	XCSelect bool
	// Source is the row the toolchain came from, and Derived whether it
	// was a buildbot's Xcode row made the tools profile.
	Source  macos.Source
	Derived bool
}

// ErrNoToolchain is a platform the facts table has no row for, such as
// Darwin 8 and 9, which no buildbot builds.
var ErrNoToolchain = errors.New("macports: the facts table has no toolchain")

// Toolchain is a modelled platform's developer tools, from the facts table.
// A context that states them is modelled with them: Xcode, as a Tart
// release with its Xcode image has, or the Command Line Tools alone. One
// that doesn't is modelled as MacPorts' builders are set up, with Xcode
// (D2), and with the Command Line Tools where the table has no Xcode row.
func Toolchain(platform model.Platform, developer model.DeveloperTools) (ModelledToolchain, error) {
	if _, err := PlatformVariables(platform); err != nil {
		return ModelledToolchain{}, err
	}
	darwin, _ := strconv.Atoi(platform.Version)
	table := macos.Table()
	switch developer {
	case model.DeveloperToolsXcode:
		return xcodeToolchain(table, darwin, platform)
	case "":
		if tools, err := xcodeToolchain(table, darwin, platform); !errors.Is(err, ErrNoToolchain) {
			return tools, err
		}
	}
	facts, ok := table.Lookup(darwin, platform.Architecture, macos.ProfileTools)
	derived := false
	if !ok {
		if facts, ok = table.Lookup(darwin, platform.Architecture, macos.ProfileXcode); !ok {
			return ModelledToolchain{}, fmt.Errorf("%w for %s", ErrNoToolchain, macos.Describe(platform))
		}
		derived = true
		if facts.Tools != "none" {
			facts.Xcode, facts.DeveloperDir = "none", CommandLineTools
		}
	}
	if facts.Tools != "none" {
		fill(table, &facts)
	}
	if facts.DeveloperDir == "" {
		facts.DeveloperDir = CommandLineTools
		if facts.Tools == "none" {
			facts.DeveloperDir = XcodeDeveloper
		}
	}
	return modelled(darwin, facts, derived), nil
}

// xcodeToolchain is a platform's tools in the Xcode profile: the facts
// table's Xcode row, a Tart Xcode image's where there is one, else a
// buildbot's. Xcode's developer directory is xcode-select's choice there.
func xcodeToolchain(table macos.FactsTable, darwin int, platform model.Platform) (ModelledToolchain, error) {
	facts, ok := table.Lookup(darwin, platform.Architecture, macos.ProfileXcode)
	if !ok || facts.Xcode == "none" {
		return ModelledToolchain{}, fmt.Errorf("%w with Xcode for %s", ErrNoToolchain, macos.Describe(platform))
	}
	fill(table, &facts)
	if facts.DeveloperDir == "" {
		facts.DeveloperDir = XcodeDeveloper
	}
	return modelled(darwin, facts, false), nil
}

// fill takes what a row doesn't tell from any row that tells it of the
// same version, whatever its source: the tools' clang and SDKs from a row
// with the same tools, and Xcode's SDKs from a row with the same Xcode.
// Each belongs to its version alone, as GitHub's runners, Intel and arm64,
// and the Tart images give them field for field (#85): a buildbot's Darwin
// 23 and 24 x86_64 rows take Xcode 15.4's and 16.4's SDKs from the arm64
// Tart rows (#87). It took them only from a Tart row with both the same.
func fill(table macos.FactsTable, facts *macos.Facts) {
	for _, other := range table.Facts {
		if facts.Tools != "none" && other.Tools == facts.Tools {
			if facts.Clang == "" {
				facts.Clang = other.Clang
			}
			if len(facts.SDKs) == 0 {
				facts.SDKs = other.SDKs
			}
		}
		if facts.Xcode != "none" && other.Xcode == facts.Xcode {
			if len(facts.XcodeSDKs) == 0 {
				facts.XcodeSDKs = other.XcodeSDKs
			}
			if facts.XcodeClang == "" {
				facts.XcodeClang = other.XcodeClang
			}
		}
	}
}

func modelled(darwin int, facts macos.Facts, derived bool) ModelledToolchain {
	// libxcselect.dylib is a file from OS X 10.9 until macOS 11, which moved
	// system libraries into the dyld cache.
	xcselect := darwin >= 13 && darwin < 20
	if facts.XCSelect != nil {
		xcselect = *facts.XCSelect
	}
	return ModelledToolchain{Darwin: darwin, DarwinVersion: facts.DarwinVersion, MacOS: facts.MacOS, Xcode: facts.Xcode, Tools: facts.Tools, DeveloperDir: facts.DeveloperDir, SDK: facts.SDK,
		Clang: facts.Clang, XcodeClang: facts.XcodeClang, SDKs: facts.SDKs, XcodeSDKs: facts.XcodeSDKs, XCSelect: xcselect, Source: facts.Source, Derived: derived}
}

// shims are the programs /usr/bin holds for the developer tools from OS X
// 10.9 on, which Base's get_tool_path finds there; the old compilers it
// also asks for are in none of them.
var shims = []string{"clang", "clang++", "cc", "c++", "gcc", "g++", "cpp"}
var gone = []string{"llvm-gcc-4.2", "llvm-g++-4.2", "gcc-4.2", "g++-4.2"}

// ModelVariables is PlatformVariables for a modelled context: the platform,
// and its developer tools from the facts table (Toolchain). They are Xcode's
// version or none, the tools', the developer directory, and the SDK Base
// asks for. The tools' clang answers through the compiler cache Base
// consults before running a compiler, at /usr/bin/clang and in the tools'
// own directory. /usr/bin's shims answer through the cache get_tool_path
// consults. A host that is not a Mac models every context this way, and a
// Mac every context but its own. A platform the table has no row for is
// described alone, and the host answers for its tools, host-in-model.
func ModelVariables(platform model.Platform, developer model.DeveloperTools) (string, error) {
	pairs, err := PlatformVariables(platform)
	if err != nil {
		return "", err
	}
	tools, err := Toolchain(platform, developer)
	if errors.Is(err, ErrNoToolchain) {
		return pairs, nil
	}
	if err != nil {
		return "", err
	}
	variables := []string{pairs, "developer_dir", tools.DeveloperDir, "xcodeversion", tools.Xcode, "xcodecltversion", tools.Tools}
	// The release's versions are the row's source's, whose minor some
	// Portfiles test: py-numpy's +accelerate wants os.minor 4 or later on
	// Darwin 22, and PlatformVariables' 22.0.0 lost it (#105). A later
	// pair stands over an earlier one.
	if version := tools.DarwinVersion; strings.HasPrefix(version, strconv.Itoa(tools.Darwin)+".") {
		minor, _, _ := strings.Cut(strings.TrimPrefix(version, strconv.Itoa(tools.Darwin)+"."), ".")
		variables = append(variables, "os_version", version, "os_minor", minor)
	}
	if tools.MacOS != "" {
		variables = append(variables, "macos_version", tools.MacOS)
	}
	if tools.SDK != "" {
		variables = append(variables, "macosx_sdk_version", tools.SDK)
	}
	// Both caches are always replaced, emptied where the table doesn't
	// tell, so neither the host's nor another modelled platform's answers.
	// Base asks it under configure.developer_dir, which is the tools'
	// wherever they're installed, Xcode or not: keyed by Xcode's directory
	// alone, a context with Xcode and the tools ran this Mac's clang, and
	// a C++20 port kept Apple's 1403 clang where Base's 1500 minimum turns
	// it down (#106).
	compilers := ""
	if tools.Clang != "" {
		versions := func(usrBin string) string {
			return "/usr/bin/clang " + usrBin + " " + CommandLineTools + "/usr/bin/clang " + tools.Clang
		}
		var entries []string
		if tools.DeveloperDir == CommandLineTools {
			entries = append(entries, CommandLineTools+" {"+versions(tools.Clang)+"}")
		} else {
			// Under Xcode's developer_dir, /usr/bin/clang is Xcode's own,
			// which the table keeps where a source tells it: Sequoia's
			// Xcode 16.4 beside the 26.0 tools answered the tools' (#213).
			entries = append(entries, tools.DeveloperDir+" {"+versions(cmp.Or(tools.XcodeClang, tools.Clang))+"}")
			if tools.Tools != "none" {
				entries = append(entries, CommandLineTools+" {"+versions(tools.Clang)+"}")
			}
		}
		// Without the tools, their clang isn't there, and Base answers
		// it empty: asked for it, an Xcode context on Catalina ran this
		// Mac's (#107).
		if tools.Tools == "none" && tools.DeveloperDir != CommandLineTools {
			entries = append(entries, CommandLineTools+" {"+CommandLineTools+"/usr/bin/clang {}}")
		}
		compilers = "versions {" + strings.Join(entries, " ") + "}"
	}
	variables = append(variables, "compiler_version_cache", "{"+compilers+"}")
	var paths []string
	if tools.Darwin >= 13 && (tools.Tools != "none" || tools.Xcode != "none") {
		for _, tool := range shims {
			paths = append(paths, tool, "/usr/bin/"+tool)
		}
		for _, tool := range gone {
			paths = append(paths, tool, "{}")
		}
	}
	variables = append(variables, "tool_path_cache", "{"+strings.Join(paths, " ")+"}")
	return strings.Join(variables, " "), nil
}

// ToolchainAnswers is what a modelled context's worker answers about the
// developer tools' files, as the Tcl dictionary the evaluator's dispatcher
// reads: whether the tools and Xcode are there, libxcselect, and the
// tools' SDKs, an empty list where no source tells. It is empty for a
// platform the table has no row for.
func ToolchainAnswers(platform model.Platform, developer model.DeveloperTools) (string, error) {
	tools, err := Toolchain(platform, developer)
	if errors.Is(err, ErrNoToolchain) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// Each SDK by name, a link and its target alike, and the links apart,
	// whose targets xcrun names: Xcode's MacOSX.sdk is the directory, and
	// xcrun --sdk macosx says MacOSX15.5.sdk, a link to it.
	names := func(listed []string) (sdks, links []string) {
		for _, sdk := range listed {
			name, target, _ := strings.Cut(sdk, " -> ")
			sdks = append(sdks, name)
			if target != "" {
				sdks = append(sdks, target)
				links = append(links, name, target)
			}
		}
		return sdks, links
	}
	sdks, links := names(tools.SDKs)
	xcodeSDKs, xcodeLinks := names(tools.XcodeSDKs)
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	return strings.Join([]string{
		"darwin", strconv.Itoa(tools.Darwin),
		"tools", flag(tools.Tools != "none"),
		"xcode", flag(tools.Xcode != "none"),
		"xcselect", flag(tools.XCSelect),
		"sdks", "{" + strings.Join(sdks, " ") + "}",
		"links", "{" + strings.Join(links, " ") + "}",
		"xcode_sdks", "{" + strings.Join(xcodeSDKs, " ") + "}",
		"xcode_links", "{" + strings.Join(xcodeLinks, " ") + "}",
		"developer_dir", "{" + tools.DeveloperDir + "}",
	}, " "), nil
}
