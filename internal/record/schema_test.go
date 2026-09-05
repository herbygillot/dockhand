package record

import (
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The instants the fixtures carry. They are distinct so that a field
// written into the wrong key is a visible failure rather than a
// coincidence.
var (
	started     = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	claimedAt   = time.Date(2026, 9, 1, 0, 5, 0, 0, time.UTC)
	treeAsOf    = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	foundAt     = time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC)
	heldAt      = time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	committedAt = time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
)

// populated sets every one of the record's eighteen fields and every
// one of a run's fourteen, so that the wire pin below is a statement
// about the whole schema and not about the part of it anything writes
// today.
//
// It is a cohort on two platforms: one job that carries everything and
// one that carries nothing, two subjects, and three runs — enough that
// the two maps disagree about their key sets, which is the shape the
// split exists for.
func populated() Record {
	return Record{
		// Overwritten by Encode. A caller's schema is never trusted.
		Schema: 99,
		Sha:    "7159f6b651e49cae47422560120e93ebc494acc9",
		Tree:   "84638b5a25febc78bd8ac7cad517ef4d88764262",
		Slug:   "jq-1.9",
		Subjects: []Subject{
			{Port: "jq", Names: []string{"jq"}, Portdir: "textproc/jq", Intent: "bump", Target: "1.9"},
			{
				Port:    "oniguruma",
				Names:   []string{"oniguruma", "oniguruma-devel"},
				Portdir: "devel/oniguruma",
				Intent:  "bump-revision",
				Target:  "rev2",
				Reason:  "oniguruma.5.dylib became oniguruma.6.dylib",
			},
		},
		Destination: ToVerdict,
		AskedBy:     Human,
		Agent:       "claude-code",
		MintedVia:   MintedSingle,
		Jobs: map[string]JobRecord{
			"Testos": {
				Job:      verify.Job{Provider: "fake", ID: "fake-1", Started: started},
				Handle:   "dockhand-worker-1",
				Test:     true,
				TreeAsOf: treeAsOf,
				Claim:    &Claim{By: "session-a", At: claimedAt},
				Released: true,
			},
			// Claimed but not yet submitted: the job is a placeholder, so
			// every optional field is absent and tree_as_of is omitted
			// rather than written as the zero instant.
			"Oldos": {},
		},
		Runs: map[string]Run{
			RunKey("jq", "Testos"): {
				State:          Passed,
				Platform:       "Testos",
				Evidence:       "built in a pristine VM",
				Linted:         true,
				Lint:           "2 warnings",
				FromSource:     true,
				KeepEnv:        true,
				Manifest:       &verify.Manifest{Port: "jq", Version: "1.9", Platform: "Testos", Files: []string{"/opt/local/bin/jq"}, Dylibs: []verify.Dylib{{Path: "/opt/local/lib/libjq.1.dylib", Arch: "arm64", InstallName: "/opt/local/lib/libjq.1.dylib", CompatVersion: "1.0.0", CurrentVersion: "1.9.0"}}},
				Baseline:       &verify.Manifest{Port: "jq", Version: "1.8", Platform: "Testos", Files: []string{}, Dylibs: []verify.Dylib{}},
				BaselineSource: "archive",
				Links:          []string{"/opt/local/bin/jq links against /opt/local/lib/libjq.1.dylib"},
				Probes:         []verify.ProbeLine{{Binary: "/opt/local/bin/jq", Argv: "jq --version", Output: "jq-1.9"}},
			},
			// The cohort stopped before this member was reached. The
			// detail carries the three bytes encoding/json escapes, and
			// links is nil — "nobody looked", which the wire spells null.
			RunKey("oniguruma", "Testos"): {
				State:    Blocked,
				Platform: "Testos",
				Detail:   `stopped at jq: "a & b" <not> ok`,
				Blamed:   "jq",
			},
			RunKey("jq", "Oldos"): {
				State:    Queued,
				Platform: "Oldos",
				Detail:   "all 2 verification slots are busy",
			},
		},
		Hold:   &Hold{Reason: "checksums re-witnessed and differ", At: heldAt},
		Riders: []string{"modeline"},
		Findings: []Finding{{
			Kind:  "abi-change",
			Ports: []string{"jq"},
			Candidates: []Candidate{
				{Port: "oniguruma", Portdir: "devel/oniguruma", Proposed: true, Reason: "depends_lib"},
				{Port: "jq-devel", Reason: "already in flight"},
			},
			Criterion:   "compatibility_version 1.0.0 to 2.0.0, measured on Testos",
			Source:      "https://example.invalid/jq/NEWS",
			Quote:       "libjq's soname changed",
			Disposition: Proposed,
			At:          foundAt,
		}},
		ClosesTicket: "12345",
		SupersededBy: "dockhand/jq-1.10",
		Base:         Base{Sha: "0ddba11deadbeef0ddba11deadbeef0ddba11dea", CommittedAt: committedAt},
		Evidence:     &Measured{From: "c0ffee0c0ffee0c0ffee0c0ffee0c0ffee0c0ffe"},
	}
}

