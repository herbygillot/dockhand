package render

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

// RepoURL is where the PR body's "dockhand" links point, so a reviewer
// meeting the tool in a PR can see what vouched for the claim.
const RepoURL = "https://github.com/herbygillot/dockhand"

// abbrevLen is the width of a displayed sha: twelve hex digits, enough
// to be unique in a ports tree and short enough for one line.
const abbrevLen = 12

// abbrevSha is a sha as the body prints it — its first twelve
// characters, with anything already shorter coming back whole rather
// than indexed past its end.
//
// It restates git.Abbrev's rule instead of calling it, because a
// renderer that imports the package which shells out to git is a
// renderer that can read a repository, and these bytes are meant to be
// checkable without one. The two must agree on twelve; the golden
// pinning `0123456789ab` is what says so.
func abbrevSha(sha string) string {
	if len(sha) > abbrevLen {
		return sha[:abbrevLen]
	}
	return sha
}

// lintClause phrases a note's lint record for the evidence line.
func lintClause(lint string) string {
	if lint == "clean" {
		return "clean"
	}
	return "with " + lint
}

// PRBody renders the PR body in the shape of macports-ports' own
// pull request template, with the boxes dockhand can honestly vouch
// for checked and everything it cannot left for the human. Candour is
// the accepted currency: the verdict set is enumerated in full, an
// unverified promotion says so, and the install checkbox strikes the
// template's command through in favour of the one actually run.
func PRBody(n record.Record, verified bool, closes string, ownCommits int, checkedPRs bool) string {
	var b strings.Builder
	b.WriteString("#### Description\n\n")
	var passed []string
	tested, linted := false, false
	if !verified {
		b.WriteString("Not locally verified: no verification environment on the submitting machine.\n")
	} else {
		// The whole verdict set, enumerated: a passing platform and a
		// declining one are both facts a reviewer wants.
		var parts []string
		for _, plat := range n.Platforms() {
			r := n.Runs[plat]
			switch r.State {
			case record.Passed:
				what := "built in a pristine VM"
				if r.Tested {
					what, tested = "built and tested in a pristine VM", true
				}
				// The lint claim rides the evidence line, because the
				// checked box below is only honest if the body states
				// what backs it.
				switch {
				case r.Lint != "" && r.Linted:
					what, linted = "linted "+lintClause(r.Lint)+", "+what, true
				case r.Linted:
					what, linted = "linted, "+what, true
				}
				parts = append(parts, plat+": "+what)
				passed = append(passed, plat)
			case record.Unsupported:
				parts = append(parts, plat+": the port declines this platform (known_fail)")
			case record.Running, record.Failed, record.Blocked, record.Canceled,
				record.Superseded, record.Deferred, record.Errored:
				// Nothing to vouch for. This list enumerates what was
				// established about the change, and a run still going, one
				// that failed, or one that never reached the change
				// establishes nothing — promote's own gate is where a
				// failure is answered for, not the body.
			}
		}
		// One verdict per line: GitHub keeps single newlines in PR
		// bodies, so the set reads as the list it is.
		fmt.Fprintf(&b, "Verified with [dockhand](%s) at commit `%s`\n", RepoURL, abbrevSha(n.Sha))
		for _, part := range parts {
			fmt.Fprintf(&b, "  — %s.\n", part)
		}
	}
	if closes != "" {
		fmt.Fprintf(&b, "\nCloses: https://trac.macports.org/ticket/%s\n", closes)
	}

	b.WriteString("\n###### Type(s)\n\n- [ ] bugfix\n- [ ] enhancement\n- [ ] security fix\n")
	if len(passed) > 0 {
		b.WriteString("\n###### Tested on\n")
		for _, plat := range passed {
			fmt.Fprintf(&b, "- macOS %s — pristine tart VM, via dockhand\n", plat)
		}
	}

	box := func(ok bool, item string) {
		mark := " "
		if ok {
			mark = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s\n", mark, item)
	}
	// The single minted commit is the one whose message dockhand wrote
	// in project format; a branch the user grew past it is theirs to
	// vouch for.
	single := ownCommits == 1
	b.WriteString("\n###### Verification\nHave you\n\n")
	box(single, "followed our [Commit Message Guidelines](https://trac.macports.org/wiki/CommitMessages)?")
	box(single, "squashed and [minimized your commits](https://guide.macports.org/#project.github)?")
	box(checkedPRs, "checked that there aren't other open [pull requests](https://github.com/macports/macports-ports/pulls) for the same change?")
	box(false, "referenced existing tickets on [Trac](https://trac.macports.org/wiki/Tickets) with full URL in commit message?")
	box(linted, "checked your Portfile with `port lint`?")
	box(tested, "tried existing tests with `sudo port test`?")
	box(len(passed) > 0, "tried a full install with ~~`sudo port -vst install`~~ `sudo port install` in a pristine VM")
	box(false, "tested basic functionality of all binary files?")
	box(false, "checked that the Portfile's most important [variants](https://trac.macports.org/wiki/Variants) haven't been broken?")
	// Every body signs off, the unverified ones included: a PR with no
	// verification claim still owes the reviewer the fact of how it was
	// made.
	fmt.Fprintf(&b, "\nAutomated by [dockhand](%s)\n", RepoURL)
	return b.String()
}
