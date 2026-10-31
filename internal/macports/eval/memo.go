package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
)

// Memo keeps a port's baseline evaluation with what it read, and answers it
// again while all of that is as it was. Which files an evaluation reads is
// one of its outputs, so the memo is a depfile: a slot for each Portfile,
// context, and whole or selected evaluation holds the snapshot and the
// inputs its record named (macports.Input), each with the identity it had,
// and a lookup takes each again and answers only where all match; else the
// port is evaluated and the slot written anew. A look reads most ports
// unchanged since its last, so their evaluation, mportopen, PortGroup
// sourcing, and option evaluation, about three minutes of a warm look over
// 1,114 ports, is skipped (the faster look's step 3; the oracle survey's
// Q2, and Q18, one _resources change moving 32,687 of 32,739 ports'
// options, so reuse holds between such changes and not across them).
//
// The slot also holds the dockhand build that evaluated, its runtime, and
// what mportinit read, since the evaluator's Go side and Base's
// configuration are inputs too. A host program a port ran, clang or perl
// asked where it looks, is an input by its answer, which only reports and
// is asked again once a look; one whose command line can't be asked again
// as it was isn't kept. One slot for each Portfile
// and context bounds the memo to the tree; a torn or unreadable slot is a
// miss, never an error, since dockhands on one Mac share it.
type Memo struct {
	Directory  string
	Identities macports.Identities

	hits, misses, unkept atomic.Int64
}

// memoVersion names the slot's layout.
const memoVersion = 1

// memoRoot stands for the evaluation's root in a kept snapshot's options,
// which name it absolutely, as filespath does.
const memoRoot = "\x00root\x00"

// memoSlot is a kept evaluation.
type memoSlot struct {
	Version   int
	Build     string
	Runtime   macports.Runtime
	Bootstrap string
	Inputs    []macports.Input
	Ports     map[string]macports.PortInfo
}

// MemoCounts are what a memo answered, evaluated and kept, and evaluated
// without keeping, since it was made.
type MemoCounts struct{ Hits, Misses, Unkept int64 }

// Counts says what the memo has answered and evaluated.
func (m *Memo) Counts() MemoCounts {
	return MemoCounts{Hits: m.hits.Load(), Misses: m.misses.Load(), Unkept: m.unkept.Load()}
}

// slotPath is where an evaluation of the selected Portfile in its context
// is kept.
func (m *Memo) slotPath(source macports.Context, runtime macports.Runtime, selectedOnly bool) (string, error) {
	target := source.Target()
	variants := make([]string, 0, len(target.Variants))
	for name, on := range target.Variants {
		sign := "-"
		if on {
			sign = "+"
		}
		variants = append(variants, sign+name)
	}
	sort.Strings(variants)
	key, err := json.Marshal([]any{target.Portfile, target.Name, target.Subport, variants, selectedOnly, runtime.Platform, runtime.Host})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(key)
	name := hex.EncodeToString(sum[:])
	return filepath.Join(m.Directory, name[:2], name+".json"), nil
}

// lookup is the kept snapshot's ports where the slot's evaluation read
// what source would read now; else why not, for a look's debug output.
func (m *Memo) lookup(path string, source macports.Context, runtime macports.Runtime, bootstrap string) (map[string]macports.PortInfo, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "nothing kept"
	}
	var slot memoSlot
	switch {
	case json.Unmarshal(data, &slot) != nil || slot.Version != memoVersion:
		return nil, "its slot is unreadable"
	case slot.Build != buildinfo.Current().String():
		return nil, "kept by dockhand " + slot.Build
	case slot.Runtime != runtime:
		return nil, "kept under another MacPorts runtime"
	case slot.Bootstrap != bootstrap:
		return nil, "MacPorts' configuration changed"
	}
	if changed, ok := m.Identities.Changed(source.Root(), slot.Inputs); ok {
		return nil, changed.Path + " changed"
	}
	return rooted(slot.Ports, memoRoot, source.Root()), ""
}

