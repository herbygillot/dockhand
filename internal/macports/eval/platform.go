package eval

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports/info"
)

// platformOverrides renders the macports:: variable overrides for a
// platform frame, mirroring base's own portindex -p handling verbatim
// (src/port/portindex.tcl) — including the macosx special case, whose
// cxx_stdlib rule is copied as written because its semantics are base's to
// define, not ours.
func platformOverrides(p info.Platform) string {
	osPlatform := p.OS
	extra := ""
	if osPlatform == "macosx" {
		cxx := "libc++"
		if p.Major < 10 {
			cxx = "libstdc++"
		}
		extra = fmt.Sprintf(" os_subplatform macosx cxx_stdlib %s", cxx)
		osPlatform = "darwin"
	}
	return fmt.Sprintf(
		"macports::override_vars {os_platform %s os_major %d os_version %d.0.0 os_arch %s%s}\n",
		osPlatform, p.Major, p.Major, p.Arch, extra)
}