// wirePopulated is schema 3, entire. Everything it demonstrates is
// load-bearing:
//
//   - the eighteen top-level keys in declaration order, which is the
//     order encoding/json emits and therefore the wire;
//   - two-space indent and no trailing newline (git adds one);
//   - map keys sorted by encoding/json — Oldos before Testos, and
//     jq@Oldos before jq@Testos before oniguruma@Testos, not the order
//     they were written in;
//   - "state" and "platform" on every run and "links" on every run,
//     including the null that says nobody looked;
//   - "tree_as_of" omitted for the zero instant (omitzero) while
//     "started" inside a zero job is written in full, because Job's own
//     tags predate this schema and are not being moved;
//   - lowercase keys throughout the manifest and probe values, whose Go
//     names would otherwise have become the wire;
//   - HTML escaping left on, so a detail's ampersand and angle brackets
//     come out as &, < and >;
//   - the schema stamped 3 over the 99 the caller supplied.
const wirePopulated = `{
  "schema": 3,
  "sha": "7159f6b651e49cae47422560120e93ebc494acc9",
  "tree": "84638b5a25febc78bd8ac7cad517ef4d88764262",
  "slug": "jq-1.9",
  "subjects": [
    {
      "port": "jq",
      "names": [
        "jq"
      ],
      "portdir": "textproc/jq",
      "intent": "bump",
      "target": "1.9"
    },
    {
      "port": "oniguruma",
      "names": [
        "oniguruma",
        "oniguruma-devel"
      ],
      "portdir": "devel/oniguruma",
      "intent": "bump-revision",
      "target": "rev2",
      "reason": "oniguruma.5.dylib became oniguruma.6.dylib"
    }
  ],
  "destination": "verdict",
  "asked_by": "human",
  "agent": "claude-code",
  "minted_via": "single",
  "jobs": {
    "Oldos": {
      "job": {
        "provider": "",
        "id": "",
        "started": "0001-01-01T00:00:00Z"
      }
    },
    "Testos": {
      "job": {
        "provider": "fake",
        "id": "fake-1",
        "started": "2026-09-01T00:00:00Z"
      },
      "handle": "dockhand-worker-1",
      "test": true,
      "tree_as_of": "2026-08-30T12:00:00Z",
      "claim": {
        "by": "session-a",
        "at": "2026-09-01T00:05:00Z"
      },
      "released": true
    }
  },
  "runs": {
    "jq@Oldos": {
      "state": "queued",
      "platform": "Oldos",
      "detail": "all 2 verification slots are busy",
      "links": null
    },
    "jq@Testos": {
      "state": "passed",
      "platform": "Testos",
      "evidence": "built in a pristine VM",
      "linted": true,
      "lint": "2 warnings",
      "from_source": true,
      "keep_env": true,
      "manifest": {
        "port": "jq",
        "version": "1.9",
        "platform": "Testos",
        "files": [
          "/opt/local/bin/jq"
        ],
        "dylibs": [
          {
            "path": "/opt/local/lib/libjq.1.dylib",
            "arch": "arm64",
            "install_name": "/opt/local/lib/libjq.1.dylib",
            "compat_version": "1.0.0",
            "current_version": "1.9.0"
          }
        ]
      },
      "baseline": {
        "port": "jq",
        "version": "1.8",
        "platform": "Testos",
        "files": [],
        "dylibs": []
      },
      "baseline_source": "archive",
      "links": [
        "/opt/local/bin/jq links against /opt/local/lib/libjq.1.dylib"
      ],
      "probes": [
        {
          "binary": "/opt/local/bin/jq",
          "argv": "jq --version",
          "output": "jq-1.9"
        }
      ]
    },
    "oniguruma@Testos": {
      "state": "blocked",
      "platform": "Testos",
      "detail": "stopped at jq: \"a \u0026 b\" \u003cnot\u003e ok",
      "blamed": "jq",
      "links": null
    }
  },
  "hold": {
    "reason": "checksums re-witnessed and differ",
    "at": "2026-09-01T02:00:00Z"
  },
  "riders": [
    "modeline"
  ],
  "findings": [
    {
      "kind": "abi-change",
      "ports": [
        "jq"
      ],
      "candidates": [
        {
          "port": "oniguruma",
          "portdir": "devel/oniguruma",
          "proposed": true,
          "reason": "depends_lib"
        },
        {
          "port": "jq-devel",
          "reason": "already in flight"
        }
      ],
      "criterion": "compatibility_version 1.0.0 to 2.0.0, measured on Testos",
      "source": "https://example.invalid/jq/NEWS",
      "quote": "libjq's soname changed",
      "disposition": "proposed",
      "at": "2026-09-01T01:00:00Z"
    }
  ],
  "closes_ticket": "12345",
  "superseded_by": "dockhand/jq-1.10",
  "base": {
    "sha": "0ddba11deadbeef0ddba11deadbeef0ddba11dea",
    "committed_at": "2026-08-31T09:00:00Z"
  },
  "evidence": {
    "from": "c0ffee0c0ffee0c0ffee0c0ffee0c0ffee0c0ffe"
  }
}`

