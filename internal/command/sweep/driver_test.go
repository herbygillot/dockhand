package sweep

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/store"
)

// A scenario is a workflow as the sweep runs it: the world it starts in,
// the command lines that are the workflow, and, beside it, declarations
// rather than code (docs/designs/fault-sweep.md, "Per workflow").
type scenario struct {
	name string
	// arrange makes the world before the workflow, its own command
	// lines included, unswept; it returns the GitHub stand-in.
	arrange func(t *testing.T, w cmdtest.World) *forgetest.GitHub
	// steps are the workflow's command lines, run in turn.
	steps [][]string
	// recover is what a person does after a failure, where it isn't the
	// steps again.
	recover [][]string
	// bestEffort are the effects, by address prefix, whose failure the
	// command may not report, each with why.
	bestEffort map[string]string
	// known are the first run's findings not yet fixed, by address prefix
	// and mode ("address/mode"), each with its issue: the sweep expects
	// them to fail, and fails where one no longer does, so the list only
	// shrinks.
	known map[string]string
	// unreadable are the faults, by "address/mode" prefix, whose outcome
	// dockhand can't read back, and the end state the scenario declares
	// for each: a value in the recovered world, and the clean run's it
	// stands for, each pair with why (docs/designs/publication-record.md,
	// "Tests": a pull request found rather than opened, a reviewer marked
	// "may have been sent").
	unreadable map[string]declaredEnd
	// unresolved are the writes, by address prefix, whose outcome the
	// run may leave unknown with every step succeeding, each with why:
	// elsewhere a write that didn't land and said its outcome was
	// unknown must be made again (#129's fix list, 3).
	unresolved map[string]string
	// required are records that must exist when a remote effect runs,
	// by the effect's boundary, which the final world can't show: a
	// request sent with no mark recorded looks, once later writes land,
	// like one marked (#129's fix list, 3).
	required map[string]requiredRecord
}

// requiredRecord is a record a remote effect needs written before it
// runs: a query of the database at the effect, a text its first row
// holds, and why.
type requiredRecord struct {
	Query, Holds, Why string
}

// declaredEnd is a fault's declared end: Replace are pairs of a recovered
// world's text and the clean run's it stands for, and Why says why it
// can't be read back.
type declaredEnd struct {
	Replace []string
	// Unsaid are the clean run's lines, by a part of each, the recovered
	// world rightly lacks: the timeline's line for a request it can't say
	// was made.
	Unsaid []string
	Why    string
}

// run is one run of a scenario in a world of its own, with a fault
// armed, or none: what it left, and what it did.
type run struct {
	world   world
	effects []effect
	// failed are the steps that returned an error, with it.
	failed []string
	// recovered are the recovery steps that failed, with why.
	recovered []string
	reached   bool
	// steps is how many of effects the steps made, recovery the rest;
	// missing are the required records an effect ran without.
	steps   int
	missing []string
}

// sweepWorld makes a scenario's World with the plan's seams in place:
// the store wrapped, git hooked, and the forge stand-in recording.
func sweepWorld(t *testing.T, sc scenario, p *plan) (cmdtest.World, *forgetest.GitHub) {
	t.Helper()
	w := cmdtest.NewWorld(t)
	g := sc.arrange(t, w)
	stands := cmdtest.Stands(t)
	stands.GitHook = p.gitHook
	stands.WrapStore = func(s store.Store) store.Store { return recordingStore{Store: s, plan: p} }
	stands.Forge = func(*engine.Engine) engine.Forge { return recordingForge{GitHub: g, plan: p} }
	return w, g
}

// stoppedProcess is the process start the steps' sessions are recorded
// under: a process of their own, which ends when they do.
const stoppedProcess = "sweep-stopped-run"

// stoppable says the steps' process gone once they've returned, as a
// killed run's is, and asks the operating system of any other: a stopped
// run's session carried the test's own process, which the next command
// called alive (docs/designs/fault-sweep.md, "dead"; #129's fix list, 4).
type stoppable struct{ over *atomic.Bool }

func (s stoppable) Alive(p engine.SessionProcess) (bool, error) {
	if p.Start == stoppedProcess {
		return !s.over.Load(), nil
	}
	return coord.System{}.Alive(p)
}

