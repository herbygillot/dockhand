// Package commitmsg holds the rules of a MacPorts commit message that more
// than one part of dockhand writes by: the "<port>: <what changed>"
// subject, and dockhand's attribution line. It depends only on the build's
// version, so the editor, tidy, and the pull-request body read one rule.
package commitmsg

import (
	"errors"
	"fmt"
	"strings"

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
