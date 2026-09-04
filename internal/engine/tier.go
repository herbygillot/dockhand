package engine

// The maintainer tier of the port a change targets, read for the one
// sentence that needs it: what an elapsed review window means, and
// therefore what a follow-up can honestly say.

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
)

// portTier reads the target port's maintainers field off the branch's
// own tip and reduces it to a tier.
//
// From the BRANCH and not from the ports tree, which is the whole
// reason this is cheap enough for a report to run. The change edited
// that Portfile, so its own commit carries the exact bytes the pull
// request is about — no glob over the tree's categories, no ambiguity
// between two ports sharing a name, and no PortIndex pass (41630
// entries, 25.6MB) inside a verb people are supposed to run casually.
// It also answers the honest question: the tier the reviewer will see
// is the one in the diff, not the one the host happened to have checked
// out.
//
// A line read and not an evaluation. Evaluating the Portfile would cost
// a port-tclsh process per branch and would be the correct thing to do
// if the answer were load-bearing; it is not — it chooses a clause in
// one advisory sentence — and an unparsed line is an honest "we do not
// know", which is a state the rendering already has to carry.
//
// Every failure is TierUnknown, and none of them is reported. A tier is
// a courtesy on top of a pull request's age; a status pass that failed
// because a Portfile moved would be spending the reader's whole report
// on the least important thing in it.
func portTier(ctx context.Context, repo *git.Repo, branch string, n *record.Record) render.Tier {
	if n == nil {
		return render.TierUnknown
	}
	dir := n.Headline().Portdir
	if dir == "" {
		return render.TierUnknown
	}
	// The portdir is the host's path to the port; the blob's path is the
	// tree-relative <category>/<port>. The repository is the tree, so the
	// last two elements are the whole of what git needs.
	parts := strings.Split(strings.Trim(strings.ReplaceAll(dir, "\\", "/"), "/"), "/")
	if len(parts) < 2 {
		return render.TierUnknown
	}
	path := strings.Join(parts[len(parts)-2:], "/") + "/Portfile"
	b, err := repo.BlobAt(ctx, branch, path)
	if err != nil {
		return render.TierUnknown
	}
	return tierOf(maintainersField(string(b)))
}

// maintainersField pulls the maintainers line out of a Portfile's text,
// continuations included.
//
// A Portfile is Tcl and this is a scan, which is a real limitation and
// a bounded one: it finds the field where it is actually written — at
// the top level, first word on a line — and misses one built up by
// code. Ports that compute their maintainers exist and are rare, and
// what missing one costs is the parenthetical on a single advisory
// line.
//
// The word is matched whole. `nomaintainer` and `openmaintainer` both
// end in "maintainer", and a prefix test for "maintainers" would match
// neither — but a Contains test anywhere in this file would match both,
// which is the substring trap this field invites. Every comparison here
// is against a token.
func maintainersField(portfile string) string {
	lines := strings.Split(portfile, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		rest, ok := strings.CutPrefix(line, "maintainers")
		if !ok || (rest != "" && !isSpace(rest[0])) {
			continue
		}
		field := strings.TrimSpace(rest)
		// A backslash at the end of the line continues the field onto the
		// next one, which is how a port with several maintainers is
		// conventionally written.
		for strings.HasSuffix(field, "\\") && i+1 < len(lines) {
			i++
			field = strings.TrimSpace(strings.TrimSuffix(field, "\\")) + " " + strings.TrimSpace(lines[i])
		}
		return field
	}
	return ""
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

// tierOf reduces a maintainers field to a tier.
//
// nomaintainer wins over everything: a port can name a person and still
// declare nobody is on the hook, and the keyword is the declaration
// that settles it. openmaintainer comes next because it is the one that
// changes what a stranger may do. A port that names maintainers and
// neither keyword is maintained, and a port that names nothing at all
// is unknown rather than nomaintainer — a missing field and a field
// saying "nobody" are different claims, and only one of them was made.
func tierOf(field string) render.Tier {
	if field == "" {
		return render.TierUnknown
	}
	keys, none := portindex.Maintainers(field)
	if none {
		return render.TierNomaintainer
	}
	// portindex drops the openmaintainer keyword on the floor — it is
	// building an index of people, and openmaintainer is not one. It is
	// recovered here by looking for it as its own whole token, which is
	// also why the fields are lowercased first: the keyword is written
	// both ways in the tree.
	for _, tok := range strings.Fields(strings.ToLower(field)) {
		if strings.Trim(tok, "{}") == "openmaintainer" {
			return render.TierOpenmaintainer
		}
	}
	if len(keys) == 0 {
		return render.TierUnknown
	}
	return render.TierMaintained
}
