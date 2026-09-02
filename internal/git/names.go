package git

// BranchNamespace is the prefix under refs/heads/ that every branch
// dockhand mints lives in — the one namespace status lists, clean
// sweeps, and resolve matches a target against. Slash-terminated
// because Branches matches ref namespaces, not substrings. It is a
// branch namespace only: VerifyNotesRef is a notes ref that happens to
// share the word, and the two are not one constant.
const BranchNamespace = "dockhand/"

// MintBranchName is the branch a plan's slug is minted under: the
// namespace and the slug, with no other structure in between, so the
// slug alone identifies the change within it.
func MintBranchName(slug string) string {
	return BranchNamespace + slug
}

// abbrevLen is the width of a displayed sha: twelve hex digits, enough
// to be unique in a ports tree and short enough for one line.
const abbrevLen = 12

// Abbrev is a sha as messages and PR bodies print it — its first
// twelve characters. A full sha is always forty, but callers also
// hand over revisions that are already short (a partial sha from a
// lockfile, an empty string from a missing field), and those come
// back whole rather than indexed past their end.
func Abbrev(sha string) string {
	if len(sha) > abbrevLen {
		return sha[:abbrevLen]
	}
	return sha
}
