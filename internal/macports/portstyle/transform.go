package portstyle

import (
	"strconv"
	"strings"
)

// perl5ConvertVersion is a faithful port of perl5_convert_version from the
// perl5 PortGroup (perl5-1.0.tcl): the transform perl5.setup applies to its
// version argument before it becomes the port's version. CPAN's single-dot
// versions are fixed-point decimals — 1.23 means 1.230.0 — and the group
// converts them to dotted decimal so vercmp orders them correctly.
//
// Semantics are copied as written, warts included: fractional digits are
// chunked in threes, right-padded with zeros, parsed as leading decimal
// digits the way Tcl's [scan %u] does (so a CPAN dev-release underscore
// silently ends a chunk: 3.45_01 becomes 3.45.10), with a minimum of two
// fractional components. A v prefix is stripped; zero-dot and multi-dot
// versions pass through.
func perl5ConvertVersion(vers string) string {
	rest := strings.TrimPrefix(vers, "v")
	first := strings.IndexByte(rest, '.')
	if first == -1 || strings.IndexByte(rest[first+1:], '.') != -1 {
		return rest
	}
	ret := rest[:first]
	frac := rest[first+1:]
	for i := 0; i < len(frac) || i < 6; i += 3 {
		sub := ""
		if i < len(frac) {
			sub = frac[i:min(i+3, len(frac))]
		}
		for len(sub) < 3 {
			sub += "0"
		}
		j := 0
		for j < len(sub) && sub[j] >= '0' && sub[j] <= '9' {
			j++
		}
		// When no digits parse, Tcl's inline [scan $sub %u] yields a list
		// of one empty element, whose string form is literally {} — so a
		// chunk led by an underscore produces a {} component in the
		// version. Absurd, and faithfully reproduced: the differential
		// test holds this port to the oracle's behavior, warts included.
		comp := "{}"
		if j > 0 {
			n, _ := strconv.ParseUint(sub[:j], 10, 64)
			comp = strconv.FormatUint(n, 10)
		}
		ret += "." + comp
	}
	return ret
}
