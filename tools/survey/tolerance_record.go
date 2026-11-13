package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/model"
)

func compress(data []byte) ([]byte, error) {
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	// A corrupt capture must not expand without a bound.
	b, err := io.ReadAll(io.LimitReader(r, 256<<20+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 256<<20 {
		return nil, fmt.Errorf("capture exceeds 256 MiB")
	}
	return b, nil
}

func (r *Record) tolerance(tx *sql.Tx, id int64, o observed) error {
	c := o.Capture
	if c == nil {
		return nil
	}
	c.Evaluation = id
	pairJSON, err := json.Marshal(c.Pair)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`insert into tolerance_context(evaluation_id,on_verdict,off_verdict,on_left,off_left,parity,complete,pair) values (?,?,?,?,?,?,?,?)`, id, c.Pair.On.Inconclusive, c.Pair.Off.Inconclusive, c.Pair.OnLeft, c.Pair.OffLeft, c.Pair.Parity, c.Pair.Complete, string(pairJSON)); err != nil {
		return err
	}
	if len(c.Pair.On.Accesses) != len(c.Pair.Off.Accesses) {
		return fmt.Errorf("measurement access counts differ")
	}
	for i, a := range c.Pair.On.Accesses {
		b := c.Pair.Off.Accesses[i]
		if a.Declaration != b.Declaration {
			return fmt.Errorf("measurement access identities differ")
		}
		on, err := json.Marshal(a.Followers)
		if err != nil {
			return err
		}
		off, err := json.Marshal(b.Followers)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`insert into tolerance_access(evaluation_id,declaration_ordinal,on_rule,off_rule,on_followers,off_followers) values (?,?,?,?,?,?)`, id, a.Declaration, a.Rule, b.Rule, string(on), string(off)); err != nil {
			return err
		}
	}
	var compressionNanos int64
	for _, file := range c.Files {
		if r.toleranceFiles[file.Hash] {
			continue
		}
		started := time.Now()
		packed, err := compress(file.Data)
		compressionNanos += time.Since(started).Nanoseconds()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`insert or ignore into tolerance_file(hash,payload,raw_bytes) values (?,?,?)`, file.Hash, packed, len(file.Data)); err != nil {
			return err
		}
		r.toleranceFiles[file.Hash] = true
	}
	started := time.Now()
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	packed, err := compress(raw)
	if err != nil {
		return err
	}
	compressionNanos += time.Since(started).Nanoseconds()
	_, err = tx.Exec(`insert into tolerance_capture(evaluation_id,hash,payload,raw_bytes,analysis_ns,capture_ns,compression_ns) values (?,?,?,?,?,?,?)`, id, digest(raw), packed, len(raw), c.AnalysisNanos, c.CaptureNanos, compressionNanos)
	return err
}

type toleranceMention struct {
	File string
	Line int
	Text string
	Hash string
}
type toleranceChange struct {
	Evaluation               int64
	Port                     string
	Platform                 model.Platform
	Modeled                  bool
	On, Off, OnLeft, OffLeft bool
	Mentions                 []toleranceMention
}

type replayTotals struct {
	Ports, FlippingPorts int
	Changes              []toleranceChange `json:",omitempty"`

	Contexts, Parity, Flips, OnLeft, OffLeft int
	Incomplete                               int
	CaptureBytes, FileBytes                  int64
}

