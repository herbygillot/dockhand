package github

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ValidRepositoryName reports whether value is a repository's owner/name,
// each in the characters GitHub allows.
func ValidRepositoryName(value string) bool {
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

// RemoteRepository is the owner/name of the github.com repository a Git
// remote's URL names, over HTTPS or SSH: https://github.com/owner/name,
// ssh://git@github.com/owner/name, or git@github.com:owner/name, each
// perhaps ending in .git or a slash. It names the repository exactly:
// nothing beneath it, no query, and no credentials in an HTTPS address.
func RemoteRepository(remote string) (string, error) {
	var name string
	if rest, ok := strings.CutPrefix(remote, "git@github.com:"); ok {
		name = rest
	} else {
		u, err := url.Parse(remote)
		if err != nil || !strings.EqualFold(u.Hostname(), "github.com") || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && !(u.Scheme == "ssh" && u.Port() == "22")) {
			return "", errors.New("github: remote must identify a github.com repository")
		}
		if u.Scheme == "https" && u.User != nil || u.Scheme == "ssh" && (u.User == nil || u.User.String() != "git") || u.Scheme != "https" && u.Scheme != "ssh" {
			return "", errors.New("github: unsupported GitHub remote URL")
		}
		name = strings.TrimPrefix(u.Path, "/")
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, "/"), ".git")
	if !ValidRepositoryName(name) {
		return "", errors.New("github: invalid remote repository")
	}
	return name, nil
}

// ErrNotGitHub is an address somewhere other than github.com.
var ErrNotGitHub = errors.New("github: not a github.com address")

// PageRepository is the owner/name of the repository a github.com page
// belongs to, as a person copies its address from a browser: with or
// without its scheme, on www.github.com too, and perhaps a page beneath
// the repository's own, such as https://github.com/owner/name/releases.
func PageRepository(address string) (string, error) {
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || !strings.EqualFold(parsed.Host, "github.com") && !strings.EqualFold(parsed.Host, "www.github.com") {
		return "", ErrNotGitHub
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || !ValidRepositoryName(parts[0]+"/"+strings.TrimSuffix(parts[1], ".git")) {
		return "", fmt.Errorf("%s names no repository", address)
	}
	return parts[0] + "/" + strings.TrimSuffix(parts[1], ".git"), nil
}

// Remote is a repository's Git address on github.com: over SSH,
// git@github.com:owner/name.git, or else HTTPS,
// https://github.com/owner/name.git.
func Remote(repository string, ssh bool) string {
	if ssh {
		return "git@github.com:" + repository + ".git"
	}
	return "https://github.com/" + repository + ".git"
}

// PullRequestURL is a pull request's page on github.com.
func PullRequestURL(repository string, number int) string {
	return fmt.Sprintf("https://github.com/%s/pull/%d", repository, number)
}

// PullRequestChecksURL is the page of a pull request's checks on
// github.com.
func PullRequestChecksURL(repository string, number int) string {
	return PullRequestURL(repository, number) + "/checks"
}
