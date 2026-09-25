// Package commitrules checks commits against what MacPorts asks of them
// (Design v3 §8): subjects that name the port, short and specific; bodies
// wrapped at 72; tickets as full URLs; no follow-up or merge commits; and
// a revision reset to 0 when the version changes. tidy writes by these
// rules, and submit's preview applies them. The rules that need MacPorts
// (lint, version order) or GitHub (other open pull requests, maintainers)
// are not here.
package commitrules

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Severity is how much a finding matters.
type Severity string

const (
	// Error findings are what reviewers ask to have fixed: ✗.
	Error Severity = "error"
	// Warning findings are worth a look: !.
	Warning Severity = "warning"
)

// Mark is the finding's symbol on screen.
func (s Severity) Mark() string {
	if s == Error {
		return "✗"
	}
	return "!"
}

// Finding is one rule a commit or a Portfile breaks.
type Finding struct {
	// Code names the rule, for dockhand explain.
	Code     string
	Severity Severity
	// Commit is the commit found at fault; empty for a Portfile finding.
	Commit string
	// Where is a file and line, for a Portfile finding.
	Where   string
	Message string
}

func (f Finding) String() string {
	at := f.Where
	if f.Commit != "" {
		at = "commit " + short(f.Commit)
	}
	if at == "" {
		return f.Severity.Mark() + " " + f.Message
	}
	return fmt.Sprintf("%s %s: %s", f.Severity.Mark(), at, f.Message)
}

// Errors reports whether any finding is an error.
func Errors(findings []Finding) bool {
	return slices.ContainsFunc(findings, func(f Finding) bool { return f.Severity == Error })
}

// Commit is what the rules read of one commit.
type Commit struct {
	ID      string
	Message string
	Merge   bool
	// Ports are the names of the port directories the commit changes.
	Ports []string
}

// MaxSubject is the longest subject the rules let pass without a warning;
// the wiki asks for 50 to 55.
const MaxSubject = 60

// MaxBody is the width a body wraps at.
const MaxBody = 72

var (
	vague = []string{"update", "updates", "update to latest", "fix", "fixes", "fixed", "wip", "changes", "misc"}
	// followUp matches the subjects of commits that correct an earlier one
	// rather than make a change of their own.
	followUp   = regexp.MustCompile(`(?i)^(fixup!|squash!|amend!)|\b(oops|typo|address(ed)? (review|comments|feedback)|review (comments|feedback)|fix (checksums?|the build|lint|previous|last commit|revision))\b`)
	bareTicket = regexp.MustCompile(`(^|[\s(])#(\d{4,})\b`)
	longURL    = regexp.MustCompile(`^\S*https?://\S+$`)
)

// CheckCommits applies the message rules to a branch's commits, oldest
// first.
func CheckCommits(commits []Commit) []Finding {
	var findings []Finding
	seen := map[string]bool{}
	for i, commit := range commits {
		add := func(code string, severity Severity, format string, args ...any) {
			findings = append(findings, Finding{Code: code, Severity: severity, Commit: commit.ID, Message: fmt.Sprintf(format, args...)})
		}
		if commit.Merge {
			add("merge", Error, "is a merge; rebase instead")
		}
		subject, body, _ := strings.Cut(strings.TrimRight(commit.Message, "\n"), "\n")
		subject = strings.TrimSpace(subject)
		named, rest, ok := strings.Cut(subject, ":")
		if len(commit.Ports) > 0 && (!ok || !namesPort(named, commit.Ports)) {
			add("subject-port", Error, "subject %q should start with the port it changes: %q", subject, commit.Ports[0]+": …")
		}
		if ok && slices.Contains(vague, strings.ToLower(strings.TrimSpace(rest))) {
			add("subject-vague", Warning, "subject %q should say what changed, such as \"update to 1.8.1\"", subject)
		}
		if n := utf8.RuneCountInString(subject); n > MaxSubject {
			add("subject-length", Warning, "subject is %d characters; keep it to %d (the wiki asks for 50–55)", n, MaxSubject)
		}
		for _, line := range strings.Split(strings.TrimPrefix(body, "\n"), "\n") {
			if utf8.RuneCountInString(line) > MaxBody && !longURL.MatchString(strings.TrimSpace(line)) && !strings.Contains(line, "://") {
				add("body-wrap", Warning, "body has lines over %d characters", MaxBody)
				break
			}
		}
		if m := bareTicket.FindStringSubmatch(commit.Message); m != nil {
			add("ticket-url", Warning, "cites #%s; cite tickets as full URLs, such as Closes: https://trac.macports.org/ticket/%s", m[2], m[2])
		}
		if i > 0 && followUp.MatchString(subject) && slices.ContainsFunc(commit.Ports, func(p string) bool { return seen[p] }) {
			add("follow-up", Error, "%q corrects an earlier commit; squash it into that one (dockhand tidy)", subject)
		}
		for _, port := range commit.Ports {
			seen[port] = true
		}
	}
	return findings
}

// namesPort reports whether a subject's prefix, "jq" or "jq, jq-devel",
// names one of the ports.
func namesPort(prefix string, ports []string) bool {
	for name := range strings.SplitSeq(prefix, ",") {
		if slices.Contains(ports, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func short(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

// Portfile is one Portfile as a branch changes it.
type Portfile struct {
	Path          string
	Before, After string
}

var (
	versionLine  = regexp.MustCompile(`(?m)^\s*version\s+(\S+)`)
	setupLine    = regexp.MustCompile(`(?m)^\s*(?:github|gitlab|bitbucket|codeberg|sourcehut)\.setup\s+\S+\s+\S+\s+(\S+)`)
	revisionLine = regexp.MustCompile(`(?m)^\s*revision\s+(\S+)`)
)

// CheckPortfiles applies the content rules to the Portfiles a branch
// changes. It reads only the Portfile's first version and revision, the
// port's own; subports that declare their own are not checked.
func CheckPortfiles(portfiles []Portfile) []Finding {
	var findings []Finding
	for _, p := range portfiles {
		if p.Before == "" || p.After == "" {
			continue
		}
		before, after := version(p.Before), version(p.After)
		if before == "" || after == "" || before == after {
			continue
		}
		if m := revisionLine.FindStringSubmatchIndex(p.After); m != nil {
			value := p.After[m[2]:m[3]]
			if value != "0" {
				line := strings.Count(p.After[:m[2]], "\n") + 1
				findings = append(findings, Finding{Code: "revision-after-update", Severity: Error, Where: fmt.Sprintf("%s:%d", p.Path, line),
					Message: fmt.Sprintf("revision is %s after a version update; MacPorts expects 0", value)})
			}
		}
	}
	return findings
}

// Version is the version a Portfile declares for itself, from its version
// line or its forge setup line; empty when neither is literal enough to read.
func Version(portfile string) string { return version(portfile) }

func version(portfile string) string {
	if m := setupLine.FindStringSubmatch(portfile); m != nil {
		return m[1]
	}
	if m := versionLine.FindStringSubmatch(portfile); m != nil {
		return m[1]
	}
	return ""
}
