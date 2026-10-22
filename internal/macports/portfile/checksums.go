package portfile

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

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
	Kind, Value textedit.Span
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
func RewriteChecksumGroup(src []byte, pairs []ChecksumWords, sums Checksum) (textedit.Edit, []string) {
	first, last := pairs[0], pairs[len(pairs)-1]
	span := textedit.Span{Start: first.Kind.Start, End: last.Value.End}
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
	return textedit.Edit{Span: span, New: []byte(out.String())}, values
}

// ErrUnsupported is an edit dockhand doesn't make, which a person makes by
// hand: a refusal of its kind, which every error wrapping it is.
var ErrUnsupported error = refusal.New(refusal.Unsupported, "portfile: unsupported source edit")

type Checksum struct {
	Name, SHA256, RMD160 string
	// MD5 and SHA1 are written only into a legacy group kept as written.
	MD5, SHA1 string `json:",omitempty"`
	Size      int64
}

// Words is the checksum as a person compares it: sha256, shortened, and
// the size, else rmd160.
func (c Checksum) Words() string {
	short := func(hash string) string {
		if len(hash) <= 10 {
			return hash
		}
		return hash[:4] + "…" + hash[len(hash)-4:]
	}
	var words []string
	switch {
	case c.SHA256 != "":
		words = append(words, "sha256 "+short(c.SHA256))
	case c.RMD160 != "":
		words = append(words, "rmd160 "+short(c.RMD160))
	}
	if c.Size != 0 {
		words = append(words, "size "+prose.Count(int(c.Size)))
	}
	return strings.Join(words, "   ")
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

// ChecksumsBlock is a checksums declaration of these archives, in the
// layout MacPorts' guide gives one: each archive named, then its rmd160,
// sha256, and size, one to a line, continued, aligned under the first.
func ChecksumsBlock(sums []Checksum) string {
	const indent = "                    "
	var lines []string
	for _, sum := range sums {
		lines = append(lines, sum.Name)
		for _, pair := range [][2]string{{"rmd160", sum.RMD160}, {"sha256", sum.SHA256}, {"size", fmt.Sprint(sum.Size)}} {
			lines = append(lines, fmt.Sprintf("%-8s%s", pair[0], pair[1]))
		}
	}
	return "checksums           " + strings.Join(lines, " \\\n"+indent)
}

// RenameChecksumFiles writes a version edit into the archive names a
// checksums declaration lists literally: go-1.26's names its three,
// go1.26.9.src.tar.gz and the two darwin binaries, so moving the version
// alone left them naming the old release, and no declaration was found for
// the new archives (field testing at 1cc87c43). Each literal name word of
// a checksums, checksums-append, or checksums-prepend command, anywhere in
// the Portfile, that writes from has it written as to; algorithms and
// values are left as they are.
func RenameChecksumFiles(src []byte, from, to string) ([]byte, error) {
	if from == "" || from == to {
		return src, nil
	}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: invalid Tcl syntax", ErrUnsupported)
	}
	var edits []textedit.Edit
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		switch name, _ := cmd.Name(src); name {
		case "checksums", "checksums-append", "checksums-prepend":
		default:
			continue
		}
		for i, word := range cmd.Words[1:] {
			value, literal := word.Literal(src)
			// A name is a word that's neither an algorithm nor the value
			// after one.
			if !literal || word.Expand || IsChecksumKind(value) || i > 0 && isKindWord(src, cmd.Words[i]) || !strings.Contains(value, from) {
				continue
			}
			edits = append(edits, textedit.Edit{Span: word.Inner(), New: []byte(strings.ReplaceAll(value, from, to))})
		}
	}
	if len(edits) == 0 {
		return src, nil
	}
	return textedit.Apply(src, edits)
}

// isKindWord reports a word that names a checksum algorithm.
func isKindWord(src []byte, word syntax.Word) bool {
	value, literal := word.Literal(src)
	return literal && IsChecksumKind(value)
}
