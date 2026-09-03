package render

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verdict"
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

// subjectPrefix names the member an evidence line is about, for a
// cohort, and names nothing for a change with one subject.
//
// A single change's lines already have a subject: the PR is about that
// port and its title says so, and prefixing every line with it would
// be noise in the one place candour is the whole point. A cohort's
// lines need it, because "Sequoia: built in a pristine VM" said nine
// times over is a claim about nine different ports that reads as one
// repeated nine times.
func subjectPrefix(named bool, port string) string {
	if !named {
		return ""
	}
	return port + " on "
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
		named := verdict.Names(n)
		onPlatform := map[string]bool{}
		var parts []string
		for _, ref := range verdict.Runs(n) {
			r := ref.Run
			plat := ref.Platform
			switch r.State {
			case record.Passed:
				what := "built in a pristine VM"
				// The test suite was asked of the ENVIRONMENT and so is
				// recorded on it: one guest runs one submission's tests,
				// however many subjects installed into it.
				if ref.Job.Test {
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
				parts = append(parts, subjectPrefix(named, ref.Port)+plat+": "+what)
				// The "Tested on" section names environments, so a
				// platform appears once however many members passed in
				// it: listing one guest nine times would overstate the
				// evidence by a factor of nine.
				if !onPlatform[plat] {
					onPlatform[plat] = true
					passed = append(passed, plat)
				}
			case record.Unsupported:
				parts = append(parts, subjectPrefix(named, ref.Port)+plat+": the port declines this platform (known_fail)")
			case record.Queued, record.Submitting, record.Running, record.Failed,
				record.Blocked, record.Canceled, record.Superseded, record.Errored:
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
