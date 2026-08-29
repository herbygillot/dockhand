package info

import "fmt"

// Platform names an evaluation platform frame: the platform a Portfile is
// evaluated as though running on. The zero value means the host platform.
//
// The vocabulary mirrors base's own cross-platform indexing (portindex -p
// plat_major_arch): OS is "darwin", or "macosx" to engage macOS
// subplatform semantics; Major is the Darwin major version; Arch is the
// architecture ("x86_64", "arm", "i386", "ppc").
type Platform struct {
	OS    string
	Major int
	Arch  string
}

// IsZero reports the host-platform frame.
func (p Platform) IsZero() bool { return p == Platform{} }

// String renders the portindex-style specifier, e.g. "macosx_19_x86_64".
func (p Platform) String() string {
	return fmt.Sprintf("%s_%d_%s", p.OS, p.Major, p.Arch)
}
