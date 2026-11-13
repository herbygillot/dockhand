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
	if _, err = tx.Exec(`insert into tolerance_context(evaluation_id,on_verdict,off_verdict,on_left,off_left,parity,pair) values (?,?,?,?,?,?,?)`, id, c.Pair.On.Inconclusive, c.Pair.Off.Inconclusive, c.Pair.OnLeft, c.Pair.OffLeft, c.Pair.Parity, string(pairJSON)); err != nil {
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
	for _, file := range c.Files {
		packed, err := compress(file.Data)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`insert or ignore into tolerance_file(hash,payload,raw_bytes) values (?,?,?)`, file.Hash, packed, len(file.Data)); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	packed, err := compress(raw)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`insert into tolerance_capture(evaluation_id,hash,payload,raw_bytes) values (?,?,?,?)`, id, digest(raw), packed, len(raw))
	return err
}

type replayTotals struct {
	Contexts, Parity, Flips, OnLeft, OffLeft int
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
	return total, nil
}
