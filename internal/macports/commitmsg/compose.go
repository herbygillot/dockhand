package commitmsg

import (
	"regexp"
	"slices"
	"strings"
)

var trailerLine = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*: \S`)

// Body is a message's text after its subject, less its trailer paragraph.
func Body(message string) string {
	_, rest, _ := strings.Cut(strings.TrimSpace(message), "\n")
	text, _ := SplitTrailers(strings.TrimSpace(rest))
	return text
}

// SplitTrailers separates a body's final paragraph when every line of it
// is a trailer.
func SplitTrailers(text string) (string, []string) {
	paragraphs := strings.Split(text, "\n\n")
	last := strings.Split(strings.TrimSpace(paragraphs[len(paragraphs)-1]), "\n")
	for _, line := range last {
		if !trailerLine.MatchString(line) {
			return text, nil
		}
	}
	return strings.TrimSpace(strings.Join(paragraphs[:len(paragraphs)-1], "\n\n")), last
}

// Compose writes a commit's message as tidy proposes it: a subject, a
// body, and the trailers the combined commits' messages carried, Closes:,
// then See:, then the others, then dockhand's Generated-By: when dockhand
// wrote the edit, an earlier one left out. It's
// tidy's composer, here beside the rules it writes to, where engine/tidy.go
// held it (the code-organization review's finding 9).
func Compose(subject, text string, combined []string, generated bool) string {
	var closes, see, other []string
	add := func(list *[]string, line string) {
		if !slices.Contains(*list, line) {
			*list = append(*list, line)
		}
	}
	// The text's own trailer paragraph joins the combined commits', each
	// line once: tidy --squash --message given an Assisted-by: its commit
	// carried wrote it twice, the message's as body (the gnome-voice
	// session's note 2, 2026-10-24).
	text, given := SplitTrailers(strings.TrimSpace(text))
	lists := [][]string{given}
	for _, message := range combined {
		_, rest, _ := strings.Cut(strings.TrimSpace(message), "\n")
		_, trailers := SplitTrailers(strings.TrimSpace(rest))
		lists = append(lists, trailers)
	}
	for _, trailers := range lists {
		for _, line := range trailers {
			switch {
			case IsAttribution(line):
			case strings.HasPrefix(line, "Closes:"):
				add(&closes, line)
			case strings.HasPrefix(line, "See:"):
				add(&see, line)
			default:
				add(&other, line)
			}
		}
	}
	trailers := slices.Concat(closes, see, other)
	if generated {
		trailers = append(trailers, GeneratedBy())
	}
	parts := []string{strings.TrimSpace(subject)}
	if text = strings.TrimSpace(text); text != "" {
		parts = append(parts, text)
	}
	if len(trailers) > 0 {
		parts = append(parts, strings.Join(trailers, "\n"))
	}
	return strings.Join(parts, "\n\n") + "\n"
}
