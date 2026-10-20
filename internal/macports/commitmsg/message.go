// Package commitmsg holds the rules of a MacPorts commit message that more
// than one part of dockhand writes by: the "<port>: <what changed>"
// subject, and dockhand's attribution line. It depends only on the build's
// version, so the editor, tidy, and the pull-request body read one rule.
package commitmsg

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildinfo"
)

const generatedByPrefix = "Generated-By: Dockhand "

// GeneratedBy is the trailer naming the build that generated a commit:
// "Generated-By: Dockhand devel+1a2b3c4d5e6f (https://github.com/herbygillot/dockhand)".
// It is plain text; a commit message is not Markdown.
func GeneratedBy() string {
	return generatedByPrefix + buildinfo.Current().Tag() + " (" + buildinfo.ProjectURL + ")"
}

// IsAttribution reports whether a message line is dockhand's trailer.
func IsAttribution(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), generatedByPrefix)
}

// IsAttributionTrailer reports whether a message line attributes the
// commit, rather than saying anything of the change: dockhand's
// Generated-By, or an Assisted-by, Co-authored-by, or Signed-off-by
// trailer, in any case. A reference trailer, as Closes: with a ticket's
// URL, isn't one.
func IsAttributionTrailer(line string) bool {
	if IsAttribution(line) {
		return true
	}
	key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
	if !ok || strings.TrimSpace(value) == "" {
		return false
	}
	switch strings.ToLower(key) {
	case "assisted-by", "co-authored-by", "signed-off-by":
		return true
	}
	return false
}

// Attributed reports whether a commit message carries dockhand's
// attribution line, as tidy writes it on a commit of dockhand's own edits.
func Attributed(message string) bool {
	return slices.ContainsFunc(strings.Split(message, "\n"), IsAttribution)
}

// Build is the build a message's Generated-By names, by its tag:
// "v0.0.0-20260924.0.0.20260928175309-2bbcfdb76480", or
// "devel+1a2b3c4d5e6f.modified". A message without the line names none.
func Build(message string) (string, bool) {
	for _, line := range strings.Split(message, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), generatedByPrefix); ok {
			tag, _, _ := strings.Cut(rest, " ")
			return tag, true
		}
	}
	return "", false
}

// ModifiedBuild reports whether a message's Generated-By names a dockhand
// built from uncommitted source, which nobody else can find.
func ModifiedBuild(message string) bool {
	tag, ok := Build(message)
	return ok && buildinfo.TagModified(tag)
}

// OtherBuilds are the builds messages' attribution lines name other than
// this one, each once, in order: a pre-v3 branch's commit names
// "v0.0.0-20260921…", which a rebase keeps and a commit tidy writes again
// replaces (the flatbuffers, nuspell, zola, and alertmanager run's finding
// 4).
func OtherBuilds(messages []string) []string {
	current := buildinfo.Current().Tag()
	var builds []string
	for _, message := range messages {
		for _, line := range strings.Split(message, "\n") {
			rest, ok := strings.CutPrefix(strings.TrimSpace(line), generatedByPrefix)
			if !ok {
				continue
			}
			build, _, _ := strings.Cut(rest, " ")
			if build != current && !slices.Contains(builds, build) {
				builds = append(builds, build)
			}
		}
	}
	return builds
}

// Unchanged reports whether a commit with message had can stand for one
// with message want: they say the same, but perhaps for dockhand's
// attribution line, which names the build that wrote each and so differs
// across builds, where both carry one. The one already written names the
// build that wrote it, which stays true; unless that build was of
// uncommitted source, which nobody can find, and which tidying again is
// meant to replace.
func Unchanged(had, want string) bool {
	if strings.TrimRight(had, "\n") == strings.TrimRight(want, "\n") {
		return true
	}
	if ModifiedBuild(had) || Attributed(had) != Attributed(want) {
		return false
	}
	without := func(message string) string {
		var lines []string
		for _, line := range strings.Split(strings.TrimRight(message, "\n"), "\n") {
			if !IsAttribution(line) {
				lines = append(lines, line)
			}
		}
		return strings.TrimRight(strings.Join(lines, "\n"), "\n")
	}
	return without(had) == without(want)
}

// SubjectPorts are the ports a subject in MacPorts' form names, "a, b:
// what changed": the names before its first colon; none where it has no
// colon. A pull request's title takes the same form.
func SubjectPorts(subject string) []string {
	prefix, _, ok := strings.Cut(subject, ":")
	if !ok {
		return nil
	}
	var ports []string
	for name := range strings.SplitSeq(prefix, ",") {
		if name = strings.TrimSpace(name); name != "" {
			ports = append(ports, name)
		}
	}
	return ports
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
