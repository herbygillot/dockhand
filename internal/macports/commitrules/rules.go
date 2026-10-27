// Package commitrules checks commits against what MacPorts asks of them
// (Design v3 §8): subjects that name the port, short and specific; bodies
// wrapped at 72; tickets as full URLs; no follow-up or merge commits; a
// revision reset to 0 when the version changes, an epoch raised when it
// goes lower, by MacPorts' version order the caller gives (Order), and a
// replaced_by that changes the version, revision, or epoch. tidy writes by
// these rules, and submit's preview applies them. The rules that need
// MacPorts to run (lint) or GitHub (other open pull requests,
// maintainers) are not here.
package commitrules

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
)

// ErrorCodes are the codes of the rules whose findings are errors, which
// hold a submission nobody looks over, for a hold list to name
// (holds.allow).
func ErrorCodes() []string {
	return []string{"merge", "subject-port", "follow-up", "revision-after-update"}
}

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

// String is the finding as a line, ending with its code in brackets, what
// dockhand explain takes.
func (f Finding) String() string {
	at := f.Where
	if f.Commit != "" {
		at = "commit " + model.ObjectID(f.Commit).Short()
	}
	code := ""
	if f.Code != "" {
		code = " [" + f.Code + "]"
	}
	if at == "" {
		return f.Severity.Mark() + " " + f.Message + code
	}
	return fmt.Sprintf("%s %s: %s%s", f.Severity.Mark(), at, f.Message, code)
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
	// Defined are the ports those directories define, subports among
	// them: a subject may name the one it changes, as
	// "terraform-1.16: update to 1.16.5" does, which tidy writes (field
	// testing, 2026-10-02).
	Defined []string
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
	// tracTicket is a Trac ticket cited by its URL, and ticketTrailer a
	// trailer line that cites one, as Closes: or See: does, or a line
	// written as one, in a list or without its colon, "* Fixes: …" and
	// "Fixes …", which already says what the ticket is to the commit (the
	// measure over the Mac's tree).
	tracTicket    = regexp.MustCompile(`https?://trac\.macports\.org/ticket/(\d+)\b`)
	ticketTrailer = regexp.MustCompile(`^(?:[-*]\s+)?(?:[A-Za-z][A-Za-z0-9-]*:|(?i:closes|fixes|fix|see|refs?|references))\s+\S*https?://trac\.macports\.org/ticket/\d+`)
	longURL       = regexp.MustCompile(`^\S*https?://\S+$`)
	// urlTrailer is a trailer whose value ends in a URL, as Closes:
	// https://trac.macports.org/ticket/71234, or Generated-By's "Dockhand
	// <build> (https://github.com/herbygillot/dockhand)", which may run
	// long too.
	urlTrailer = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*:\s.*https?://\S+$`)
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
		if len(commit.Ports) > 0 && (!ok || !namesPort(named, slices.Concat(commit.Ports, commit.Defined))) {
			add("subject-port", Error, "subject %q should start with the port it changes: %q", subject, commit.Ports[0]+": …")
		}
		if ok && slices.Contains(vague, strings.ToLower(strings.TrimSpace(rest))) {
			add("subject-vague", Warning, "subject %q should say what changed, such as \"update to 1.8.1\"", subject)
		}
		if n := utf8.RuneCountInString(subject); n > MaxSubject {
			add("subject-length", Warning, "subject is %d characters; keep it to %d (the wiki asks for 50–55)", n, MaxSubject)
		}
		for _, line := range strings.Split(strings.TrimPrefix(body, "\n"), "\n") {
			// A line that is only a URL, or a trailer whose value is one, may
			// run long; a sentence holding a URL may not, where any line
			// holding "://" was spared (the exiv2 session's notes, 2026-10-21:
			// a 79-character line beside a URL wasn't flagged).
			trimmed := strings.TrimSpace(line)
			if utf8.RuneCountInString(line) > MaxBody && !longURL.MatchString(trimmed) && !urlTrailer.MatchString(trimmed) {
				add("body-wrap", Warning, "body has lines over %d characters", MaxBody)
				break
			}
		}
		if m := bareTicket.FindStringSubmatch(commit.Message); m != nil {
			add("ticket-url", Warning, "cites #%s; cite tickets as full URLs, such as Closes: https://trac.macports.org/ticket/%s", m[2], m[2])
		} else if ticket := untrailedTicket(subject, body); ticket != "" {
			add("ticket-trailer", Warning, "cites Trac ticket %s outside a trailer; if this fixes it, say Closes: https://trac.macports.org/ticket/%s in the body (update's --closes %s writes it), or See: if it doesn't", ticket, ticket, ticket)
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

// untrailedTicket is the first Trac ticket a message cites by its URL in
// its subject, or in its body outside a trailer line such as Closes: or
// See:, where reviewers ask for it as a trailer (the review survey's
// §3, 6 PRs: "The Trac ticket should not be in the short summary",
// #31382); empty where every one it cites a trailer cites too, as
// commons-lang3's sentence did beside its See: lines (the measure over the
// Mac's tree).
func untrailedTicket(subject, body string) string {
	trailed := map[string]bool{}
	var cited []string
	if m := tracTicket.FindStringSubmatch(subject); m != nil {
		cited = append(cited, m[1])
	}
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		for _, m := range tracTicket.FindAllStringSubmatch(line, -1) {
			if ticketTrailer.MatchString(line) {
				trailed[m[1]] = true
			} else {
				cited = append(cited, m[1])
			}
		}
	}
	for _, ticket := range cited {
		if !trailed[ticket] {
			return ticket
		}
	}
	return ""
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

// Portfile is one Portfile as a branch changes it.
type Portfile struct {
	Path          string
	Before, After string
	// Files are the files the change adds under the port's files/, as
	// paths from it, "patch-fix.diff" or "a/b.patch".
	Files []string
}

// Order orders two versions as MacPorts' vercmp does, negative where a is
// lower; false where it couldn't tell.
type Order func(a, b string) (int, bool)

// CheckPortfiles applies the content rules to the Portfiles a branch
// changes. It reads the main port's version, revision, and epoch as the
// Portfile proves them (portfile.DeclaredVersion); subports that declare
// their own are not checked, nor is a version the source can't prove.
// order is vercmp's, for the version-order rule, which a nil order or one
// that can't tell leaves unchecked.
func CheckPortfiles(portfiles []Portfile, order Order) []Finding {
	var findings []Finding
	for _, p := range portfiles {
		if p.Before == "" || p.After == "" {
			continue
		}
		old, next := []byte(p.Before), []byte(p.After)
		before, knownBefore := portfile.DeclaredVersion(old, "")
		after, knownAfter := portfile.DeclaredVersion(next, "")
		epochBefore, epochAfter, knownEpochs := epochs(old, next)
		if knownBefore && knownAfter && before != after {
			if value, line, ok := portfile.DeclaredRevision(next); ok && value != "0" {
				findings = append(findings, Finding{Code: "revision-after-update", Severity: Error, Where: fmt.Sprintf("%s:%d", p.Path, line),
					Message: fmt.Sprintf("revision is %s after a version update; MacPorts expects 0", value)})
			}
			// port upgrade takes a lower version only where the epoch
			// rises (macports::_plan_upgrade): the docs dig's I3.
			if order != nil && knownEpochs && epochAfter <= epochBefore {
				if lower, ok := order(after, before); ok && lower < 0 {
					findings = append(findings, Finding{Code: "version-order", Severity: Error, Where: p.Path,
						Message: fmt.Sprintf("version goes from %s to %s, which MacPorts orders lower, without raising epoch; port upgrade never takes it", before, after)})
				}
			}
		}
		// The revbump classifier's non-ABI half, read from the diff alone
		// (Then's item 9): a runtime dependency changed with neither the
		// version nor the revision raised leaves installed copies on the
		// old one, and a revision raised for a change to build-time
		// dependencies alone rebuilds them for nothing.
		if knownBefore && knownAfter && before == after {
			revisionBefore, _, _ := portfile.DeclaredRevision(old)
			revisionAfter, _, _ := portfile.DeclaredRevision(next)
			changed := changedOptions(p.Before, p.After)
			runtime := slices.ContainsFunc(changed, func(option string) bool { return option == "depends_lib" || option == "depends_run" })
			switch {
			case revisionBefore == revisionAfter && runtime:
				findings = append(findings, Finding{Code: "dependencies-unbumped", Severity: Warning, Where: p.Path,
					Message: "depends_lib or depends_run changes with neither the version nor the revision raised, so installed copies keep the old dependencies until rebuilt; raise the revision"})
			case revisionBefore != revisionAfter && len(changed) > 0 && !slices.ContainsFunc(changed, func(option string) bool {
				return option != "revision" && option != "depends_build" && option != "depends_fetch" && option != "depends_extract"
			}) && slices.ContainsFunc(changed, func(option string) bool { return option != "revision" }):
				findings = append(findings, Finding{Code: "revbump-build-only", Severity: Warning, Where: p.Path,
					Message: "revision is raised for a change to build-time dependencies alone, which installed copies don't need a rebuild for"})
			}
		}
		// An obsoleting replaced_by moves an install to its replacement
		// only through an upgrade, which nothing triggers where the
		// version, revision, and epoch all stand.
		if portfile.Declares(next, "replaced_by") && !portfile.Declares(old, "replaced_by") && knownBefore && knownAfter && before == after && knownEpochs && epochBefore == epochAfter {
			revisionBefore, _, _ := portfile.DeclaredRevision(old)
			revisionAfter, _, _ := portfile.DeclaredRevision(next)
			if revisionBefore == revisionAfter {
				findings = append(findings, Finding{Code: "replaced-by-unchanged", Severity: Error, Where: p.Path,
					Message: "replaced_by is added with no version, revision, or epoch change, so port upgrade never replaces installed copies; raise the revision"})
			}
		}
	}
	return findings
}

// changedOptions are the options whose lines two Portfiles differ in, by
// each logical line's first word, its continuations joined and -append,
// -delete, and -replace taken for the option: depends_lib for
// "depends_lib-append port:zlib". A line that isn't an option's, as a
// comment or a block's brace, is "".
func changedOptions(before, after string) []string {
	lines := func(text string) map[string]int {
		counts := map[string]int{}
		joined := strings.ReplaceAll(text, "\\\n", " ")
		for _, line := range strings.Split(joined, "\n") {
			line = strings.Join(strings.Fields(line), " ")
			if line != "" && !strings.HasPrefix(line, "#") {
				counts[line]++
			}
		}
		return counts
	}
	was, now := lines(before), lines(after)
	var changed []string
	note := func(line string) {
		option, _, _ := strings.Cut(line, " ")
		option, _, _ = strings.Cut(option, "-")
		if !slices.Contains(changed, option) {
			changed = append(changed, option)
		}
	}
	for line, n := range now {
		if was[line] != n {
			note(line)
		}
	}
	for line, n := range was {
		if now[line] != n {
			note(line)
		}
	}
	slices.Sort(changed)
	return changed
}

// epochs are the epochs two Portfiles declare, 0 where none is; false
// where either is one the source can't prove.
func epochs(old, next []byte) (before, after int, ok bool) {
	read := func(src []byte) (int, bool) {
		if !portfile.Declares(src, "epoch") {
			return 0, true
		}
		value, _, ok := portfile.DeclaredEpoch(src)
		if !ok {
			return 0, false
		}
		n, err := strconv.Atoi(value)
		return n, err == nil
	}
	before, knownBefore := read(old)
	after, knownAfter := read(next)
	return before, after, knownBefore && knownAfter
}

// Providers are the other ports that can satisfy a dependency on a port,
// as the caller found them by evaluating each: a port whose conflicts
// names it, as pkgconf's names pkgconfig and OpenBLAS-devel's OpenBLAS,
// installs the same files. Spec is the path-style dependency on the port
// the tree writes most, path:bin/pkg-config:pkgconfig; empty where the
// tree writes none, as for libffi, whose 1,562 dependencies are all
// port:libffi though libffi-devel conflicts with it (the measure over the
// Mac's tree). Nil where none can be read.
type Providers func(port string) (others []string, spec string)

// CheckDependencies offers a path-style dependency for each port:
// dependency a change adds where another port can satisfy it, as reviewers
// ask (the review survey's §5, 8 PRs: path:bin/pkg-config:pkgconfig, so
// pkgconf does), and the tree writes it so: a notice, holding nothing, on
// the lines the change touched alone, and its claim that another port
// provides the files the providers' evaluation's to make.
func CheckDependencies(portfiles []Portfile, providers Providers) []Finding {
	if providers == nil {
		return nil
	}
	var findings []Finding
	for _, p := range portfiles {
		if p.After == "" {
			continue
		}
		offered := map[string]bool{}
		for _, dependency := range portfile.AddedPortDependencies([]byte(p.Before), []byte(p.After)) {
			if offered[dependency.Port] {
				continue
			}
			others, spec := providers(dependency.Port)
			if len(others) == 0 || spec == "" {
				continue
			}
			offered[dependency.Port] = true
			findings = append(findings, Finding{Code: "path-style", Severity: Warning, Where: fmt.Sprintf("%s:%d", p.Path, dependency.Line),
				Message: fmt.Sprintf("%s takes port:%s, which %s can satisfy too; the tree writes it path-style, %s, which lets either", dependency.Option, dependency.Port, strings.Join(others, " or "), spec)})
		}
	}
	return findings
}

// Evaluate is a Portfile's main port as MacPorts evaluates a text of it at
// path; false where it can't be evaluated.
type Evaluate func(path string, text []byte) (macports.PortInfo, bool)

// restatable are the options the review survey found set to what a
// PortGroup or base sets already (§1, 35 PRs): github.setup's and
// go.setup's name, version, homepage, master_sites, and distname, the
// cargo PortGroup's use_configure, the python PortGroup's python.pep517,
// and use_parallel_build.
var restatable = []string{"name", "version", "homepage", "master_sites", "distname", "use_configure", "use_parallel_build", "python.pep517"}

// CheckRestated says each line a change adds that sets what the Portfile's
// PortGroups or base set already, by evaluating the Portfile without it,
// the alternative: a restatable option evaluating the same, or a
// dependency line of one port: dependency, as the python PortGroup's
// py-setuptools or golang's go, still declared in the same phase (the
// review survey's §1). A notice, holding nothing, on the lines the change
// touched alone; a line whose Portfile can't be evaluated either way
// isn't said.
func CheckRestated(portfiles []Portfile, evaluate Evaluate) []Finding {
	if evaluate == nil {
		return nil
	}
	var findings []Finding
	for _, p := range portfiles {
		if p.After == "" {
			continue
		}
		var with *macports.PortInfo
		for _, line := range portfile.AddedLines([]byte(p.Before), []byte(p.After)) {
			option, _, _ := strings.Cut(line.Option, "-")
			phase, dependency := strings.CutPrefix(option, "depends_")
			if !slices.Contains(restatable, line.Option) && !dependency {
				continue
			}
			var port string
			if dependency {
				words := strings.Fields(line.Text)[1:]
				if len(words) != 1 || !strings.HasPrefix(words[0], "port:") {
					continue
				}
				port = strings.TrimPrefix(words[0], "port:")
			}
			if with == nil {
				info, ok := evaluate(p.Path, []byte(p.After))
				if !ok {
					break
				}
				with = &info
			}
			without, ok := evaluate(p.Path, line.Without)
			if !ok {
				continue
			}
			same := false
			if dependency {
				same = slices.ContainsFunc(without.Dependencies, func(d macports.Dependency) bool { return d.Phase == phase && d.Port == port })
			} else {
				value, known := optionValue(*with, line.Option)
				other, knownWithout := optionValue(without, line.Option)
				same = known && knownWithout && value == other
			}
			if same {
				findings = append(findings, Finding{Code: "restated", Severity: Warning, Where: fmt.Sprintf("%s:%d", p.Path, line.Line),
					Message: fmt.Sprintf("%q restates what its PortGroups or MacPorts set already: without it, the port evaluates the same", line.Text)})
			}
		}
	}
	return findings
}

// optionValue is an option of an evaluated port, its name and version
// among them; false where the evaluation didn't settle it.
func optionValue(info macports.PortInfo, option string) (string, bool) {
	switch option {
	case "name":
		return info.Name, info.Name != ""
	case "version":
		return info.Version, info.Version != ""
	}
	if _, failed := info.OptionErrors[option]; failed {
		return "", false
	}
	value, ok := info.Options[option]
	return value, ok
}