// runOnce runs a scenario: its steps, and where a fault was armed, the
// recovery after it, with nothing failing.
func runOnce(t *testing.T, sc scenario, armed *fault) run {
	t.Helper()
	p := newPlan()
	// arrange's command lines are the world, not the workflow: nothing is
	// recorded or failed until the steps.
	w, g := sweepWorld(t, sc, newPlan())
	stands := cmdtest.Stands(t)
	stands.GitHook = p.gitHook
	stands.WrapStore = func(s store.Store) store.Store { return recordingStore{Store: s, plan: p} }
	stands.Forge = func(*engine.Engine) engine.Forge { return recordingForge{GitHub: g, plan: p} }
	// The steps are a process of their own, gone once they return;
	// recovery is this one.
	over := &atomic.Bool{}
	self, err := coord.Self()
	require.NoError(t, err)
	stands.Process = func() (engine.SessionProcess, error) {
		if over.Load() {
			return self, nil
		}
		return engine.SessionProcess{PID: self.PID, Start: stoppedProcess}, nil
	}
	stands.Liveness = stoppable{over: over}
	p.required, p.database = sc.required, filepath.Join(w.Home, ".dockhand", "dockhand.db")
	p.fault = armed
	var r run
	for _, args := range sc.steps {
		if _, errs, err := cmdtest.Dockhand(t, args...); err != nil {
			r.failed = append(r.failed, fmt.Sprintf("%v: %v %s", args, err, strings.TrimSpace(errs)))
		}
	}
	over.Store(true)
	r.reached, r.steps = p.reached, len(p.effects())
	if armed != nil {
		p.disarm()
		recovery := sc.recover
		if recovery == nil {
			recovery = sc.steps
		}
		for _, args := range recovery {
			if _, errs, err := cmdtest.Dockhand(t, args...); err != nil {
				r.recovered = append(r.recovered, fmt.Sprintf("%v: %v %s", args, err, strings.TrimSpace(errs)))
			}
		}
	}
	r.effects, r.missing = p.effects(), p.missingRecords()
	r.world = snapshot(t, w, g)
	return r
}

// madeAgain reports whether a write at an address was made again later
// in the steps, at the same call site: how a write that didn't land is
// resolved by the run that met it.
func madeAgain(r run, address string) bool {
	at := slices.IndexFunc(r.effects, func(e effect) bool { return e.address() == address })
	if at < 0 {
		return false
	}
	site := r.effects[at].Boundary + "@" + r.effects[at].Site
	return slices.ContainsFunc(r.effects[at+1:r.steps], func(e effect) bool { return e.Boundary+"@"+e.Site == site })
}

// unrepeatable are the forge's effects GitHub doesn't take twice as
// once: a second pull request, a second review, a second rerun.
var unrepeatable = []string{"forge.Create", "forge.PostReview", "forge.RerunFailed"}

// sweep runs a scenario clean, then once for each of its effects' addresses
// and each way the effect can fail, and holds each recovered world to the
// generic invariants.
func sweep(t *testing.T, sc scenario) {
	t.Helper()
	clean := runOnce(t, sc, nil)
	require.Empty(t, clean.failed, "the clean run fails")
	require.Empty(t, clean.missing, "the clean run ran an effect without a record it requires")
	var addresses []string
	boundaries := map[string]string{}
	for _, e := range clean.effects {
		if !slices.Contains(addresses, e.address()) {
			addresses = append(addresses, e.address())
			boundaries[e.address()] = e.Boundary
		}
	}
	for prefix := range sc.bestEffort {
		require.True(t, slices.ContainsFunc(addresses, func(a string) bool { return strings.HasPrefix(a, prefix) }), "best-effort %s names no effect the scenario makes", prefix)
	}
	cleanCounts := counts(clean.effects)
	for _, address := range addresses {
		for _, how := range sweptModes(boundaries[address]) {
			t.Run(address+"/"+string(how), func(t *testing.T) {
				t.Parallel()
				r := runOnce(t, sc, &fault{address: address, mode: how})
				require.True(t, r.reached, "the run never reached %s", address)
				var problems []string
				// An unresolved operation says so: a write read back and
				// found is done, which is success.
				if len(r.failed) == 0 && !resolvable(how) && !declared(sc.bestEffort, address) {
					problems = append(problems, fmt.Sprintf("with %s failing (%s), every step succeeded", address, how))
				}
				// An unknown outcome is resolved: a write that didn't land
				// and said so, with every step succeeding, was made again
				// (#129's fix list, 3).
				if how == notLandedUnknown && len(r.failed) == 0 && !madeAgain(r, address) && !declared(sc.bestEffort, address) && !declared(sc.unresolved, address) {
					problems = append(problems, fmt.Sprintf("with %s not landing (its outcome unknown), every step succeeded and the run never made it again", address))
				}
				problems = append(problems, r.missing...)
				// Recovery ends: it finishes, or refuses, and the world is
				// still the clean one, which the comparison below holds it
				// to; a refusal is said beside a difference.
				for _, name := range unrepeatable {
					if happened(r.effects, name) > cleanCounts[name] {
						problems = append(problems, fmt.Sprintf("%s happened %d times, the clean run %d", name, happened(r.effects, name), cleanCounts[name]))
					}
				}
				found := differences(clean.world, r.world)
				for prefix, end := range sc.unreadable {
					if strings.HasPrefix(address+"/"+string(how), prefix) {
						found = settleDeclared(found, end)
					}
				}
				if len(found) > 0 {
					problems = append(problems, "the recovered world differs from the clean run's:\n"+strings.Join(found, "\n"))
					if len(r.recovered) > 0 {
						problems = append(problems, "recovery refused: "+strings.Join(r.recovered, "; "))
					}
				}
				issue, isKnown := knownFinding(sc.known, address, how)
				switch {
				case isKnown && len(problems) == 0:
					t.Errorf("the known finding %s/%s (%s) no longer reproduces; take it off the list", address, how, issue)
				case !isKnown && len(problems) > 0:
					t.Errorf("%s\nto list it, known: %q", strings.Join(problems, "\n"), address+"/"+string(how))
				}
			})
		}
	}
}

