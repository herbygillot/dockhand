package plan

// The memoizability rule: which declines may be remembered, and which
// may never be. The completeness sweeps use the taxonomy's own idiom —
// walk until String stops naming members — so a type added later is
// caught here rather than shipping with no ruling and no way back from
// its code.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// beyond is where the taxonomy ends, the way decline_test.go finds it.
const beyond = "unknown decline"

// A kind with no ruling must not be storable, and the fallthrough is
// what guarantees it: an eleventh type that nobody classified answers
// ByNetwork and stores nothing.
func TestEveryDeclineTypeRulesOnItsDeterminacy(t *testing.T) {
	require.Equal(t, ByNetwork, DeclineType(1000).Determinacy(),
		"a kind outside the taxonomy stores nothing")
	require.False(t, (&Decline{Type: DeclineType(1000), Determined: ByPortfile}).Memoizable(),
		"and no producer can talk it into being stored")

	counts := map[Determinacy]int{}
	for dt := AlreadyCurrent; dt.String() != beyond; dt++ {
		d := dt.Determinacy()
		assert.Contains(t, []Determinacy{Unstated, ByPortfile, ByNetwork}, d, "%s", dt)
		assert.NotEmpty(t, d.String(), "%s", dt)
		assert.NotEqual(t, "unknown determinacy", d.String(), "%s falls through", dt)
		counts[d]++
	}
	assert.Equal(t, map[Determinacy]int{ByPortfile: 6, Unstated: 3, ByNetwork: 2}, counts,
		"the rulings are the contract; moving one is a change to what may be remembered")
}

// The code is what a memo stores and what reads one back, so the two
// directions have to agree over the whole taxonomy.
func TestDeclineCodeRoundTrips(t *testing.T) {
	for dt := AlreadyCurrent; dt.String() != beyond; dt++ {
		got, ok := DeclineTypeFor(dt.Code())
		assert.True(t, ok, "%s: %q maps to nothing", dt, dt.Code())
		assert.Equal(t, dt, got, "%s round-trips to the wrong type", dt)
	}
	_, ok := DeclineTypeFor("unknown-decline")
	assert.False(t, ok, "the fallthrough token is not a member")
	_, ok = DeclineTypeFor("already-current-withheld")
	assert.False(t, ok, "the withheld code names a decline's shape, not a type")
	_, ok = DeclineTypeFor("")
	assert.False(t, ok)
}

// The network decline is refused from every direction. This is the gate
// the memo exists to pass: a judgment about what a forge published must
// never be replayed from a store nothing upstream can invalidate.
func TestNetworkDeterminedDeclinesAreNeverMemoizable(t *testing.T) {
	assert.False(t, (&Decline{Type: LatestUnresolved}).Memoizable())
	assert.False(t, (&Decline{Type: LatestUnresolved, Determined: ByPortfile}).Memoizable(),
		"the type's ruling is a ceiling; a producer may narrow it and never widen it")

	// A patch that would not relocate was looked for in what a server
	// served, against a patch file the key does not hash. A maintainer
	// who rewrote the patch, or an upstream that re-rolled the tarball,
	// has changed the answer without moving the Portfile.
	assert.False(t, (&Decline{Type: PatchWontRelocate}).Memoizable())
	assert.False(t, (&Decline{Type: PatchWontRelocate, Determined: ByPortfile}).Memoizable(),
		"no producer relocates without a fetch, so none may claim otherwise")

	// refresh-checksums' own already-current is the dangerous one: its
	// cause is a fetch, and the supply-chain event it exists to catch is
	// the one where the Portfile's bytes do not move.
	refreshSaysCurrent := &Decline{
		Type:       AlreadyCurrent,
		Detail:     "recorded checksums match what upstream serves",
		Determined: ByNetwork,
	}
	assert.False(t, refreshSaysCurrent.Memoizable())
}

