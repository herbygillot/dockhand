package record

import (
	"encoding/json"
	"fmt"
)

// There is no ref name here, and NotesRef's reason for existing does
// not carry over: that constant is in this package because the codec's
// refusals tell a user which ref to clear, and clearing is the last
// thing to tell anyone about an audit. Where these rows are kept is the
// ledger's business alone.

// Driver is who an act was carried out by — a person at a terminal, or
// dockhand running unattended. It is provenance and nothing else: no
// gate anywhere reads it, and recording it exists so that a later
// question about how a change reached review can be answered by a query
// instead of an estimate.
type Driver string

const (
	// Human is a person's own act, which is every publication today.
	Human Driver = "human"
	// Machine is dockhand publishing unattended. The word exists here
	// because the wire is worth defining once; nothing writes it yet.
	Machine Driver = "machine"
)

// MintedVia says whether the branch being published came from a
// deliberate single target or from a sweep over many. It is the field
// the ladder's arithmetic turns on — human promotions of sweep-minted
// changes are its numerator — so it is recorded rather than inferred
// afterwards from a branch name.
type MintedVia string

const (
	// MintedSingle is a change the user named, one target per run.
	MintedSingle MintedVia = "single"
	// MintedSweep is a change a sweep proposed on its own.
	MintedSweep MintedVia = "sweep"
)

// Evidence is what the publication rested on: a verdict set that
// cleared promote's gate, or none. It is the published claim rather
// than a re-reading of the record, because the record can gain runs
// after the pull request is open and the audit's question is what was
// known when the change went out.
type Evidence string

const (
	// Verified means the tip's verdict set cleared the gate at publish.
	Verified Evidence = "verified"
	// Unverified means it did not, and the pull request said so.
	Unverified Evidence = "unverified"
)

// Outcome is what became of a published change. It is empty on the
// opening row and filled on the closing one — the two are separate
// rows, never one row edited twice.
type Outcome string

const (
	// Merged means the pull request landed.
	Merged Outcome = "merged"
	// Rejected means it was closed without merging. Rejection is an
	// outcome worth counting, not an absence.
	Rejected Outcome = "rejected"
)

// OutcomeRow is one line of the audit log: a publication, or the same
// publication closed by what became of it. Rows are keyed by the mint
// sha and appended, so a change that was published and later merged has
// two rows and the second is the current one.
//
// A whole row is repeated on close rather than a delta being appended.
// That costs a few bytes and buys a log whose last line is the answer,
// readable without replaying what came before it.
//
// The timestamps are strings and not time.Time deliberately: this
// package may not import a clock any more than it may import a
// repository, and a wire format that cannot read the time is a wire
// format that cannot disagree with the caller that did. RFC 3339 is
// what the callers write.
//
// There is no schema stamp, which is the one place this format parts
// company with the verification note. A note's schema exists because a
// newer dockhand can record state an older one would act on wrongly —
// releasing a worker, authorizing a promotion. Nothing acts on an audit
// row. It is read by queries written against the shape they find, and
// JSON already tolerates a field arriving that a reader does not know,
// so refusing a row for carrying one would lose history to protect
// nothing.
//
// Field order is the wire order, as everywhere else in this package.
type OutcomeRow struct {
	// MintSha is the commit that was published — the key the rows for
	// one change are gathered under.
	MintSha string `json:"mint_sha"`
	Branch  string `json:"branch"`
	// Port and Target are what changed and what it moved to. Both are
	// omitted when unknown rather than written empty: a row that cannot
	// name the port is still a real publication.
	Port      string    `json:"port,omitempty"`
	Target    string    `json:"target,omitempty"`
	MintedVia MintedVia `json:"minted_via"`
	// AskedBy and PublishedBy are two fields because they will disagree:
	// a machine may publish what a person queued, and the ladder counts
	// those apart. Today one act does both.
	AskedBy     Driver   `json:"asked_by"`
	PublishedBy Driver   `json:"published_by"`
	Evidence    Evidence `json:"evidence"`
	PRNumber    int      `json:"pr_number,omitempty"`
	PublishedAt string   `json:"published_at"`

	// Outcome, MergeSha and SettledAt are the closing row's own. They
	// are empty on an opening row, and an empty Outcome is what makes a
	// row set still open.
	Outcome   Outcome `json:"outcome,omitempty"`
	MergeSha  string  `json:"merge_sha,omitempty"`
	SettledAt string  `json:"settled_at,omitempty"`
}

// Open reports whether the row is still awaiting an outcome. A row set
// is open when its LAST row is, because a publication appended after a
// close reopens it: re-promoting a rejected change is a new publication
// and deserves a close of its own.
func (r OutcomeRow) Open() bool { return r.Outcome == "" }

// EncodeOutcomeRow renders a row as the single line it occupies in the
// note. One line and no indentation: the note is a log, the reader
// splits it on newlines, and a row spanning several would end the only
// property that makes that split exact.
func EncodeOutcomeRow(row OutcomeRow) ([]byte, error) {
	return json.Marshal(row)
}

// DecodeOutcomeRows reads a note's bytes back as the rows appended to
// it, oldest first.
//
// Blank lines are skipped rather than parsed: git writes one between
// what a note held and what was appended to it, so they are the
// storage's punctuation and never a row. A line that does not parse is
// an error naming which line it was — an audit whose middle is
// unreadable must say so, since the alternative is a query that quietly
// counts fewer publications than happened.
func DecodeOutcomeRows(b []byte) ([]OutcomeRow, error) {
	var rows []OutcomeRow
	// Hand-rolled rather than reached for: this package's import budget
	// is the wire's, and splitting a byte slice on newlines is not a
	// reason to widen it.
	line := 0
	for start := 0; start <= len(b); {
		end := start
		for end < len(b) && b[end] != '\n' {
			end++
		}
		field := b[start:end]
		start = end + 1
		line++
		if len(field) == 0 {
			continue
		}
		var row OutcomeRow
		if err := json.Unmarshal(field, &row); err != nil {
			return nil, fmt.Errorf("outcome row on line %d does not parse: %w", line, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}