// resolvable modes are those the code may resolve by reading back: a
// write whose outcome was unknown, found to have landed, is done, as is
// a remote effect whose reply was lost and which is found to have
// happened, as submit finds a pull request it opened.
func resolvable(how mode) bool { return how == landedUnknown || how == notLandedUnknown || how == lost }

func declared(bestEffort map[string]string, address string) bool {
	for prefix := range bestEffort {
		if strings.HasPrefix(address, prefix) {
			return true
		}
	}
	return false
}

func counts(effects []effect) map[string]int {
	out := map[string]int{}
	for _, e := range effects {
		out[e.Boundary]++
	}
	return out
}

// happened counts a boundary's effects that happened: attempts that
// failed before they happened aren't, a lost reply's are.
func happened(effects []effect, boundary string) int {
	n := 0
	for _, e := range effects {
		if e.Boundary == boundary && !e.Failed {
			n++
		}
	}
	return n
}

// knownFinding is a fault's entry on a scenario's known list, by its
// address's prefix and its mode.
func knownFinding(known map[string]string, address string, how mode) (string, bool) {
	// A key's mode is one mode, none for every mode, or "!mode" for every
	// mode but that one, as a write read back and found resolves.
	for key, issue := range known {
		prefix, wanted, _ := strings.Cut(key, "/")
		matches := wanted == "" || wanted == string(how) || strings.HasPrefix(wanted, "!") && wanted[1:] != string(how)
		if strings.HasPrefix(address, prefix) && matches {
			return issue, true
		}
	}
	return "", false
}

// sweptModes are the modes a run sweeps: every effect failed, in the
// gate; dead, lost and the two unknown modes too with DOCKHAND_SWEEP_FULL
// set, as make sweep sets it, which runs for each release
// (docs/designs/fault-sweep.md, "Cost in suite time").
func sweptModes(boundary string) []mode {
	if os.Getenv("DOCKHAND_SWEEP_FULL") != "" {
		return modes(boundary)
	}
	return []mode{failed}
}

// settleDeclared drops the differences a declared end accounts for: a row
// the recovered world has that, with the declared values put back, is the
// row the clean run has and the recovered lacks.
func settleDeclared(found []string, end declaredEnd) []string {
	const cleanHas, recoveredHas = ": clean has, recovered lacks: ", ": recovered has, clean lacks: "
	put := strings.NewReplacer(end.Replace...)
	missing := map[string]int{}
	for _, line := range found {
		if section, row, ok := strings.Cut(line, cleanHas); ok {
			missing[section+cleanHas+row]++
		}
	}
	var kept, settled []string
	for _, line := range found {
		section, row, ok := strings.Cut(line, recoveredHas)
		if !ok {
			continue
		}
		key := section + cleanHas + put.Replace(row)
		if missing[key] > 0 {
			missing[key]--
			settled = append(settled, key)
			continue
		}
		kept = append(kept, line)
	}
	for _, line := range found {
		if strings.Contains(line, recoveredHas) {
			continue
		}
		if i := slices.Index(settled, line); i >= 0 {
			settled = slices.Delete(settled, i, i+1)
			continue
		}
		if slices.ContainsFunc(end.Unsaid, func(part string) bool { return strings.Contains(line, part) }) {
			continue
		}
		kept = append(kept, line)
	}
	return kept
}
