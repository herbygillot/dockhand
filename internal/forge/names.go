package forge

import (
	"net/url"
	"strings"
	"unicode"
)

// ValidGitHubRepositoryName reports whether value is a GitHub repository's
// owner/name, each in the characters GitHub allows: letters, digits, '-',
// '_' and '.', neither "." nor "..". github.ValidRepositoryName asks it.
func ValidGitHubRepositoryName(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				return false
			}
		}
	}
	return true
}

// ValidGitLabInstance reports whether a parsed address can be a GitLab
// instance: http or https, a host, and no user, query, or fragment. A
// path names the instance's root below its host.
func ValidGitLabInstance(parsed *url.URL) bool {
	return parsed.Scheme != "" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

// ValidGitLabProjectPath reports whether a name is a GitLab project path:
// two segments or more, none empty, "." or "..", and none holding space,
// a control character, or one of ?#%\.
func ValidGitLabProjectPath(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.IndexFunc(part, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("?#%\\", r)
		}) >= 0 {
			return false
		}
	}
	return true
}