// A type whose producers disagree stores nothing until a producer says
// which one this is.
func TestUnstatedTypesNeedTheProducerToSpeak(t *testing.T) {
	for _, dt := range []DeclineType{AlreadyCurrent, FetchNotDriven, VendoredBlock} {
		require.Equal(t, Unstated, dt.Determinacy(), "%s", dt)
		assert.False(t, (&Decline{Type: dt}).Memoizable(), "%s: silence stores nothing", dt)
		assert.False(t, (&Decline{Type: dt, Determined: ByNetwork}).Memoizable(), "%s", dt)
		assert.True(t, (&Decline{Type: dt, Determined: ByPortfile}).Memoizable(),
			"%s: a producer that knows may say so", dt)
	}
}

// A type every producer of which reads only the port is storable
// without ceremony, and a producer that knows better can still veto.
func TestPortfileTypesAreStorableUnlessTheProducerObjects(t *testing.T) {
	for _, dt := range []DeclineType{
		TransformedStyle, ChecksumsNotLocated, SubportsChanged,
		TargetNotReached, UnexpectedChange, RevisionShapeAmbiguous,
	} {
		require.Equal(t, ByPortfile, dt.Determinacy(), "%s", dt)
		assert.True(t, (&Decline{Type: dt}).Memoizable(), "%s", dt)
		assert.True(t, (&Decline{Type: dt, Determined: ByPortfile}).Memoizable(), "%s", dt)
		assert.False(t, (&Decline{Type: dt, Determined: ByNetwork}).Memoizable(),
			"%s: a producer's veto is believed", dt)
	}
}

// What a sweep held back is a function of the run's rider policy, and
// the memo's key does not name one — so a decline carrying riders is
// refused however it is otherwise classified.
func TestWithheldRidersAreNeverMemoizable(t *testing.T) {
	d := &Decline{Type: SubportsChanged, Withheld: []string{"modeline"}}
	require.Equal(t, ByPortfile, d.Type.Determinacy())
	assert.False(t, d.Memoizable(), "the riders are not in the key")

	d.Withheld = nil
	assert.True(t, d.Memoizable(), "and without them it is the ordinary case")
}

// The wire word is what a refusal's sentence names.
func TestDeterminacyNamesItself(t *testing.T) {
	assert.Equal(t, "unstated", Unstated.String())
	assert.Equal(t, "portfile-determined", ByPortfile.String())
	assert.Equal(t, "network-determined", ByNetwork.String())
	assert.Equal(t, "unknown determinacy", Determinacy(9).String())
}

// byPortfileProducers is the reviewed census of every place in the tree
// that raises a decline of a ByPortfile kind.
//
// It exists because the two gates are not symmetric, and the asymmetry
// is easy to forget. On an Unstated kind nothing is stored until a
// producer says ByPortfile, so a new producer of one is safe by
// default. On a ByPortfile kind the type's ruling IS the consent — a
// producer that says nothing is memoized — so a new producer is
// memoized silently unless its author remembers a field the compiler
// never asks about. The file's own defence, that an eleventh kind does
// not compile until it has ruled, protects new TYPES and does nothing
// for new PRODUCERS of existing ones, which is the likelier change.
//
// refresh-checksums is the worked example and the reason this is a test
// rather than a comment. Its share of these is raised by intent.Finish's
// guards, which for that verb are reachable only through the network —
// so they are stamped ByNetwork through FinishOpts.Determined rather
// than inheriting the type's ruling. A producer added beside them
// without that thought would be a memo that suppresses a fetch.
//
// The counts are per file and deliberately exact: a site added to a file
// that already produces the kind is exactly the change that needs
// reviewing.
var byPortfileProducers = map[DeclineType]map[string]int{
	TransformedStyle: {
		"internal/intent/bumprevision/bumprevision.go": 1,
	},
	ChecksumsNotLocated: {
		"internal/intent/bump/bump.go":       2,
		"internal/intent/bump/checksums.go":  3,
		"internal/intent/refresh/refresh.go": 4,
	},
	SubportsChanged: {
		"internal/intent/guard.go": 1,
	},
	TargetNotReached: {
		"internal/intent/bump/bump.go":                 1,
		"internal/intent/bumprevision/bumprevision.go": 1,
	},
	UnexpectedChange: {
		"internal/intent/bump/bump.go": 1,
		"internal/intent/guard.go":     2,
	},
	RevisionShapeAmbiguous: {
		"internal/intent/bumprevision/bumprevision.go": 1,
	},
}

