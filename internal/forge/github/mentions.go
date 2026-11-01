package github

import (
	"regexp"
	"strings"
)

// GitHub notifies whom a description mentions, "@" and their login, as
// its documentation's "Mentioning people and teams" says
// (docs.github.com, Basic writing and formatting syntax). A login is
// letters, digits, and single hyphens, at most 39 of them, not starting
// with a hyphen; an "@" after a letter or digit is an email address, and
// a team's mention, @org/team, is the organization's team, kept whole.
// What a description holds in code, a fenced block or a backquoted span,
// and in an HTML comment, as MacPorts' template's guidance is, renders as
// no mention and notifies no one.
var (
	mentionPattern = regexp.MustCompile(`(^|[^A-Za-z0-9_@/.\x60-])@([A-Za-z0-9](?:-?[A-Za-z0-9]){0,38})(/[A-Za-z0-9][A-Za-z0-9_-]*)?\b`)
	commentPattern = regexp.MustCompile(`(?s)<!--.*?(-->|$)`)
	spanPattern    = regexp.MustCompile("`[^`\n]*`")
)

// mentions are the logins a description mentions, each once, in the
// order written.
func mentions(body string) []string {
	body = commentPattern.ReplaceAllString(body, " ")
	var found []string
	seen := map[string]bool{}
	fence := ""
	for line := range strings.Lines(body) {
		// A fence opens with three backquotes or tildes and closes with
		// the same.
		trimmed := strings.TrimSpace(line)
		if marker := trimmed[:min(3, len(trimmed))]; marker == "```" || marker == "~~~" {
			switch {
			case fence == "":
				fence = marker
			case fence == marker:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		for _, match := range mentionPattern.FindAllStringSubmatch(spanPattern.ReplaceAllString(line, " "), -1) {
			login := match[2] + match[3]
			if key := strings.ToLower(login); !seen[key] {
				seen[key] = true
				found = append(found, login)
			}
		}
	}
	return found
}
