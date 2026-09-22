package macports

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
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
func PlatformVariables(platform record.Platform) (string, error) {
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