// store keeps an evaluation of source with its inputs; where it can't, it
// says why: a host program it ran that can't be asked again as it was, or
// no record to say when it changed.
func (m *Memo) store(path string, source macports.Context, runtime macports.Runtime, bootstrap string, observation macports.Observation) string {
	if len(observation.Ports) == 0 {
		return "nothing was recorded"
	}
	var reads []macports.PathRead
	var runs []macports.CommandRun
	for name, port := range observation.Ports {
		if len(port.Reads) == 0 {
			return name + " recorded no reads"
		}
		reads = append(reads, port.Reads...)
		runs = append(runs, port.Execs...)
	}
	inputs, unkept := m.Identities.Inputs(source.Root(), reads, runs)
	if unkept != "" {
		return unkept
	}
	data, err := json.Marshal(memoSlot{Version: memoVersion, Build: buildinfo.Current().String(), Runtime: runtime, Bootstrap: bootstrap, Inputs: inputs, Ports: rooted(observation.Snapshot.Ports, source.Root(), memoRoot)})
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return ""
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".slot-*")
	if err != nil {
		return ""
	}
	_, err = temporary.Write(data)
	if err = errors.Join(err, temporary.Close()); err != nil || os.Rename(temporary.Name(), path) != nil {
		_ = os.Remove(temporary.Name())
	}
	return ""
}

// rooted is ports with from written as to in every option value.
func rooted(ports map[string]macports.PortInfo, from, to string) map[string]macports.PortInfo {
	out := make(map[string]macports.PortInfo, len(ports))
	for name, port := range ports {
		options := make(map[string]string, len(port.Options))
		for option, value := range port.Options {
			options[option] = strings.ReplaceAll(value, from, to)
		}
		port.Options = options
		out[name] = port
	}
	return out
}

// bootstrapDigest is what mportinit read and settled in a recording
// session: Base's configuration, which a port's evaluation stands on.
func bootstrapDigest(ctx context.Context, session *rpc.Session) (string, error) {
	reply, err := session.Call(ctx, "bootstrap_record")
	if err != nil {
		return "", err
	}
	record, err := decodeBootstrap(reply)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	for _, read := range record.Reads {
		_, _ = io.WriteString(sum, read.Command+"\x00"+read.Path+"\x00"+read.Value+"\x00")
	}
	for _, setting := range record.Settings {
		_, _ = io.WriteString(sum, setting.Name+"\x00"+setting.Value+"\x00")
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// memoized is a session's baseline evaluations through its memo.
type memoized struct{ session *Session }

// Memoized is the session's evaluations of a port's baseline, answered
// from the evaluator's memo where it keeps one that holds; the session
// itself where it has none. Only a baseline goes through it: an overlay of
// the same Portfile would overwrite the baseline's slot.
func (s *Session) Memoized() macports.SnapshotEvaluator {
	if s.memo == nil {
		return s
	}
	return memoized{s}
}

func (m memoized) Evaluate(ctx context.Context, source macports.Context) (macports.Snapshot, error) {
	return m.session.remembered(ctx, source, false)
}

func (m memoized) EvaluateSelected(ctx context.Context, source macports.Context) (macports.Snapshot, error) {
	return m.session.remembered(ctx, source, true)
}

// remembered is the port's evaluation from the memo, or evaluated and kept.
func (s *Session) remembered(ctx context.Context, source macports.Context, selectedOnly bool) (macports.Snapshot, error) {
	checked, err := s.bound(source)
	if err != nil {
		return macports.Snapshot{}, err
	}
	if s.bootstrap == "" {
		if s.bootstrap, err = bootstrapDigest(ctx, s.session); err != nil {
			return macports.Snapshot{}, err
		}
	}
	path, err := s.memo.slotPath(checked, s.runtime, selectedOnly)
	if err != nil {
		observation, err := evaluateIn(ctx, s.session, s.runtime, checked, source, nil, selectedOnly, false)
		return observation.Snapshot, err
	}
	ports, why := s.memo.lookup(path, checked, s.runtime, s.bootstrap)
	if why == "" {
		s.memo.hits.Add(1)
		return macports.Snapshot{Source: source.Source(), Target: source.Target(), Platform: s.runtime.Platform, Runtime: s.runtime, Ports: ports, Root: source.Root(), ObservedAt: time.Now().UTC()}, nil
	}
	progress.DebugReport(ctx, "Evaluating %s afresh: %s", source.Target().Portfile, why)
	observation, err := evaluateIn(ctx, s.session, s.runtime, checked, source, nil, selectedOnly, true)
	if err != nil {
		return observation.Snapshot, err
	}
	if unkept := s.memo.store(path, checked, s.runtime, s.bootstrap, observation); unkept != "" {
		s.memo.unkept.Add(1)
		progress.DebugReport(ctx, "Not keeping %s's evaluation: %s", source.Target().Portfile, unkept)
	} else {
		s.memo.misses.Add(1)
	}
	return observation.Snapshot, nil
}
