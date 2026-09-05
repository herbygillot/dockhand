package cmd

// The write-intent catalogue: what the three verbs offer a terminal,
// pinned against the table they are now built from.
//
// A registry is only worth having if it produces exactly what the hand
// -written constructors produced, so this file states the surface —
// name, aliases, one-line help, every flag on every verb, and which
// verbs go to the network or carry a caution — as data rather than as
// three assertions about three constructors. What moved and what did
// not is then a diff of a table.

import (
	"os/exec"
	"sort"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// sharedIntentFlags are the realization flags every intent has, plus
// the ticket every intent may close. They are declared once by
// intentCommand and register, and a verb that lost one would be a verb
// whose --plan or --in-place silently stopped existing.
// The rider pair is shared for the same reason the rest are: every
// headline intent is examined, so a verb whose --riders was missing
// would be a verb whose housekeeping silently could not be asked for.
//
// --replace is here and --recheck is not, which is the whole of the
// S10 split stated as data: what to do about a branch already in
// flight is every intent's question, and re-deriving a port at the
// version it already carries is bump's alone. One flag named --force
// answered both.
var sharedIntentFlags = []string{
	"closes", "diff", "in-place", "keep-env", "no-riders", "no-verify", "on", "plan", "replace",
	"riders", "test", "to-pr", "trace", "verify",
}

// intentSurface is what each verb shows a user.
var intentSurface = []struct {
	name    string
	aliases []string
	short   string
	own     []string // the verb's own flags, beyond the shared set
	fetches bool
	caution string
}{
	{
		name:    "bump",
		short:   "Bump a port to a new version, as a branch",
		own:     []string{"latest", "recheck", "to"},
		fetches: true,
	},
	{
		name:    "bump-revision",
		aliases: []string{"revbump"},
		short:   "Increment a port's revision (requires --reason)",
		// --for is the plural invocation: it names a branch whose revbump
		// proposal is being accepted, and takes no port. It is bump-
		// revision's own because the edit is bump-revision's edit — what
		// changes is who chose the ports and who wrote the reason.
		//
		// --exclude belongs to --for and to nothing else: it names
		// members of the proposal being accepted, so there is no reading
		// of it on the single-port road.
		own: []string{"exclude", "for", "reason"},
	},
	{
		name:    "refresh-checksums",
		aliases: []string{"refresh"},
		short:   "Re-fetch a port's distfiles and repair its recorded checksums",
		fetches: true,
		caution: refreshCaution,
	},
}

func TestIntentCatalogueBuildsTheVerbsItDeclares(t *testing.T) {
	cmds := intentCommands()
	require.Len(t, cmds, len(intentSurface))
	verbs := intentCatalogue()

	for i, want := range intentSurface {
		c, v := cmds[i], verbs[i]
		t.Run(want.name, func(t *testing.T) {
			assert.Equal(t, want.name, c.Name(), "registration order is display order")
			assert.Equal(t, want.aliases, c.Aliases)
			assert.Equal(t, want.short, c.Short)
			assert.Equal(t, want.name+" "+intentArgSketch, c.Use,
				"every intent takes one selector, and says so the same way")

			var names []string
			c.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
			sort.Strings(names)
			assert.Equal(t, sortedUnion(sharedIntentFlags, want.own), names)

			assert.Equal(t, want.fetches, v.Fetches,
				"a verb that reads the network acquires a fetcher; one that does not must not open a session for nothing")
			assert.Equal(t, want.caution, v.Caution)
			require.NotNil(t, v.New, "a registration with no planner is a verb that panics when it is typed")
			planner, err := v.New(intent.Params{Version: "1.0", Reason: "r"})
			require.NoError(t, err)
			assert.NotNil(t, planner)
		})
	}
}

// The registration is data, and the data is what root.go registers:
// a verb declared in the catalogue and never reachable would be the
// registry's one interesting failure.
func TestIntentCatalogueIsWhatTheRootRegisters(t *testing.T) {
	root := Root("test")
	for _, want := range intentSurface {
		for _, spelling := range append([]string{want.name}, want.aliases...) {
			c, _, err := root.Find([]string{spelling})
			require.NoError(t, err, spelling)
			assert.Equal(t, want.name, c.Name(), "%q reaches the verb", spelling)
			assert.Equal(t, "intent", c.GroupID)
		}
	}
}

// Each verb's own flags belong to its own command and to no other: the
// catalogue is a slice of values, and a --to that leaked onto
// refresh-checksums would be a shared variable pretending to be data.
func TestIntentVerbsDoNotShareEachOthersFlags(t *testing.T) {
	root := Root("test")
	refresh, _, err := root.Find([]string{"refresh-checksums"})
	require.NoError(t, err)
	assert.Nil(t, refresh.Flags().Lookup("to"))
	assert.Nil(t, refresh.Flags().Lookup("reason"))

	// Two command trees in one process are two sets of flag storage.
	// Parsing --to on one must not be visible from the other.
	first, _, err := Root("test").Find([]string{"bump"})
	require.NoError(t, err)
	require.NoError(t, first.Flags().Set("to", "9.9"))
	second, _, err := Root("test").Find([]string{"bump"})
	require.NoError(t, err)
	assert.Empty(t, second.Flags().Lookup("to").Value.String())
}

// --closes becomes a URL in a commit message, so it is held to a ticket
// number at the boundary rather than interpolated into nonsense later.
func TestClosesTakesATicketNumber(t *testing.T) {
	for _, in := range []string{"12345", "#12345"} {
		n, err := checkTicket(in)
		require.NoError(t, err, in)
		assert.Equal(t, "12345", n, "the hash a hand types is dropped")
	}
	empty, err := checkTicket("")
	require.NoError(t, err)
	assert.Empty(t, empty)

	for _, in := range []string{
		"https://trac.macports.org/ticket/12345",
		"see the PR",
		"12345a",
		"#",
		"12 345",
	} {
		_, err := checkTicket(in)
		assert.Equal(t, exitcode.Usage, ExitCode(err), "%q is not a ticket number", in)
	}
}

// The check is wired into every intent, not just the one it was written
// for: the flag is shared, so its refusal is too.
func TestEveryIntentRejectsANonTicketCloses(t *testing.T) {
	for _, want := range intentSurface {
		args := []string{want.name, "--closes", "trac-12345"}
		if want.name == "bump-revision" {
			args = append(args, "--reason", "openssl soname moved")
		}
		assert.Equal(t, exitcode.Usage, code(t, append(args, "someport")...), want.name)
	}
}

// The whole chain, through the command tree the user types into:
// --closes on an intent verb becomes a Closes: trailer on the commit
// the mint writes. Every link between them — the flag, the params, the
// planner, the plan's own field, the realizer's composition — is
// exercised by running the verb.
//
// --no-verify because a trailer is not a verdict, and the transcript
// must not depend on whether the machine running it can boot a VM.
func TestBumpClosesReachesTheMintedCommit(t *testing.T) {
	testenv.PortTclsh(t)
	portdir := goldenPortRepo(t)
	tr := captureExecute(t, "bump", "--to", "2.0", "--no-verify", "--closes", "71234", portdir)
	require.Equal(t, 0, tr.exit, tr.render())

	repo, err := git.Open(t.Context(), testFinder(), portdir)
	require.NoError(t, err)
	tip, err := repo.RevParse(t.Context(), "dockhand/bumpee-2.0")
	require.NoError(t, err)
	out, err := exec.Command("git", "-C", repo.Root, "log", "-1", "--format=%B", tip).Output()
	require.NoError(t, err)
	assert.Equal(t, "bumpee: update to 2.0\n\nCloses: https://trac.macports.org/ticket/71234\n", string(out))
}

func sortedUnion(a, b []string) []string {
	out := append(append([]string{}, a...), b...)
	sort.Strings(out)
	return out
}
