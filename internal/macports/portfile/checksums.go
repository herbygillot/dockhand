package portfile

import (
	"errors"
	"fmt"
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
type checksumGroup struct {
	name  string
	pairs []checksumPair
}

// checksumKind reports whether s is an algorithm MacPorts accepts in a
// checksums declaration, current or legacy.
func checksumKind(s string) bool {
	return s == "rmd160" || s == "sha256" || s == "size" || s == "md5" || s == "sha1"
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
	values := map[string]string{"rmd160": c.RMD160, "sha256": c.SHA256, "size": strconv.FormatInt(c.Size, 10)}
	var out []string
	for _, kind := range ModernChecksumKinds {
		out = append(out, kind, values[kind])
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

func checksumGroups(src []byte, evaluated string) ([]checksumGroup, error) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: invalid checksum syntax", ErrUnsupported)
	}
	var commands []syntax.Command
	for _, item := range script.Items {
		if cmd, ok := item.(syntax.Command); ok {
			if name, _ := cmd.Name(src); name == "checksums" {
				commands = append(commands, cmd)
			}
		}
	}
	if len(commands) != 1 {
		return nil, fmt.Errorf("%w: expected one checksums command", ErrUnsupported)
	}
	words := commands[0].Words[1:]
	values, errs := syntax.ListValues(evaluated)
	if len(errs) > 0 || len(words) == 0 || len(words) != len(values) {
		return nil, fmt.Errorf("%w: checksum source does not match evaluation", ErrUnsupported)
	}
	var groups []checksumGroup
	names := map[string]bool{}
	for i := 0; i < len(words); {
		group := checksumGroup{}
		if !checksumKind(values[i]) {
			group.name = values[i]
			literal, ok := words[i].Literal(src)
			if group.name == "" || words[i].Expand || (ok && literal != group.name) || names[group.name] {
				return nil, fmt.Errorf("%w: ambiguous checksum filename", ErrUnsupported)
			}
			names[group.name] = true
			i++
		} else if len(groups) > 0 {
			return nil, fmt.Errorf("%w: mixed named and unnamed checksums", ErrUnsupported)
		}
		seen := map[string]bool{}
		for i < len(words) && checksumKind(values[i]) {
			kind, ok := words[i].Literal(src)
			if !ok || kind != values[i] || seen[kind] || i+1 >= len(words) {
				return nil, fmt.Errorf("%w: ambiguous checksum algorithm", ErrUnsupported)
			}
			value, ok := words[i+1].Literal(src)
			if !ok || words[i+1].Expand || value != values[i+1] {
				return nil, fmt.Errorf("%w: calculated or overridden checksums", ErrUnsupported)
			}
			seen[kind] = true
			group.pairs = append(group.pairs, checksumPair{kind, ChecksumWords{Kind: words[i].Span, Value: words[i+1].Span}, words[i+1]})
			i += 2
		}
		if len(group.pairs) == 0 {
			return nil, fmt.Errorf("%w: checksum group without an algorithm", ErrUnsupported)
		}
		groups = append(groups, group)
		if group.name == "" && i < len(words) {
			return nil, fmt.Errorf("%w: mixed named and unnamed checksums", ErrUnsupported)
		}
	}
	return groups, nil
}

func (g checksumGroup) kinds() []string {
	var kinds []string
	for _, pair := range g.pairs {
		kinds = append(kinds, pair.kind)
	}
	return kinds
}

func (g checksumGroup) words() []ChecksumWords {
	var words []ChecksumWords
	for _, pair := range g.pairs {
		words = append(words, pair.words)
	}
	return words
}

// ReplaceChecksums writes the downloads' checksums into the one checksums
// command of src. A group written with current algorithms keeps its layout;
// a legacy group is rewritten as rmd160, sha256, and size.
func ReplaceChecksums(src []byte, evaluated string, downloads ...Checksum) ([]byte, string, error) {
	return ReplaceChecksumsKeeping(src, evaluated, false, downloads...)
}

// ReplaceChecksumsKeeping is ReplaceChecksums with the choice to keep
// legacy groups as written, refreshing every value they name, md5 and sha1
// included.
func ReplaceChecksumsKeeping(src []byte, evaluated string, keepLegacy bool, downloads ...Checksum) ([]byte, string, error) {
	groups, err := checksumGroups(src, evaluated)
	if err != nil {
		return nil, "", err
	}
	byName := map[string]Checksum{}
	for _, d := range downloads {
		if _, ok := byName[d.Name]; ok {
			return nil, "", fmt.Errorf("%w: duplicate distfile %s", ErrUnsupported, d.Name)
		}
		byName[d.Name] = d
	}
	if len(groups) != len(downloads) {
		return nil, "", fmt.Errorf("%w: checksums must cover every source distfile exactly once", ErrUnsupported)
	}
	var edits []text.Edit
	var expected []string
	for _, group := range groups {
		download, ok := byName[group.name]
		if group.name == "" && len(downloads) == 1 {
			download, ok = downloads[0], true
		}
		if !ok {
			return nil, "", fmt.Errorf("%w: no source distfile for checksums %s", ErrUnsupported, group.name)
		}
		if group.name != "" {
			expected = append(expected, group.name)
		}
		if !keepLegacy && LegacyChecksums(group.kinds()) {
			edit, values := RewriteChecksumGroup(src, group.words(), download)
			edits = append(edits, edit)
			expected = append(expected, values...)
			continue
		}
		sums := map[string]string{"rmd160": download.RMD160, "sha256": download.SHA256, "size": strconv.FormatInt(download.Size, 10), "md5": download.MD5, "sha1": download.SHA1}
		for _, pair := range group.pairs {
			edits = append(edits, text.Edit{Span: pair.value.Span, New: []byte(sums[pair.kind])})
			expected = append(expected, pair.kind, sums[pair.kind])
		}
	}
	out, err := text.Apply(src, edits)
	return out, strings.Join(expected, " "), err
}

var ErrUnsupported = errors.New("portfile: unsupported source edit")

type Checksum struct {
	Name, SHA256, RMD160 string
	// MD5 and SHA1 are written only into a legacy group kept as written.
	MD5, SHA1 string
	Size      int64
}
