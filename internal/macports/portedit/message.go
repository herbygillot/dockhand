package portedit

import (
	"strings"

	"github.com/herbygillot/dockhand/internal/version"
)

// assistedByPrefix opens the trailer every generated contribution commit
// ends with. legacyGeneratedBy is the trailer earlier builds wrote; it is
// still recognized so a rewritten message carries one trailer, not two.
const (
	assistedByPrefix  = "Assisted-By: Dockhand "
	legacyGeneratedBy = "Generated-by: "
)

// AssistedBy is the trailer naming the build that generated a commit:
// "Assisted-By: Dockhand devel+1a2b3c4d5e6f (https://github.com/herbygillot/dockhand)".
// It is plain text; a commit message is not Markdown.
func AssistedBy() string {
	return assistedByPrefix + version.Current().Tag() + " (" + version.ProjectURL + ")"
}

// IsAttribution reports whether a message line is dockhand's trailer, in
// its current or legacy form.
func IsAttribution(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, assistedByPrefix) || strings.HasPrefix(line, legacyGeneratedBy)
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
	return strings.Join(append(parts, AssistedBy()), "\n\n") + "\n"
}