// replayTolerance opens only the selected measurement DB and creates
// temporary files from its captured bytes. It never evaluates a Portfile.
func replayTolerance(ctx context.Context, file string) (replayTotals, error) {
	var total replayTotals
	db, err := sql.Open("sqlite", "file:"+file+"?mode=ro&immutable=1")
	if err != nil {
		return total, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var version int
	if err = db.QueryRowContext(ctx, "select version from schema").Scan(&version); err != nil {
		return total, err
	}
	if version != schemaVersion {
		return total, fmt.Errorf("replay needs schema %d", schemaVersion)
	}
	if err = db.QueryRowContext(ctx, "select coalesce(sum(length(payload)),0) from tolerance_file").Scan(&total.FileBytes); err != nil {
		return total, err
	}
	seen, flipping := map[string]bool{}, map[string]bool{}
	var last int64
	for {
		if err = ctx.Err(); err != nil {
			return total, err
		}
		var id int64
		var hash string
		var packed []byte
		err = db.QueryRowContext(ctx, `select evaluation_id,hash,payload from tolerance_capture where evaluation_id>? order by evaluation_id limit 1`, last).Scan(&id, &hash, &packed)
		if err == sql.ErrNoRows {
			break
		}
		if err != nil {
			return total, err
		}
		last = id
		total.CaptureBytes += int64(len(packed))
		raw, err := decompress(packed)
		if err != nil {
			return total, err
		}
		if digest(raw) != hash {
			return total, fmt.Errorf("capture %d checksum mismatch", id)
		}
		var c toleranceCapture
		if err = json.Unmarshal(raw, &c); err != nil {
			return total, err
		}
		seen[c.Target.Name] = true
		for role, entry := range c.Files {
			var data []byte
			if err = db.QueryRowContext(ctx, "select payload from tolerance_file where hash=?", entry.Hash).Scan(&data); err != nil {
				return total, err
			}
			entry.Data, err = decompress(data)
			if err != nil {
				return total, err
			}
			c.Files[role] = entry
		}
		if !c.Pair.Complete {
			for role, entry := range c.Files {
				if digest(entry.Data) != entry.Hash {
					return total, fmt.Errorf("capture file checksum mismatch: %s", role)
				}
			}
			total.Contexts++
			total.Incomplete++
			if c.Pair.OnLeft {
				total.OnLeft++
			}
			if c.Pair.OffLeft {
				total.OffLeft++
			}
			continue
		}
		dir, err := os.MkdirTemp("", "dockhand-tolerance-replay-")
		if err != nil {
			return total, err
		}
		err = func() error {
			defer os.RemoveAll(dir)
			p, root, err := c.materialize(dir)
			if err != nil {
				return err
			}
			if !filepath.IsLocal(c.Target.Portfile) {
				return fmt.Errorf("capture target is not local")
			}
			contents, err := os.ReadFile(filepath.Join(root, c.Target.Portfile))
			if err != nil {
				return err
			}
			on, off, err := measurePolicies(ctx, p, contents, root)
			if err != nil {
				return err
			}
			// Problem strings can contain a materialized root; rule identity,
			// follower provenance and final verdicts must survive replay exactly.
			if on.Inconclusive != c.Pair.On.Inconclusive || off.Inconclusive != c.Pair.Off.Inconclusive || !reflect.DeepEqual(on.Accesses, c.Pair.On.Accesses) || !reflect.DeepEqual(off.Accesses, c.Pair.Off.Accesses) {
				return fmt.Errorf("capture %d replay differs", id)
			}
			return nil
		}()
		if err != nil {
			return total, err
		}
		total.Contexts++
		if c.Pair.Parity {
			total.Parity++
		}
		if c.Pair.On.Inconclusive != c.Pair.Off.Inconclusive {
			total.Flips++
			flipping[c.Target.Name] = true
			total.Changes = append(total.Changes, toleranceChange{Evaluation: id, Port: c.Target.Name, Platform: c.Platform, Modeled: c.Modeled, On: c.Pair.On.Inconclusive, Off: c.Pair.Off.Inconclusive, OnLeft: c.Pair.OnLeft, OffLeft: c.Pair.OffLeft, Mentions: c.mentions()})
		}
		if c.Pair.OnLeft {
			total.OnLeft++
		}
		if c.Pair.OffLeft {
			total.OffLeft++
		}
	}
	if total.Contexts == 0 {
		return total, fmt.Errorf("record contains no tolerance captures")
	}
	total.Ports, total.FlippingPorts = len(seen), len(flipping)
	return total, nil
}

// mentions supplies source witnesses for a person's grouping, not a proof
// that every line is part of the accepted flow. Base files are excluded.
func (c *toleranceCapture) mentions() []toleranceMention {
	names := map[string]bool{}
	for i, a := range c.Pair.On.Accesses {
		if i >= len(c.Pair.Off.Accesses) || a.Rule == c.Pair.Off.Accesses[i].Rule || (a.Rule != "build-default" && a.Rule != "base-lookup") {
			continue
		}
		for _, f := range a.Followers {
			if f.Benign {
				names[f.Option] = true
				for _, n := range f.Watched {
					names[n] = true
				}
			}
		}
	}
	var files []string
	for name := range c.Files {
		if strings.HasPrefix(name, "source/") {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	var found []toleranceMention
	for _, name := range files {
		file := c.Files[name]
		for i, line := range strings.Split(string(file.Data), "\n") {
			text := strings.TrimSpace(line)
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			for watched := range names {
				if strings.Contains(text, watched) {
					found = append(found, toleranceMention{name, i + 1, text, file.Hash})
					break
				}
			}
			if len(found) >= 12 {
				return found
			}
		}
	}
	return found
}

func evaluationCode(err error) string {
	if err == nil {
		return ""
	}
	return portedit.Problem("evaluation", err).Code
}

type measurementReport struct {
	Evaluations, Captures, Complete, Parity, Unavailable, Missing, Flips, OnLeft, OffLeft int
	CaptureBytes, FileBytes                                                               int64
	AnalysisSeconds, CaptureSeconds, CompressionSeconds                                   float64
}

func (r *Record) measurementReport() (measurementReport, error) {
	var m measurementReport
	if err := r.db.QueryRow(`select count(*),coalesce(sum(error_code='evaluation-unavailable'),0) from evaluation where run_id=?`, r.run).Scan(&m.Evaluations, &m.Unavailable); err != nil {
		return m, err
	}
	if err := r.db.QueryRow(`select count(*),coalesce(sum(t.complete),0),coalesce(sum(t.parity),0),coalesce(sum(t.complete and t.on_verdict<>t.off_verdict),0),coalesce(sum(t.on_left),0),coalesce(sum(t.off_left),0) from tolerance_context t join evaluation e on e.id=t.evaluation_id where e.run_id=?`, r.run).Scan(&m.Captures, &m.Complete, &m.Parity, &m.Flips, &m.OnLeft, &m.OffLeft); err != nil {
		return m, err
	}
	m.Missing = m.Evaluations - m.Captures
	if err := r.db.QueryRow(`select coalesce(sum(length(payload)),0), coalesce(sum(analysis_ns),0)/1e9, coalesce(sum(capture_ns),0)/1e9, coalesce(sum(compression_ns),0)/1e9 from tolerance_capture`).Scan(&m.CaptureBytes, &m.AnalysisSeconds, &m.CaptureSeconds, &m.CompressionSeconds); err != nil {
		return m, err
	}
	if err := r.db.QueryRow(`select coalesce(sum(length(payload)),0) from tolerance_file`).Scan(&m.FileBytes); err != nil {
		return m, err
	}
	return m, nil
}
