package macports

// VerCmp compares two version strings under MacPorts' own ordering:
// negative when a < b, zero when equal, positive when a > b.
//
// This is a faithful port of base's pextlib vercomp.c — RPM-derived
// segment rules, byte-wise ASCII classification, quirks included (a
// digit segment always beats a non-digit one; versions differing only
// in separators are equal; a trailing alpha segment against an
// exhausted string can compare equal). Version ordering is base's
// semantics or it is wrong, so the port is pinned by a differential
// test against the oracle (portfetch's vercmp op) rather than by our
// own expectations.
func VerCmp(a, b string) int {
	if a == b {
		return 0
	}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		// Skip all non-alphanumeric characters.
		for i < len(a) && !isAlnum(a[i]) {
			i++
		}
		for j < len(b) && !isAlnum(b[j]) {
			j++
		}
		ca, cb := byteAt(a, i), byteAt(b, j)

		// A digit segment in a beats whatever b has that is not one
		// (RPM's redhat-compatibility rule), and mixed segment types
		// order the other way.
		if !isDigit(cb) && isDigit(ca) {
			return 1
		}
		if (isDigit(ca) && isAlpha(cb)) || (isAlpha(ca) && isDigit(cb)) {
			return -1
		}

		// Find each side's segment: a run of entirely alphabetic or
		// entirely numeric characters.
		ei, ej := i, j
		if isAlpha(ca) {
			for ei < len(a) && isAlpha(a[ei]) {
				ei++
			}
			for ej < len(b) && isAlpha(b[ej]) {
				ej++
			}
		} else {
			countA, countB := 0, 0
			for ei < len(a) && isDigit(a[ei]) {
				countA++
				ei++
			}
			for ej < len(b) && isDigit(b[ej]) {
				countB++
				ej++
			}
			// Leading zeros do not count toward a longer number.
			for i < ei && a[i] == '0' {
				i++
				countA--
			}
			for j < ej && b[j] == '0' {
				j++
				countB--
			}
			if countA > countB {
				return 1
			}
			if countB > countA {
				return -1
			}
		}

		// Compare the segments lexicographically.
		for i < ei && j < ej && a[i] == b[j] {
			i++
			j++
		}
		if i < ei && j < ej {
			return int(a[i]) - int(b[j])
		}
		i, j = ei, ej
	}

	// All compared characters identical: equal when both exhausted,
	// else the side with remaining characters is newer.
	switch {
	case i >= len(a) && j >= len(b):
		return 0
	case i < len(a):
		return 1
	default:
		return -1
	}
}

// byteAt returns the byte at i, or 0 past the end — mirroring C's NUL
// terminator, which the original's rules read through.
func byteAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAlnum(c byte) bool { return isDigit(c) || isAlpha(c) }
