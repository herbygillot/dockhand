package portfile

import (
	"errors"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

type checksumPair struct {
	kind  string
	words ChecksumWords
	value syntax.Word
}

// ModernChecksumKinds is the layout MacPorts writes today, in the order
// `port checksum` suggests it.
var ModernChecksumKinds = []string{"rmd160", "sha256", "size"}

// LegacyChecksums reports whether a group written with these algorithms is
// rewritten as a whole: it names md5 or sha1, which MacPorts no longer
// accepts as a sole guarantee, or it lacks sha256. Groups made only of
// current algorithms keep their layout and are edited value by value.
func LegacyChecksums(kinds []string) bool {
	hasSHA256 := false
	for _, kind := range kinds {
		switch kind {
		case "md5", "sha1":
			return true
		case "sha256":
			hasSHA256 = true
		}
	}
	return !hasSHA256
}

// ChecksumWords locates one algorithm and its value as written.
type ChecksumWords struct {
	Kind, Value text.Span
}

func (c Checksum) modern() []string {
	var out []string
	for _, kind := range ModernChecksumKinds {
		value, _ := c.Value(kind)
		out = append(out, kind, value)
	}
	return out
}

// RewriteChecksumGroup replaces the written pairs of one group, first
// algorithm through last value, with the modern layout, keeping the
// group's own spacing: the column its values are aligned in, if any, and
// the line continuation between pairs. A group written as a single pair continues
// onto new lines aligned under its first algorithm. It returns the edit
// and the evaluated words the group will then produce.
func RewriteChecksumGroup(src []byte, pairs []ChecksumWords, sums Checksum) (text.Edit, []string) {
	first, last := pairs[0], pairs[len(pairs)-1]
	span := text.Span{Start: first.Kind.Start, End: last.Value.End}
	// Values aligned in a column wider than the first algorithm needs keep
	// that column; otherwise one space separates each algorithm from its value.
	column := first.Value.Start - first.Kind.Start
	if column <= len(src[first.Kind.Start:first.Kind.End])+1 {
		column = 0
	}
	var between string
	if len(pairs) > 1 {
		between = string(src[first.Value.End:pairs[1].Kind.Start])
	} else {
		lineStart := strings.LastIndexByte(string(src[:first.Kind.Start]), '\n') + 1
		indent := []byte(string(src[lineStart:first.Kind.Start]))
		for i, b := range indent {
			if b != '\t' {
				indent[i] = ' '
			}
		}
		between = " \\\n" + string(indent)
	}
	values := sums.modern()
	var out strings.Builder
	for i := 0; i < len(values); i += 2 {
		if i > 0 {
			out.WriteString(between)
		}
		out.WriteString(values[i])
		out.WriteString(strings.Repeat(" ", max(1, column-len(values[i]))))
		out.WriteString(values[i+1])
	}
	return text.Edit{Span: span, New: []byte(out.String())}, values
}

var ErrUnsupported = errors.New("portfile: unsupported source edit")

type Checksum struct {
	Name, SHA256, RMD160 string
	// MD5 and SHA1 are written only into a legacy group kept as written.
	MD5, SHA1 string `json:",omitempty"`
	Size      int64
}

// Value is the checksum's value for one algorithm as a Portfile writes
// it, and false for a word that names no algorithm. The five it knows are
// the checksum vocabulary: the modern three and the two legacy digests.
func (c Checksum) Value(kind string) (string, bool) {
	switch kind {
	case "rmd160":
		return c.RMD160, true
	case "sha256":
		return c.SHA256, true
	case "size":
		return strconv.FormatInt(c.Size, 10), true
	case "md5":
		return c.MD5, true
	case "sha1":
		return c.SHA1, true
	}
	return "", false
}

// IsChecksumKind reports whether the word names a checksum algorithm.
func IsChecksumKind(kind string) bool {
	_, ok := Checksum{}.Value(kind)
	return ok
}
