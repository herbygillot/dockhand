package portedit

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/version"
)

// generatedByPrefix opens the trailer every generated contribution commit
// ends with. The legacy prefixes are what earlier builds wrote; they are
// still recognized so a rewritten message carries one trailer, not two.
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

// Message renders a completely generated contribution, including its attribution.
func (c CommitIntent) Message() string {
	lines := strings.Split(c.Body, "\n")
	body := lines[:0]
	for _, line := range lines {
		if !IsAttribution(line) {
			body = append(body, line)
		}
	}
	parts := []string{strings.TrimSpace(c.Subject)}
	if text := strings.TrimSpace(strings.Join(body, "\n")); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(append(parts, GeneratedBy()), "\n\n") + "\n"
}
