package intent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// parsed is the pair every rule is asked about: the bytes, and the tree
// the first proof reads them through.
func parsed(t *testing.T, src string) ([]byte, *syntax.Script) {
	t.Helper()
	cst, errs := syntax.Parse([]byte(src))
	require.Empty(t, errs)
	return []byte(src), cst
}

func TestExamineOffersTheModelineOnlyWhenMissing(t *testing.T) {
	src, cst := parsed(t, "PortSystem 1.0\nname x\n")
	ex := Examine(src, cst, nil, nil, info.Values{}, nil)
	require.Len(t, ex.Riders, 1)
	assert.Equal(t, RuleModeline, ex.Riders[0].Rule)
	assert.Equal(t, 0, ex.Riders[0].Edit.Start)
	assert.Equal(t, 0, ex.Riders[0].Edit.End)
	assert.Equal(t, Modeline+"\n", ex.Riders[0].Edit.New)
	assert.Equal(t, "modeline", ex.Riders[0].Edit.Reason)

	for _, first := range []string{
		Modeline,
		"# -*- coding: utf-8; mode: tcl -*-",
		"# vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4",
		"# vi: set ts=4:",
	} {
		src, cst := parsed(t, first+"\nPortSystem 1.0\n")
		ex := Examine(src, cst, nil, nil, info.Values{}, nil)
		assert.Empty(t, ex.Riders, "an existing modeline is never second-guessed: %s", first)
	}

	// An ordinary leading comment is not a modeline.
	src, cst = parsed(t, "# This port is maintained upstream\nPortSystem 1.0\n")
	ex = Examine(src, cst, nil, nil, info.Values{}, nil)
	assert.Len(t, ex.Riders, 1)
}

// The whole leading block is read, not the first line. A Portfile that
// opens with a blank line and carries its modeline on line two was
// judged to have none and got a second one written above the first;
// python/py27-dulwich and devel/tortoisehg are in exactly that shape,
// and the visibly wrong hunk would have gone to a public pull request.
// Both halves of the proof pass such an edit, correctly — the proofs
// certify that a rider is inert, never that it is right.
func TestExamineReadsTheWholeLeadingCommentBlockForAModeline(t *testing.T) {
	for _, opening := range []string{
		"\n" + Modeline + "\n",
		"# Copyright the ports tree\n" + Modeline + "\n",
		"\n\n# vim:fenc=utf-8:ft=tcl\n",
		"   " + Modeline + "\n",
	} {
		src, cst := parsed(t, opening+"PortSystem 1.0\n")
		assert.Empty(t, Riders(src, cst), "an existing modeline is never second-guessed: %q", opening)
	}

	// A leading block with no modeline in it still gets one, and a
	// modeline BELOW the first command is not a leading modeline: the
	// block ends where the port starts.
	for _, opening := range []string{
		"\n# an ordinary note\n",
		"PortSystem 1.0\n" + Modeline + "\n",
	} {
		src, cst := parsed(t, opening+"name x\n")
		assert.Len(t, Riders(src, cst), 1, "nothing above the first command is a modeline: %q", opening)
	}
}

// A rule dropped for failing the first proof is a fact the sweep
// reports, because "no rule had anything to offer" and "a rule offered
// something and it was suppressed" are two different answers and the
// second is a bug worth seeing.
func TestSweepReportsTheRulesItDropped(t *testing.T) {
	const source = "PortSystem 1.0\nname x\nversion 1.0\n"
	src, cst := parsed(t, source)

	withRules(t, "overreaching", func(s []byte) (edit.Edit, bool) {
		at := indexOf(t, string(s), "1.0")
		return edit.Edit{Start: at, End: at + 3, Old: "1.0", New: "2.0", Reason: "overreaching"}, true
	})
	offered, dropped := Sweep(src, cst)
	assert.Empty(t, offered)
	assert.Equal(t, []Rule{"overreaching"}, dropped)

	headed, hcst := parsed(t, Modeline+"\nPortSystem 1.0\n")
	withRules(t, "modeline", modelineEdit)
	offered, dropped = Sweep(headed, hcst)
	assert.Empty(t, offered)
	assert.Empty(t, dropped, "a rule with nothing to offer was not dropped; it had nothing to drop")
}

// The rest of the shape is declared, not implemented. The assertion is
// here so that the day a rule starts producing one, the step that did
// it is the step that changed this line.
func TestExamineProducesNoCascadesOrFindingsYet(t *testing.T) {
	src, cst := parsed(t, "PortSystem 1.0\n")
	ex := Examine(src, cst, nil, nil, info.Values{}, []string{"a-dependent"})
	assert.Empty(t, ex.Cascades)
	assert.Empty(t, ex.Findings)
}