// A producer of a memoizable kind is a reviewed thing.
func TestByPortfileKindsHaveNoUnreviewedProducers(t *testing.T) {
	found := scanDeclineProducers(t)
	for kind, want := range byPortfileProducers {
		require.Equal(t, ByPortfile, kind.Determinacy(),
			"%s is in the census and is no longer ByPortfile; move or remove its entry", kind)
		assert.Equal(t, want, found[kind],
			"the producers of %s moved. A decline of a ByPortfile kind is memoized unless its producer says otherwise, so a new site has to say what decided it — plan.ByNetwork where a fetch or a forge did — and then be recorded here.", kind)
	}
	// The other direction: a kind that BECAME ByPortfile without being
	// reviewed here would be memoized by every one of its producers.
	for kind, sites := range found {
		if kind.Determinacy() != ByPortfile {
			continue
		}
		assert.Contains(t, byPortfileProducers, kind,
			"%s is ByPortfile and has %d unreviewed producer file(s)", kind, len(sites))
	}
}

// scanDeclineProducers reads the tree for decline literals: which kind,
// in which file, how many times.
//
// A scan of the source and not a registry the producers write to,
// because a registry is a thing an author can forget to join and the
// whole point is to catch the author who forgot. The literals are
// uniform — every producer in the tree writes plan.Decline{Type:
// plan.X, or, inside this package's own tests, Decline{Type: X — so
// the pattern is a fact about the code rather than a convention this
// asks for.
func scanDeclineProducers(t *testing.T) map[DeclineType]map[string]int {
	t.Helper()
	root := moduleRoot(t)
	pat := regexp.MustCompile(`plan\.Decline\{\s*Type:\s*plan\.(\w+)`)
	found := map[DeclineType]map[string]int{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() == "testdata":
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go"):
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		for _, m := range pat.FindAllStringSubmatch(string(body), -1) {
			kind, ok := declineTypeNamed(m[1])
			if !ok {
				// A Type built from a variable rather than named — the
				// memo's own decoder does this — is not a producer.
				continue
			}
			if found[kind] == nil {
				found[kind] = map[string]int{}
			}
			found[kind][filepath.ToSlash(rel)]++
		}
		return nil
	})
	require.NoError(t, err)
	return found
}

// declineTypeNamed maps a constant's Go name to its type. It is spelled
// out rather than derived, because the tree holds no name table and one
// invented here would be a second taxonomy to keep in step.
func declineTypeNamed(name string) (DeclineType, bool) {
	switch name {
	case "AlreadyCurrent":
		return AlreadyCurrent, true
	case "TransformedStyle":
		return TransformedStyle, true
	case "FetchNotDriven":
		return FetchNotDriven, true
	case "ChecksumsNotLocated":
		return ChecksumsNotLocated, true
	case "SubportsChanged":
		return SubportsChanged, true
	case "TargetNotReached":
		return TargetNotReached, true
	case "UnexpectedChange":
		return UnexpectedChange, true
	case "LatestUnresolved":
		return LatestUnresolved, true
	case "VendoredBlock":
		return VendoredBlock, true
	case "RevisionShapeAmbiguous":
		return RevisionShapeAmbiguous, true
	case "PatchWontRelocate":
		return PatchWontRelocate, true
	}
	return 0, false
}

// moduleRoot is the directory holding go.mod, found by walking up from
// this package.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "no go.mod above %s", dir)
		dir = parent
	}
}
