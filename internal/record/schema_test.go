package record

import (
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The instants the fixtures carry. They are distinct so that a field
// written into the wrong key is a visible failure rather than a
// coincidence.
var (
	started     = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	claimedAt   = time.Date(2026, 9, 1, 0, 5, 0, 0, time.UTC)
	expiresAt   = time.Date(2026, 9, 1, 1, 5, 0, 0, time.UTC)
	treeAsOf    = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	ranAt       = time.Date(2026, 9, 1, 0, 40, 0, 0, time.UTC)
	foundAt     = time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC)
	heldAt      = time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	committedAt = time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	publishedAt = time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	retainUntil = time.Date(2026, 9, 1, 4, 0, 0, 0, time.UTC)
	releasedAt  = time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC)
)

func ptr[T any](v T) *T { return &v }

// populated sets every field of every section, so the wire pin below is
// a statement about the whole schema and not about the part of it
// anything writes today.
//
// It is a cohort on two platforms: two subjects, three runs, and one
// lease that carries everything — enough that the run map and the lease
// map disagree about their key sets, which is the shape the split
// exists for.
func populated() Record {
	return Record{
		// Overwritten by Encode. A caller's schema is never trusted.
		Schema: 99,
		Sha:    "7159f6b651e49cae47422560120e93ebc494acc9",
		Tree:   "84638b5a25febc78bd8ac7cad517ef4d88764262",
		Change: Change{
			Schema:  DocSchema,
			ID:      "chg-01HZ",
			State:   ChangeExtended,
			Branch:  "dockhand/jq-1.9",
			Tip:     "7159f6b651e49cae47422560120e93ebc494acc9",
			Pin:     "refs/dockhand/pins/chg-01HZ",
			Slug:    "jq-1.9",
			Content: "sha256:9f2c",
			Subjects: []Subject{
				{
					Port:    "jq",
					Names:   []string{"jq"},
					Portdir: "textproc/jq",
					Intent:  "bump",
					Target:  "1.9",
					Reason:  "upstream release",
				},
				{
					Port:    "oniguruma",
					Names:   []string{"oniguruma", "oniguruma-devel"},
					Portdir: "devel/oniguruma",
					Intent:  "bump-revision",
					Target:  "rev2",
					Reason:  "libjq install name moved",
				},
			},
			Destination: ToPublished,
			AskedBy:     Human,
			Agent:       "claude-code",
			MintedVia:   MintedCohort,
			Crossing:    StableToPrerelease,
			Hold: &Hold{
				Origin: HoldCrossing,
				Reason: "leaves stable",
				At:     heldAt,
			},
			Riders: []string{"modeline"},
			Findings: []Finding{{
				Kind:       KindABIDependents,
				Diverged:   []string{"Portfile"},
				Ports:      []string{"oniguruma"},
				Candidates: []Candidate{{Port: "oniguruma", Portdir: "devel/oniguruma", Proposed: true, Reason: "links libjq", Solo: true, Over: "jq", Forced: true}},
				Criterion:  "libjq.1.dylib compat 1.0.0 -> 2.0.0",
				Source:     "NEWS",
				Quote:      "the shared library soname changed",
				// Disposition and At carry no omitempty: a finding with
				// no disposition on the wire would read as one nobody had
				// to answer.
				Disposition: Accepted,
				At:          foundAt,
			}},
			ClosesTicket: "12345",
			SupersededBy: "dockhand/jq-1.9.1",
			Closed:       ptr(publishedAt),
			Base:         Base{Sha: "0ba1c0ffee", CommittedAt: committedAt},
		},
		Runs: map[RunKey]Run{
			{Port: "jq", Platform: "Testos"}: {
				Ask:            Ask{Test: true, KeepEnv: true, FromSource: true, Forced: "oniguruma"},
				State:          Passed,
				Content:        "sha256:9f2c",
				Detail:         "built and installed",
				Blamed:         "",
				Evidence:       "built in a pristine VM",
				Lint:           ptr("2 warnings"),
				Manifest:       &artifact.Manifest{Port: "jq", Version: "1.9", Platform: "Testos", Files: []string{"bin/jq"}, Dylibs: []artifact.Dylib{{Path: "lib/libjq.2.dylib", Arch: "arm64", InstallName: "/opt/local/lib/libjq.2.dylib", CompatVersion: "2.0.0", CurrentVersion: "2.0.1"}}},
				Baseline:       &artifact.Manifest{Port: "jq", Version: "1.8", Platform: "Testos", Files: []string{"bin/jq"}, Dylibs: nil},
				BaselineSource: "binary archive",
				Links:          []string{"bin/jq -> libjq.2.dylib"},
				Probes:         []artifact.Probe{{Binary: "bin/jq", Argv: "jq --version", Output: "jq-1.9"}},
				At:             ranAt,
			},
			{Port: "oniguruma", Platform: "Testos"}: {
				State:   Withheld,
				Content: "sha256:9f2c",
				Detail:  "conflicts with jq in one guest",
				Blamed:  "jq",
			},
			{Port: "jq", Platform: "Ancientos"}: {
				State:   Unsupported,
				Content: "sha256:9f2c",
			},
		},
		Leases: map[string]Lease{
			"Testos": {
				Schema:   DocSchema,
				ID:       LeaseID{Provider: "tart", ID: "vm-7", Started: started},
				Request:  "req-01HZ",
				Handle:   "dockhand-jq-1.9",
				Change:   "chg-01HZ",
				Owner:    OwnerID{Root: "/Users/x/ports", Host: "studio.local", PID: 4821, Since: started},
				Platform: "Testos",
				Phase:    Finished,
				Test:     true,
				TreeAsOf: treeAsOf,
				Claim:    &Claim{By: "cycle-1", At: claimedAt, Expires: expiresAt, Pass: "pass-9"},
				Release: &Release{
					Requested: claimedAt,
					By:        "cycle-1",
					Done:      ptr(releasedAt),
					Attempts:  2,
					LastError: "provider refused once",
					NotBefore: ptr(expiresAt),
				},
				Retain: ptr(retainUntil),
			},
		},
		Publication: &PublicationState{
			Number:      9876,
			URL:         "https://github.com/macports/macports-ports/pull/9876",
			PublishedBy: Machine,
			PublishedAt: publishedAt,
			Unproven:    1,
		},
	}
}