func TestEveryFieldIsOnTheWireInOrder(t *testing.T) {
	got, err := Encode(populated())
	require.NoError(t, err)
	//nolint:testifylint // not JSONEq: that compares parsed values, and would pass on any key order, any indentation and either escaping — which are the three things this pin exists to hold.
	assert.Equal(t, wirePopulated, string(got))
}

func TestEveryFieldRoundTrips(t *testing.T) {
	// Every field set, written, read back, and equal — the property the
	// wire pin above cannot state on its own, since a key emitted and
	// never read would still match the bytes.
	r := populated()
	b, err := Encode(r)
	require.NoError(t, err)
	got, err := Decode(b, r.Sha)
	require.NoError(t, err)

	want := populated()
	want.Schema = Schema
	assert.Equal(t, want, got)
}

func TestTheZeroRecordIsThreeKeys(t *testing.T) {
	// Every optional field is omitempty or omitzero, so a record that
	// says nothing writes nothing beyond its identity. The note on a
	// branch minted with --no-verify is close to this shape.
	got, err := Encode(Record{})
	require.NoError(t, err)
	//nolint:testifylint // the bytes are the claim; JSONEq would accept the same three keys spread over any layout.
	assert.Equal(t, "{\n  \"schema\": 3,\n  \"sha\": \"\",\n  \"tree\": \"\"\n}", string(got))
}

func TestAnUnknownKeyInsideSchemaThreeIsIgnored(t *testing.T) {
	// The additive policy, stated as a test. A field a later build
	// appends is read past by this one; only a change to what an
	// existing key MEANS bumps the number.
	const sha = "7159f6b651e49cae47422560120e93ebc494acc9"
	body := `{"schema":3,"sha":"` + sha + `","tree":"84638b5","slug":"jq-1.9",` +
		`"provenance":{"who":"someone","when":"later"},"riders":["modeline"]}`
	got, err := Decode([]byte(body), sha)
	require.NoError(t, err)
	assert.Equal(t, "jq-1.9", got.Slug)
	assert.Equal(t, []string{"modeline"}, got.Riders, "the keys it does know are still read")
}

