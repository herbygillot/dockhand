package portedit

import "strings"

const GeneratedBy = "Generated-by: [dockhand](https://github.com/herbygillot/dockhand)"

// Message renders a completely generated contribution, including its attribution.
func (c CommitIntent) Message() string {
	lines := strings.Split(c.Body, "\n")
	body := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != GeneratedBy {
			body = append(body, line)
		}
	}
	parts := []string{strings.TrimSpace(c.Subject)}
	if text := strings.TrimSpace(strings.Join(body, "\n")); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(append(parts, GeneratedBy), "\n\n") + "\n"
}
