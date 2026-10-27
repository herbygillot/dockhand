package commitrules

import "slices"

// Source is a MacPorts document a rule rests on, and what it says.
type Source struct {
	Title string
	URL   string
	// Quote is the document's own words, verbatim; empty when dockhand
	// does not carry the text, and then only the link is given.
	Quote string
}

// Explanation is what dockhand explain says about one rule.
type Explanation struct {
	Code string
	// Rule is what dockhand checks, in its own words.
	Rule    string
	Sources []Source
}

// Where the quotes come from. They were copied from these files on
// 2026-09-25; the Trac wiki could not be read then, so the Commit Message
// Guidelines are linked, not quoted.
var (
	guideGitHub       = Source{Title: "The MacPorts Guide, Contributing to MacPorts", URL: "https://guide.macports.org/#project.github"}
	guideRevision     = Source{Title: "The MacPorts Guide, Portfile keywords: revision", URL: "https://guide.macports.org/#reference.keywords.revision"}
	guideEpoch        = Source{Title: "The MacPorts Guide, Portfile keywords: epoch", URL: "https://guide.macports.org/#reference.keywords.epoch"}
	guideReplacedBy   = Source{Title: "The MacPorts Guide, Portfile keywords: replaced_by", URL: "https://guide.macports.org/#reference.keywords.replaced_by"}
	template          = Source{Title: "macports-ports' pull request template", URL: "https://github.com/macports/macports-ports/blob/master/.github/PULL_REQUEST_TEMPLATE.md"}
	guidePortGroups   = Source{Title: "The MacPorts Guide, PortGroups", URL: "https://guide.macports.org/#reference.portgroup"}
	guideDependencies = Source{Title: "The MacPorts Guide, Dependencies", URL: "https://guide.macports.org/#reference.dependencies"}
	guidelines        = Source{Title: "MacPorts' Commit Message Guidelines", URL: "https://trac.macports.org/wiki/CommitMessages"}
)

func quoted(source Source, quote string) Source {
	source.Quote = quote
	return source
}

const minimize = "Be sure to rebase your changes so as to minimize the number of commits. Ideally, you should have just one."

var explanations = []Explanation{
	{Code: "subject-port", Rule: "A commit's subject starts with the port it changes and a colon, \"jq: …\", or with the ports, \"jq, jq-devel: …\", for a change to several.",
		Sources: []Source{guidelines, quoted(template, "followed our Commit Message Guidelines?")}},
	{Code: "subject-vague", Rule: "A subject says what changed, such as \"update to 1.8.1\", rather than only \"update\" or \"fix\".",
		Sources: []Source{guidelines, quoted(template, "followed our Commit Message Guidelines?")}},
	{Code: "subject-length", Rule: "A subject is kept to 60 characters; the guidelines ask for about 50 to 55.",
		Sources: []Source{guidelines}},
	{Code: "body-wrap", Rule: "A message's body wraps at 72 characters. A line that is only a URL, or a trailer whose value ends in a URL, such as Closes: with a ticket's, may run longer; a sentence holding a URL wraps.",
		Sources: []Source{guidelines}},
	{Code: "ticket-url", Rule: "A Trac ticket is cited by its full URL, such as Closes: https://trac.macports.org/ticket/71234, not as #71234, which GitHub would take for a pull request.",
		Sources: []Source{quoted(template, "referenced existing tickets on Trac with full URL in commit message?"), guidelines}},
	{Code: "ticket-trailer", Rule: "A Trac ticket a commit fixes is given as a trailer in its body, Closes: https://trac.macports.org/ticket/71234, and one it relates to as See:, not in the subject or a sentence. update, ship, and bump write Closes: with --closes. A notice, holding nothing.",
		Sources: []Source{quoted(template, "referenced existing tickets on Trac with full URL in commit message?"), guidelines}},
	{Code: "restated", Rule: "A line a change adds doesn't set what the Portfile's PortGroups or MacPorts set already: name, version, homepage, master_sites, or distname after github.setup or go.setup, use_configure no under cargo, python.pep517 under python, use_parallel_build yes, or a dependency a PortGroup adds, as py-setuptools under python or go under golang. dockhand evaluates the Portfile without the line, and says it where the port evaluates the same. A notice, holding nothing.",
		Sources: []Source{guidePortGroups}},
	{Code: "path-style", Rule: "A dependency another port can satisfy is written path-style, on a file both install, so either does: path:bin/pkg-config:pkgconfig, which pkgconf satisfies too, and path:lib/libopenblas.dylib:OpenBLAS, which OpenBLAS-devel does. A port that can is one whose conflicts names the port depended on. Only dependencies the change adds are said, as a notice holding nothing.",
		Sources: []Source{guideDependencies}},
	{Code: "follow-up", Rule: "A commit that only corrects an earlier one in the branch, such as \"fix checksums\" or \"address review\", is squashed into it. dockhand tidy proposes that.",
		Sources: []Source{
			quoted(guideGitHub, minimize+" (There are exceptions. If you have several unrelated fixes, or you're changing multiple packages, etc., you might need more than one commit. The point is to minimize them, ideally with one commit per logical change.)"),
			quoted(guideGitHub, "If you are asked for additional changes, please squash them to avoid unnecessary commits."),
			quoted(template, "squashed and minimized your commits?"),
		}},
	{Code: "merge", Rule: "A branch has no merge commits; it is rebased onto master instead. dockhand rebase does that.",
		Sources: []Source{quoted(guideGitHub, minimize)}},
	{Code: "revision-after-update", Rule: "When a port's version changes, its revision goes back to 0: the revision counts changes made within one version of the software.",
		Sources: []Source{quoted(guideRevision, "An optional integer (the default is 0) that is incremented when a port is updated independently of the version of the software.")}},
	{Code: "version-order", Rule: "A version MacPorts orders lower than the one it replaces raises the epoch by one; otherwise port upgrade never installs it, and every install stays at the old version.",
		Sources: []Source{guideEpoch}},
	{Code: "dependencies-unbumped", Rule: "A change to depends_lib or depends_run raises the revision, or comes with a new version: port upgrade rebuilds only what a version, revision, or epoch moves, and an installed copy keeps what it was built against.",
		Sources: []Source{guideRevision}},
	{Code: "revbump-build-only", Rule: "A change to depends_build, depends_fetch, or depends_extract alone needs no revision raised: build-time dependencies don't reach an installed copy, so the revision bump rebuilds every install for nothing.",
		Sources: []Source{guideRevision}},
	{Code: "replaced-by-unchanged", Rule: "Adding replaced_by also raises the revision, or changes the version or epoch, so port upgrade moves installed copies to the replacement.",
		Sources: []Source{guideReplacedBy}},
}

// Explain is what dockhand explain says about a rule's code.
func Explain(code string) (Explanation, bool) {
	i := slices.IndexFunc(explanations, func(e Explanation) bool { return e.Code == code })
	if i < 0 {
		return Explanation{}, false
	}
	return explanations[i], true
}

// Codes lists the codes of the rules, in the order they are explained.
func Codes() []string {
	codes := make([]string, len(explanations))
	for i, e := range explanations {
		codes[i] = e.Code
	}
	return codes
}