func TestAnOlderSchemaIsRefusedWithTheRemedy(t *testing.T) {
	const sha = "7159f6b651e49cae47422560120e93ebc494acc9"
	for _, tc := range []struct {
		name string
		body string
	}{
		{"schema 2 — the shape that was on disk", `{"schema":2,"sha":"` + sha +
			`","tree":"84638b5","port":"jq","runs":{"Testos":{"state":"passed"}}}`},
		{"schema 1 — one flat verdict, and no lift left to take it",
			`{"schema":1,"sha":"` + sha + `","port":"jq","platform":"Testos","state":"passed"}`},
		{"no schema key at all", `{"sha":"` + sha + `","port":"jq"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode([]byte(tc.body), sha)
			require.ErrorIs(t, err, ErrSchemaTooOld)
			assert.Contains(t, err.Error(),
				"`git notes --ref="+NotesRef+" remove "+sha+"` discards it",
				"the remedy names the discard")
			assert.Contains(t, err.Error(), "`dockhand verify <branch>` re-earns it",
				"and the re-mint, because a discarded note leaves a branch that looks unverified")
		})
	}
}

func TestAnOlderSchemaIsRefusedEvenWhenItsShaMatchesNothing(t *testing.T) {
	// The schema is checked before the sha. An old note is unreadable
	// whatever commit it names, and the remedy is the same either way;
	// answering it with a sentence about corruption would send a user
	// looking for a copied note that does not exist.
	const sha = "7159f6b651e49cae47422560120e93ebc494acc9"
	_, err := Decode([]byte(`{"schema":2,"sha":"ffff","port":"jq"}`), sha)
	require.ErrorIs(t, err, ErrSchemaTooOld)
	assert.NotErrorIs(t, err, ErrShaMismatch)
}

func TestOneGuestSharedByManySubjectsIsReleasedOnce(t *testing.T) {
	// The point of two maps. Three subjects verified on one platform is
	// ONE environment: one job, one handle, one Released flag — and
	// three verdicts, because each subject passes or fails on its own.
	r := Record{
		Subjects: []Subject{{Port: "libwidget"}, {Port: "widget-tools"}, {Port: "py-widget"}},
		Jobs: map[string]JobRecord{"Testos": {
			Job:    verify.Job{Provider: "fake", ID: "fake-1", Started: started},
			Handle: "dockhand-worker-1",
		}},
		Runs: map[string]Run{
			RunKey("libwidget", "Testos"):    {State: Passed, Platform: "Testos"},
			RunKey("widget-tools", "Testos"): {State: Passed, Platform: "Testos"},
			RunKey("py-widget", "Testos"):    {State: Failed, Platform: "Testos"},
		},
	}

	require.Len(t, r.Jobs, 1, "one guest")
	require.Len(t, r.Runs, 3, "three verdicts on it")
	assert.Equal(t, []string{"Testos"}, r.Platforms(),
		"the platform count is the job count; projecting the runs would answer three")

	// Releasing is one assignment on the job, whatever the runs say. A
	// Released flag per run is not spellable, which is the guarantee:
	// nothing can release the same guest twice, and nothing can release
	// it when the first of three subjects finishes.
	job := r.Jobs["Testos"]
	job.Released = true
	r.Jobs["Testos"] = job

	b, err := Encode(r)
	require.NoError(t, err)
	got, err := Decode(b, "")
	require.NoError(t, err)
	assert.True(t, got.Jobs["Testos"].Released)
	assert.Equal(t, "dockhand-worker-1", got.Jobs["Testos"].Handle,
		"the handle is the guest's, not any one subject's")
}

func TestRunKeyNamesASubjectOnAPlatform(t *testing.T) {
	// The key is joined and never quoted, because neither half can
	// carry an "@": a port name is [A-Za-z0-9._+-] and a release name is
	// Apple's marketing word.
	assert.Equal(t, "jq@Sequoia", RunKey("jq", "Sequoia"))
	assert.Equal(t, "py312-lxml@Big Sur", RunKey("py312-lxml", "Big Sur"))
}
