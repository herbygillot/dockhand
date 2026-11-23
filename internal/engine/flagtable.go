package engine

import (
	"fmt"
	"strings"
)

// FlagRow is one row of the flag check's table (docs/designs/
// preparation-and-request.md, "The flag check's table", #195): flags that
// mean one thing, the request's field that keeps them, the commands that
// take them, the stage that reads them, what leaving them out means, what
// false or empty means, how long they last, and their outcomes. A flag
// another contract owns names it in Owner, and the rest of its row is that
// contract's.
type FlagRow struct {
	Flags        []string
	Field        string
	TakenBy      string
	ReadAt       string
	LeftOut      string
	FalseOrEmpty string
	Lasts        string
	Outcomes     string
	Owner        string
}

// FlagTable is every flag of update, bump, ship, submit, check, edit and
// revbump, a row each, or a row naming the contract that owns it. The
// contract test fails for a flag with no row and a row with no flag, and
// its page is made from it (FlagsPage).
var FlagTable = []FlagRow{
	{Flags: []string{"[version]"}, Field: "Targets[].Named", TakenBy: "update, bump; ship through a pinned target", ReadAt: "edit", LeftOut: "the newest release, or the one kept", Lasts: "with the target's release, on the request", Outcomes: "done, satisfied, refused"},
	{Flags: []string{"--shared-release"}, Field: "Shared", TakenBy: "update, bump, ship", ReadAt: "edit", LeftOut: "each port its own release", FalseOrEmpty: "as left out", Lasts: "the request", Outcomes: "done; satisfied with no sibling"},
	{Flags: []string{"--keep-old-checksums"}, Field: "Source.KeepOldChecksums", TakenBy: "update, bump, ship", ReadAt: "edit", LeftOut: "false", FalseOrEmpty: "as left out", Lasts: "the request", Outcomes: "done, satisfied"},
	{Flags: []string{"--keep-modeline"}, Field: "Source.KeepModeline", TakenBy: "update, bump, ship, revbump; serve by setting", ReadAt: "edit", LeftOut: "the setting", FalseOrEmpty: "false, over a setting or a kept true", Lasts: "the request", Outcomes: "done, satisfied"},
	{Flags: []string{"--keep-http"}, Field: "Source.KeepHTTP", TakenBy: "update, bump, ship; serve by setting", ReadAt: "edit", LeftOut: "update.keep_http", FalseOrEmpty: "false, over the setting", Lasts: "the request; the edit's HTTPS", Outcomes: "done, satisfied"},
	{Flags: []string{"--source"}, Field: "Source.Source", TakenBy: "update, bump, ship; serve by setting", ReadAt: "edit", LeftOut: "update.source, applied when a version edit is made", Lasts: "the request; the edit's Source", Outcomes: "done, satisfied, refused"},
	{Flags: []string{"--follow-moves"}, Field: "Source.FollowMoves", TakenBy: "update, bump, ship; serve by the port's moves", ReadAt: "edit", LeftOut: "the port's moves", FalseOrEmpty: "don't follow", Lasts: "the request", Outcomes: "done, satisfied"},
	{Flags: []string{"--with-obsolete"}, Field: "Actions.WithObsolete", TakenBy: "update, bump, ship", ReadAt: "edit", LeftOut: "the stub is left, and said to lag", FalseOrEmpty: "as left out", Lasts: "the request", Outcomes: "done; satisfied with no stub"},
	{Flags: []string{"--revbump-dependents"}, Field: "Actions.RevbumpDependents", TakenBy: "update, bump, ship", ReadAt: "edit", LeftOut: "none bumped", FalseOrEmpty: "as left out", Lasts: "the request", Outcomes: "done; satisfied with none"},
	{Flags: []string{"--except"}, Field: "Actions.Except", TakenBy: "update, bump, ship", ReadAt: "edit", LeftOut: "none left out", FalseOrEmpty: "as left out", Lasts: "the request; the edit's Excepted", Outcomes: "done; refused alone"},
	{Flags: []string{"--closes"}, Field: "Actions.Closes", TakenBy: "update, bump, ship", ReadAt: "edit", LeftOut: "none", Lasts: "the branch's Closes", Outcomes: "done, satisfied; refused with nothing else to change"},
	{Flags: []string{"--hand-edit"}, Field: "none: the attempt's own", TakenBy: "update", ReadAt: "edit", Lasts: "the hand edit's record", Outcomes: "as today"},
	{Flags: []string{"--branch", "--new", "--port", "--pr"}, Field: "none: they choose the branch", TakenBy: "update, ship, submit, check, edit, revbump", ReadAt: "before the request", LeftOut: "the port's branch, started where none is", Lasts: "the command", Outcomes: "done, refused"},
	{Flags: []string{"--mine", "--maintainer", "--category", "--not-maintainer", "--passing"}, Field: "none: they make targets", TakenBy: "update, ship", ReadAt: "before the request", Lasts: "the command", Outcomes: "targets, or none"},
	{Flags: []string{"--over-allowance"}, Field: "none: the lookup's", TakenBy: "update, ship", ReadAt: "before the request", LeftOut: "false", Lasts: "the command", Outcomes: "as today"},
	{Flags: []string{"--to"}, Field: "To", TakenBy: "ship; bump as --to submit", ReadAt: "every stage", LeftOut: "what is kept, else submit", Lasts: "the request, where given", Outcomes: "done; kept; replaced by a later one given"},
	{Flags: []string{"--yes"}, Field: "none: the caller's", TakenBy: "update, ship, submit, check; bump fixed", ReadAt: "every stage", LeftOut: "a person answers", Lasts: "the command", Outcomes: "as today"},
	{Flags: []string{"--on", "--build"}, Field: "Check.On, Check.Build: one place", TakenBy: "bump, ship, check", ReadAt: "the check", LeftOut: "the place kept, else the settings of that moment", Lasts: "the request; either replaces the other", Outcomes: "done; kept below the check; not read by --baseline"},
	{Flags: []string{"--tests"}, Field: "Check.Tests", TakenBy: "bump, ship, check", ReadAt: "the check", LeftOut: "what is kept, else the settings of that moment", Lasts: "the request", Outcomes: "done; kept below the check; not read by --baseline"},
	{Flags: []string{"--draft", "--type", "--skip-notification"}, Field: "Publish.Draft, Publish.Type, Publish.SkipNotification", TakenBy: "bump, ship, submit", ReadAt: "submit", LeftOut: "what is kept, else not a draft, the type worked out, notified", FalseOrEmpty: "false, over a kept true", Lasts: "the request, then the publication", Outcomes: "done, kept"},
	{Flags: []string{"--title", "--note", "--no-note"}, Field: "the branch's own", TakenBy: "bump, ship, submit", ReadAt: "submit", LeftOut: "what the branch holds, else worked out", FalseOrEmpty: "an empty --title clears it; an empty --note is refused and --no-note clears it", Lasts: "the branch, saved with the request", Outcomes: "done, kept"},
	{Flags: []string{"--tested-binaries", "--tested-variants"}, Field: "Publish.TestedBinaries, Publish.TestedVariants as asked, then the branch's own", TakenBy: "bump, ship, submit", ReadAt: "submit", LeftOut: "what the branch holds for these files", FalseOrEmpty: "false withdraws it, pending or bound", Lasts: "the request until bound to its files, then the branch", Outcomes: "done, kept"},
	{Flags: []string{"--no-check"}, Field: "Publish.NoCheck", TakenBy: "submit", ReadAt: "submit", LeftOut: "what is kept, else a check", FalseOrEmpty: "false, over a kept true", Lasts: "the request, then the publication", Outcomes: "done, kept"},
	{Flags: []string{"--ready", "--accept", "--standing", "--remote", "--rewrite-description"}, Field: "submit's own", TakenBy: "submit", ReadAt: "submit", Owner: "the publication record (#191); a continued submit carries --draft, --no-check, --type and --skip-notification, a flag left out told from false"},
	{Flags: []string{"--head"}, Field: "submit's and check's own", TakenBy: "submit, check", ReadAt: "submit; the check", Owner: "the publication record (#191) and the check lifecycle (#193)"},
	{Flags: []string{"--fresh", "--only", "--also", "--variants", "--keep", "--staged", "--working-tree", "--include", "--replace", "--baseline", "--of", "--enqueue", "--for"}, Field: "check's own", TakenBy: "check", ReadAt: "the check", Owner: "the check lifecycle (#193)"},
	{Flags: []string{"--plan"}, Field: "none", TakenBy: "update, bump, ship, check, submit, revbump", Outcomes: "writes nothing"},
	{Flags: []string{"--subject", "--no-open"}, Field: "a primitive's own", TakenBy: "revbump, edit", Outcomes: "as today"},
	{Flags: []string{"--tree", "--db", "--tool", "--json", "--verbose", "--migrate-database"}, Field: "settings'", TakenBy: "all", Owner: "settings (--migrate-database: #281)"},
}

