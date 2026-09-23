// Package commitmsg composes and rewrites MacPorts commit messages: the
// "<port>: <what changed>" subject, the ticket trailers, and dockhand's
// attribution line. It depends on the records and the build's version and
// on nothing that edits, so the workflow, the publisher, and the editor
// all read one rule.
package commitmsg

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/version"
)

const generatedByPrefix = "Generated-By: Dockhand "

var legacyPrefixes = []string{"Assisted-By: Dockhand ", "Generated-by: "}

// GeneratedBy is the trailer naming the build that generated a commit:
// "Generated-By: Dockhand devel+1a2b3c4d5e6f (https://github.com/herbygillot/dockhand)".
// It is plain text; a commit message is not Markdown.
func GeneratedBy() string {
	return generatedByPrefix + version.Current().Tag() + " (" + version.ProjectURL + ")"
}

// IsAttribution reports whether a message line is dockhand's trailer, in
// its current or a legacy form.
func IsAttribution(line string) bool {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, generatedByPrefix) {
		return true
	}
	for _, prefix := range legacyPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// Subject composes the commit subject MacPorts asks for, "<port>: <what
// changed>". The person writes only what follows the port name, so a
// subject that already carries it is refused rather than doubled.
func Subject(name, subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" || strings.ContainsAny(subject, "\r\n\x00") {
		return "", errors.New("portedit: the commit subject must be one nonempty line")
	}
	if strings.HasPrefix(subject, name+":") {
		return "", fmt.Errorf("portedit: the subject already begins with %q; dockhand writes the port name, so give only what follows it", name+":")
	}
	return name + ": " + subject, nil
}

// Compose renders a subject, an optional body, and the trailer block: the
// references, then the attribution. Attribution and reference lines already
// in the body move into the block, so a message carries each once.
func Compose(subject, body string, references []record.Reference) string {
	var kept, trailers []string
	seen := map[string]bool{}
	cite := func(reference record.Reference) {
		if line := reference.Trailer(); !seen[line] {
			seen[line] = true
			trailers = append(trailers, line)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if IsAttribution(line) {
			continue
		}
		if reference, ok := record.ParseReferenceTrailer(line); ok {
			cite(reference)
			continue
		}
		kept = append(kept, line)
	}
	for _, reference := range references {
		cite(reference)
	}
	parts := []string{strings.TrimSpace(subject)}
	if text := strings.TrimSpace(strings.Join(kept, "\n")); text != "" {
		parts = append(parts, text)
	}
	trailers = append(trailers, GeneratedBy())
	return strings.Join(append(parts, strings.Join(trailers, "\n")), "\n\n") + "\n"
}

// Rewrite replaces an existing contribution message's subject when one is
// given and adds the references it does not already cite, placing them with
// the trailers ahead of dockhand's attribution or, when the message has
// none, as a final paragraph. Nothing else in the message changes.
func Rewrite(message, subject string, references []record.Reference) string {
	lines := strings.Split(strings.TrimRight(message, "\n"), "\n")
	if subject != "" {
		lines[0] = subject
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if reference, ok := record.ParseReferenceTrailer(line); ok {
			seen[reference.Trailer()] = true
		}
	}
	var added []string
	for _, reference := range references {
		if line := reference.Trailer(); !seen[line] {
			seen[line] = true
			added = append(added, line)
		}
	}
	if len(added) > 0 {
		if at := slices.IndexFunc(lines, IsAttribution); at >= 0 {
			lines = slices.Insert(lines, at, added...)
		} else if citesTickets(lastParagraph(lines)) {
			lines = append(lines, added...)
		} else {
			lines = append(append(lines, ""), added...)
		}
	}
	return strings.Join(lines, "\n")
}

// lastParagraph is the run of lines after the final blank line.
func lastParagraph(lines []string) []string {
	start := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			start = i + 1
		}
	}
	return lines[start:]
}

func citesTickets(lines []string) bool {
	return slices.ContainsFunc(lines, func(line string) bool { _, ok := record.ParseReferenceTrailer(line); return ok })
}
