// Package prose words counts and sizes the same way wherever dockhand
// says them.
package prose

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Count writes a number with its thousands set apart, 2,200.
func Count(n int) string {
	digits := strconv.Itoa(n)
	if n < 0 {
		return "-" + Count(-n)
	}
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}

// Took is how long something took, short enough for a table's cell, to
// the second under an hour and the minute past it: "38s", "4m 12s",
// "1h 3m".
func Took(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d/time.Minute), int(d%time.Minute/time.Second))
	}
	d = d.Round(time.Minute)
	return fmt.Sprintf("%dh %dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
}

// Plural is a count of a noun: "1 port", "2 ports", "3 patches".
func Plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	for _, ending := range []string{"s", "x", "ch", "sh"} {
		if strings.HasSuffix(noun, ending) {
			return fmt.Sprintf("%d %ses", n, noun)
		}
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// Bytes is a size in SI units, as Finder and df -H count them: "820 MB",
// "1.4 GB", "12 KB", "512 bytes". Below ten of a unit it keeps a tenth,
// where the tenth isn't nought: "30 GB", "1 KB" of 1,024 bytes.
func Bytes(n int64) string {
	units := []string{"KB", "MB", "GB", "TB"}
	if n < 1000 && n > -1000 {
		return Plural(int(n), "byte")
	}
	value, unit := float64(n), ""
	for _, next := range units {
		value /= 1000
		unit = next
		if value < 1000 && value > -1000 {
			break
		}
	}
	if value >= 10 || value <= -10 {
		return fmt.Sprintf("%.0f %s", value, unit)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0") + " " + unit
}

// Few is a list as a line can carry it: the whole list where it has at
// most shown items, and else its count and its first few, "18 ports:
// terraform-1.16, terraform-1.17, terraform-1.18 and 15 more", as review
// says a port's dependents (the command-line UX review's §10).
func Few(items []string, shown int, noun string) string {
	return FewJoined(items, shown, noun, ", ")
}

// FewJoined is Few with the items joined by sep, as an order's " → ".
func FewJoined(items []string, shown int, noun, sep string) string {
	if len(items) <= shown {
		return strings.Join(items, sep)
	}
	return fmt.Sprintf("%s: %s and %d more", Plural(len(items), noun), strings.Join(items[:shown], sep), len(items)-shown)
}

// And is words as a sentence lists them: "14", "14 and 15", "13, 14,
// and 15".
func And(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	case 2:
		return words[0] + " and " + words[1]
	}
	return strings.Join(words[:len(words)-1], ", ") + ", and " + words[len(words)-1]
}

// FirstSentence is text's first sentence, up to its first ". ".
func FirstSentence(text string) string {
	if i := strings.Index(text, ". "); i >= 0 {
		return text[:i+1]
	}
	return text
}

// FirstLine is text's first line.
func FirstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

// Wrap breaks text into lines of at most width runes, between words.
func Wrap(text string, width int) string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && len([]rune(line))+1+len([]rune(word)) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	return strings.Join(append(lines, line), "\n")
}

// LastLines are the last n lines of a program's output that say anything,
// trimmed, its progress redrawn with carriage returns counted as lines.
func LastLines(output string, n int) []string {
	var lines []string
	for line := range strings.SplitSeq(strings.ReplaceAll(output, "\r", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines[max(0, len(lines)-n):]
}

// Sentence is text begun as a sentence: its first letter upper case.
func Sentence(text string) string {
	if text == "" {
		return text
	}
	r, size := utf8.DecodeRuneInString(text)
	return string(unicode.ToUpper(r)) + text[size:]
}