// FlagsPage is the flags page, made from FlagTable: the page is never
// edited by hand, and a test fails where it and the table differ.
func FlagsPage() string {
	var b strings.Builder
	b.WriteString("# Flags\n\n")
	b.WriteString("Every flag of `update`, `bump`, `ship`, `submit`, `check`, `edit` and `revbump`: what keeps it, which commands take it, the stage that reads it, what leaving it out means, what false or empty means, how long it lasts, and what can become of it. A flag another part of dockhand owns names it. This page is made from dockhand's own table; a test fails where the two differ.\n\n")
	b.WriteString("| Flag | Kept in | Taken by | Read at | Left out | False or empty | Lasts | Outcomes |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, row := range FlagTable {
		flags := make([]string, len(row.Flags))
		for i, flag := range row.Flags {
			flags[i] = "`" + flag + "`"
		}
		outcomes := row.Outcomes
		if row.Owner != "" {
			outcomes = strings.TrimPrefix(strings.TrimSpace(outcomes+"; "), "; ") + "owned by " + row.Owner
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", strings.Join(flags, ", "), row.Field, row.TakenBy, row.ReadAt, row.LeftOut, row.FalseOrEmpty, row.Lasts, outcomes)
	}
	return b.String()
}

// FlagFields are the request's fields a row names: its Field's words that
// begin with a capital, as "Source.KeepHTTP" or "Targets[].Named".
func (r FlagRow) FlagFields() []string {
	var fields []string
	for _, word := range strings.FieldsFunc(r.Field, func(c rune) bool { return c == ' ' || c == ',' || c == ':' }) {
		if word != "" && word[0] >= 'A' && word[0] <= 'Z' {
			fields = append(fields, word)
		}
	}
	return fields
}

// FlagRowOf is the row a flag's name, --name, is in; false where none is.
func FlagRowOf(name string) (FlagRow, bool) {
	for _, row := range FlagTable {
		for _, flag := range row.Flags {
			if flag == "--"+name {
				return row, true
			}
		}
	}
	return FlagRow{}, false
}