// The rider's rule name is the word a note carries, so a reader who
// sees "modeline" in a change's riders can look the rule up.
func TestRuleNamesAreTheNotesOwnWords(t *testing.T) {
	assert.Equal(t, "modeline", string(RuleModeline))
	assert.Equal(t, []string{"modeline"}, Names([]Rider{{Rule: RuleModeline}}))
	assert.Nil(t, Names(nil), "no riders is an absent field, not an empty list")
	assert.Equal(t, "add the editor modeline", Phrase([]Rider{{Rule: RuleModeline}}))
}

// The first half of the double proof, rule by rule. The Portfile below
// is the one every row is measured against: a comment, three commands,
// and a braced body with a comment inside it.
func TestInCommentOrSpaceReadsTheTokenSpans(t *testing.T) {
	const source = "# a leading note\nname x\nversion 1.0\nvariant foo {\n    # inside a body\n    depends_lib port:z\n}\n"
	src, cst := parsed(t, source)
	at := func(needle string) int { return indexOf(t, source, needle) }

	rows := []struct {
		name string
		e    edit.Edit
		want bool
	}{
		{"an insertion at the very top, which is where every modeline goes",
			edit.Edit{Start: 0, End: 0, New: Modeline + "\n"}, true},
		{"a rewrite inside the leading comment",
			edit.Edit{Start: at("note"), End: at("note") + 4, Old: "note", New: "remark"}, true},
		{"a rewrite of the whitespace between two commands",
			edit.Edit{Start: at("\nversion"), End: at("\nversion") + 1, Old: "\n", New: "\n\n"}, true},
		{"a rewrite of a command's own literal",
			edit.Edit{Start: at("1.0"), End: at("1.0") + 3, Old: "1.0", New: "2.0"}, false},
		{"an insertion in the middle of a command",
			edit.Edit{Start: at("1.0"), End: at("1.0"), New: "9"}, false},
		// The two boundary shapes. A gap boundary is where every
		// housekeeping line in a Portfile goes — but only when what is
		// written there is a line. Bytes at a command's FIRST offset run
		// into its first token unless they end in a newline, and `# ` in
		// front of `version 1.0` comments the command out; bytes at its
		// LAST offset are appended to its last token unless they begin
		// with one, and ` --enable-evil` after a command is an argument
		// the port now builds with. Both were offered as riders and both
		// were measured doing it.
		{"a whole line inserted at a command's first byte",
			edit.Edit{Start: at("version 1.0"), End: at("version 1.0"), New: "# a note\n"}, true},
		{"an insertion at a command's first byte that does not end the line",
			edit.Edit{Start: at("version 1.0"), End: at("version 1.0"), New: "# "}, false},
		{"an insertion at a command's last byte, which joins its last token",
			edit.Edit{Start: at("1.0") + 3, End: at("1.0") + 3, New: " --enable-evil"}, false},
		{"an insertion at a command's last byte that starts its own line",
			edit.Edit{Start: at("1.0") + 3, End: at("1.0") + 3, New: "\n# a note"}, true},
		{"a comment inside a braced body, which the command's span covers whole",
			edit.Edit{Start: at("inside"), End: at("inside") + 6, Old: "inside", New: "within"}, false},
		{"a span past the end of the source",
			edit.Edit{Start: 0, End: len(source) + 1}, false},
		{"a reversed span",
			edit.Edit{Start: 5, End: 2}, false},
	}
	for _, row := range rows {
		assert.Equal(t, row.want, InCommentOrSpace(src, cst, row.e), row.name)
	}

	// No parse, no proof. Handle.Source refuses a Portfile it could not
	// read, so this is a programming error answered the safe way.
	assert.False(t, InCommentOrSpace(src, nil, edit.Edit{Start: 0, End: 0, New: "x"}))
}

// A rule whose edit touches evaluated bytes never becomes a rider. It is
// the rule's bug and not the port's, so the sweep drops it and the
// port's own change is untouched.
func TestRidersDropsARuleThatTouchesEvaluatedBytes(t *testing.T) {
	const source = "PortSystem 1.0\nname x\nversion 1.0\n"
	src, cst := parsed(t, source)

	withRules(t, "overreaching", func(s []byte) (edit.Edit, bool) {
		at := indexOf(t, string(s), "1.0")
		return edit.Edit{Start: at, End: at + 3, Old: "1.0", New: "2.0", Reason: "overreaching"}, true
	})
	assert.Empty(t, Riders(src, cst), "a rule that rewrites a literal is not offering a rider")
}

// Withheld is what a decline says went undone with it, and --no-riders
// is the answer that nothing was ever going to.
func TestWithheldFollowsThePolicy(t *testing.T) {
	src, cst := parsed(t, "PortSystem 1.0\nname x\n")
	assert.Equal(t, []string{"modeline"}, Withheld(src, cst, RidersAlong))
	assert.Equal(t, []string{"modeline"}, Withheld(src, cst, RidersOnly))
	assert.Nil(t, Withheld(src, cst, RidersNone))

	headed, hcst := parsed(t, Modeline+"\nPortSystem 1.0\n")
	assert.Nil(t, Withheld(headed, hcst, RidersAlong), "nothing offered is nothing withheld")
}