// TestEveryFieldRoundTrips is the pin that matters most: a field the
// codec drops, renames or reorders comes back unequal here whatever the
// wire happens to look like.
func TestEveryFieldRoundTrips(t *testing.T) {
	want := populated()
	// Encode stamps the schema on its own copy, so the round trip is
	// only equal once the fixture's deliberate 99 is corrected.
	want.Schema = Schema
	b, err := Encode(populated())
	require.NoError(t, err)
	got, err := Decode(b, want.Sha)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestEveryFieldIsOnTheWireInOrder pins the bytes. Declaration order is
// wire order, so a field moved in the struct moves here, and this is
// also status --json's public surface.
func TestEveryFieldIsOnTheWireInOrder(t *testing.T) {
	b, err := Encode(populated())
	require.NoError(t, err)
	//nolint:testifylint // not JSONEq: that compares parsed values, and would pass on any key order, any indentation and either escaping — which are the three things this pin exists to hold.
	assert.Equal(t, wire, string(b))
}

func TestTheZeroRecordIsFourKeys(t *testing.T) {
	// Everything else is omitempty or a pointer, so an empty record is
	// the three identities and the change section — which is a struct
	// and therefore always written, because a note with no change is a
	// note about nothing and should be visible as such.
	b, err := Encode(Record{})
	require.NoError(t, err)
	//nolint:testifylint // the bytes are the claim; JSONEq would accept the same four keys spread over any layout.
	assert.Equal(t, "{\n  \"schema\": 4,\n  \"sha\": \"\",\n  \"tree\": \"\",\n  \"change\": {\n    \"schema\": 0,\n    \"id\": \"\",\n    \"state\": \"\",\n    \"content\": \"\"\n  }\n}", string(b))
}

func TestTheNoteSchemaIsFourAndTheDocumentsAreOne(t *testing.T) {
	// Two numbers, two disciplines. The note is a derived export a build
	// that cannot read one clears and regenerates; the state documents
	// are the authority, and DocSchema is a tripwire against
	// encoding/json zero-filling a lease with no owner and calling it
	// read.
	assert.Equal(t, 4, Schema)
	assert.Equal(t, 1, DocSchema)
}

func TestAnUnknownKeyIsReadPast(t *testing.T) {
	// The additive policy, for the NOTE alone: a field appended by a
	// later build is ignored here, and only a change to what an existing
	// key MEANS bumps the number. The state documents get the opposite
	// discipline, which is what DocSchema is for.
	b := []byte(`{"schema":4,"sha":"abc","tree":"def","change":{"id":"chg-1"},"invented":true}`)
	got, err := Decode(b, "abc")
	require.NoError(t, err)
	assert.Equal(t, ChangeID("chg-1"), got.Change.ID)
}
