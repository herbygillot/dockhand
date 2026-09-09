package eval

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/platform"
)

// platformOverrides renders the macports:: variable overrides for a
// platform frame.
//
// IT USED TO MIRROR base's portindex -p HANDLING AND NAME SIX VARIABLES,
// which is right for what portindex does — build an index — and not for
// what dockhand does with a frame, which is decide what a port would
// FETCH and what it would be checked against. base exports fifteen
// platform variables to the Portfile interpreter and a port may branch
// on any of them.
//
// Measured, before this was widened: two frames differing only in
// os_arch produced one difference between them — ${os.arch} — while
// build_arch, configure.build_arch and both universal_archs lists stayed
// at the HOST's values in each. A frame naming an older release
// simultaneously reported that release's os_major and this machine's
// macos_version, macosx_deployment_target and macosx_sdk_version. The
// frame contradicted itself, and said so to any Portfile that asked.
//
// THE CAUSE IS ORDER. base computes build_arch, universal_archs and the
// macos_version family inside mportinit, from os_major, os_arch and live
// sysctls; override_vars runs after mportinit, so overriding os_arch
// never recomputes anything derived from it. A derived value does not
// follow the variable it was derived from. So this computes each of them
// for the frame, using base's own rule, and overrides it directly.
//
// NOTHING HERE ASKS THE HOST. base consults sysctl for two questions —
// whether a Mac is running under Rosetta, and whether it is 64-bit
// capable — and a frame that answered either by looking at this machine
// would be simulating a platform while describing another. Both are
// resolved from the frame instead; see buildArch.
//
// WHAT IT STILL CANNOT SAY is left alone rather than guessed:
// macosx_sdk_version and xcodeversion describe the Xcode installed here
// and no override can make them true of somewhere else, and os_minor has
// no honest value because a platform.Release is a whole release and not
// a point version of one. A caller that needs those must refuse rather
// than believe them.
func platformOverrides(p info.Platform) string {
	var b strings.Builder
	set := func(name, value string) {
		fmt.Fprintf(&b, " %s %s", name, value)
	}

	osPlatform := p.OS
	b.WriteString("macports::override_vars {")
	if osPlatform == "macosx" {
		osPlatform = "darwin"
	}
	set("os_platform", osPlatform)
	fmt.Fprintf(&b, " os_major %d os_version %d.0.0", p.Major, p.Major)
	set("os_arch", p.Arch)

	if p.OS == "macosx" {
		set("os_subplatform", "macosx")
		// base's own rule, copied as written because its semantics are
		// base's to define.
		cxx := "libc++"
		if p.Major < 10 {
			cxx = "libstdc++"
		}
		set("cxx_stdlib", cxx)
		set("build_arch", buildArch(p))
		set("universal_archs", "{"+strings.Join(universalArchs(p.Major), " ")+"}")

		// The macOS version family. platform.Release.Product is already
		// exactly what base calls macos_version_major — the major alone
		// from Big Sur on, "10.x" before it — so the whole family falls
		// out of the release table with no second source of truth.
		//
		// macos_version is set to the release rather than to a point
		// version: which point release a frame means is not a thing a
		// frame knows, and inventing one would be the fabricated tail
		// this widening exists to remove.
		if r, ok := platform.ByDarwin(p.Major); ok {
			set("macos_version", r.Product)
			set("macos_version_major", r.Product)
			set("macosx_version", r.Product)
			set("macosx_deployment_target", deploymentTarget(r.Product))
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// buildArch is base's default build architecture for a frame, from
// macports.tcl's own cascade.
//
// THE TWO SYSCTLS ARE ANSWERED FROM THE FRAME. base asks
// sysctl.proc_translated to catch an arm Mac running the tools under
// Rosetta — a fact about a process on THIS machine, which for a
// simulated frame is answered by what the frame says its architecture
// is. And it asks hw.cpu64bit_capable for the 10.6-through-10.15 range;
// every Mac MacPorts still publishes for in that range is 64-bit, and a
// frame cannot measure the exception, so this takes x86_64 and says so
// rather than reaching for the host's answer to a question about
// somewhere else.
func buildArch(p info.Platform) string {
	switch {
	case p.Major >= 20:
		if p.Arch == "arm" {
			return "arm64"
		}
		return "x86_64"
	case p.Major >= 10:
		return "x86_64"
	case p.Arch == "powerpc":
		return "ppc"
	default:
		return "i386"
	}
}

// universalArchs is base's default universal set for a frame, from the
// same cascade.
func universalArchs(major int) []string {
	switch {
	case major >= 20:
		return []string{"arm64", "x86_64"}
	case major >= 19:
		return []string{"x86_64"}
	case major >= 10:
		return []string{"x86_64", "i386"}
	default:
		return []string{"i386", "ppc"}
	}
}

// deploymentTarget is base's rule: the major with ".0" appended from Big
// Sur on, and the "10.x" major unchanged before it.
func deploymentTarget(product string) string {
	if strings.HasPrefix(product, "10.") {
		return product
	}
	return product + ".0"
}
