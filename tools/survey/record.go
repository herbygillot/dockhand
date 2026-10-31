package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

//go:embed schema.sql
var schema string

// schemaVersion is the record's shape, schema.sql's.
const schemaVersion = 1

// Modes are what a run evaluates ports against: current, the fresh
// installation and the facts table, as dockhand does; installed, the
// fresh one with the ports a port asked the registry about installed;
// and host, the Mac's own, in a guest.
var modes = map[string]eval.Installation{"current": eval.FreshInstallation, "installed": eval.InstalledPorts, "host": eval.HostInstallation}

// Recorder is where the survey puts each port as it finishes, and what
// names the ports a rerun leaves out: the journal, or the record.
type Recorder interface {
	Begin(model.Source) error
	Has(selector string) bool
	Record(Port) error
}

// Record is a survey's database, one file per snapshot of the tree, never
// dockhand's own. One writer goroutine owns its connection, and each port
// is one transaction, its journal line written beside it by the same
// writer; the evaluators never touch it. A rerun of the same mode, with
// recording on or off as before, continues its run, and a file of another
// commit is refused.
type Record struct {
	db        *sql.DB
	path      string
	mode      string
	recording bool
	info      RunInfo
	run       int64
	commit    string
	repo      string
	git       string
	journal   *Journal
	held      map[string]bool
	trees     map[string]string

	jobs chan job
	done chan struct{}

	// The writer's own: paths and hook bodies kept by id, and the
	// toolchains written.
	paths      map[string]int64
	normalized map[string]string
	toolchains map[string]bool
	roots      sync.Map
}

// RunInfo is how a run is made, written to its row when it begins.
type RunInfo struct {
	Tree, Tclsh, Portindex, Base, Host, Load string
	Parallel                                 int
}

type job struct {
	write func(*sql.Tx) error
	line  any
	done  chan error
}

// OpenRecord opens or creates the record at file for a run of mode,
// writing the journal too where one is given.
func OpenRecord(ctx context.Context, file, mode string, recording bool, info RunInfo, repo, gitBin string, journal *Journal) (*Record, error) {
	if _, ok := modes[mode]; !ok {
		return nil, fmt.Errorf("survey: unknown mode %q", mode)
	}
	params := url.Values{"_pragma": {"journal_mode(WAL)", "synchronous(NORMAL)", "foreign_keys(1)", "busy_timeout(10000)"}}
	uri := url.URL{Scheme: "file", Path: file, RawQuery: params.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	r := &Record{db: db, path: file, mode: mode, recording: recording, info: info, repo: repo, git: gitBin, journal: journal,
		held: map[string]bool{}, trees: map[string]string{}, jobs: make(chan job), done: make(chan struct{}), paths: map[string]int64{}, normalized: map[string]string{}, toolchains: map[string]bool{}}
	if err := r.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	go r.writer()
	return r, nil
}

func (r *Record) initialize(ctx context.Context) error {
	var tables int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		_, err := r.db.ExecContext(ctx, schema)
		return err
	}
	var version int
	if err := r.db.QueryRowContext(ctx, `SELECT version FROM schema`).Scan(&version); err != nil {
		return err
	}
	if version != schemaVersion {
		return fmt.Errorf("survey: %s is a record of schema %d, not %d", r.path, version, schemaVersion)
	}
	return nil
}

func (r *Record) writer() {
	defer close(r.done)
	for j := range r.jobs {
		j.done <- r.apply(j)
	}
}

func (r *Record) apply(j job) (err error) {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	paths := len(r.paths)
	defer func() {
		if err != nil {
			_ = tx.Rollback()
			if len(r.paths) != paths {
				// Ids the rolled-back transaction gave are gone with it.
				r.paths = map[string]int64{}
			}
		}
	}()
	if err := j.write(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if j.line != nil && r.journal != nil {
		if line, ok := j.line.(Port); ok {
			return r.journal.Record(line)
		}
	}
	return nil
}

// do runs write in the writer, in its own transaction, and waits for it.
func (r *Record) do(write func(*sql.Tx) error, line any) error {
	done := make(chan error, 1)
	r.jobs <- job{write: write, line: line, done: done}
	return <-done
}

// Begin ties the record to the commit surveyed and begins or continues
// the run, and names the ports it already holds.
func (r *Record) Begin(source model.Source) error {
	r.commit = string(source.Commit)
	if r.journal != nil {
		if err := r.journal.Begin(source); err != nil {
			return err
		}
	}
	return r.do(func(tx *sql.Tx) error {
		var other string
		err := tx.QueryRow(`SELECT "commit" FROM run WHERE "commit" <> ? LIMIT 1`, r.commit).Scan(&other)
		if err == nil {
			return fmt.Errorf("survey: %s records commit %s, not %s; use another file", r.path, other, r.commit)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		err = tx.QueryRow(`SELECT id FROM run WHERE mode = ? AND recording = ? AND finished IS NULL ORDER BY id DESC LIMIT 1`, r.mode, r.recording).Scan(&r.run)
		if errors.Is(err, sql.ErrNoRows) {
			result, err := tx.Exec(`INSERT INTO run (mode, tree, "commit", tclsh, portindex, base_version, host_platform, parallel, started, load_at_start, recording)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.mode, r.info.Tree, r.commit, r.info.Tclsh, r.info.Portindex, r.info.Base, r.info.Host, r.info.Parallel,
				time.Now().UTC().Format(time.RFC3339), r.info.Load, r.recording)
			if err != nil {
				return err
			}
			if r.run, err = result.LastInsertId(); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT p.name FROM port p WHERE p.id IN (
				SELECT e.port_id FROM evaluation e JOIN run r ON r.id = e.run_id WHERE r.mode = ? AND r.recording = ?
				UNION SELECT f.port_id FROM finding f JOIN run r ON r.id = f.run_id WHERE r.mode = ? AND r.recording = ?)`,
			r.mode, r.recording, r.mode, r.recording)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			r.held[name] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return r.treeFiles(tx)
	}, nil)
}

// treeFiles records the tree's _resources at the commit, by blob, and
// every directory, a port's among them, by tree, so two snapshots' records
// tell a port whose directory didn't change from one whose did.
func (r *Record) treeFiles(tx *sql.Tx) error {
	insert, err := tx.Prepare(`INSERT OR IGNORE INTO tree_file ("commit", path, blob) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, listing := range [][]string{{"-r", r.commit, "--", "_resources"}, {"-r", "-d", r.commit}} {
		command := exec.Command(firstOf(r.git, "git"), append([]string{"-C", r.repo, "ls-tree"}, listing...)...)
		out, err := command.Output()
		if err != nil {
			return fmt.Errorf("survey: listing the tree at %s: %w", r.commit, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			meta, file, ok := strings.Cut(line, "\t")
			fields := strings.Fields(meta)
			if !ok || len(fields) != 3 {
				continue
			}
			if fields[1] == "blob" {
				r.trees[file] = fields[2]
			}
			if _, err := insert.Exec(r.commit, file, fields[2]); err != nil {
				return err
			}
		}
	}
	return nil
}

// Has reports whether the run's mode already holds the port.
func (r *Record) Has(selector string) bool { return r.held[selector] }

// Record writes one port: its findings, where it was assessed, and each
// context it was observed in, with everything the evaluator recorded.
func (r *Record) Record(port Port) error {
	var line any
	if r.mode == "current" {
		line = port
	}
	return r.do(func(tx *sql.Tx) error {
		var id int64
		category := ""
		if port.portfile != "" {
			category = strings.SplitN(port.portfile, "/", 2)[0]
		}
		if err := tx.QueryRow(`INSERT INTO port (name, portfile, category) VALUES (?, nullif(?, ''), nullif(?, ''))
			ON CONFLICT (name) DO UPDATE SET portfile = coalesce(excluded.portfile, portfile), category = coalesce(excluded.category, category)
			RETURNING id`, port.Selector, port.portfile, category).Scan(&id); err != nil {
			return err
		}
		if r.mode == "current" {
			for _, f := range port.Findings {
				if _, err := tx.Exec(`INSERT INTO finding (port_id, run_id, "check", status, code, detail) VALUES (?, ?, ?, ?, ?, ?)`,
					id, r.run, f.Check, f.Status, f.Code, f.Detail); err != nil {
					return err
				}
			}
		}
		for _, o := range port.observed {
			if err := r.evaluation(tx, id, o); err != nil {
				return err
			}
		}
		return nil
	}, line)
}

// observed is one context a port was observed in.
type observed struct {
	Platform    model.Platform
	Modeled     bool
	Root        string
	Info        macports.PortInfo
	Observation macports.PortObservation
	Err         error
}

func (r *Record) evaluation(tx *sql.Tx, port int64, o observed) error {
	outcome, detail := "observed", ""
	switch {
	case errors.Is(o.Err, macports.ErrRefused):
		outcome, detail = "refused", o.Err.Error()
	case o.Err != nil:
		outcome, detail = "failed", o.Err.Error()
	}
	var id int64
	err := tx.QueryRow(`INSERT INTO evaluation (run_id, port_id, platform_os, platform_version, platform_arch, modeled, outcome, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, nullif(?, '')) ON CONFLICT DO NOTHING RETURNING id`,
		r.run, port, o.Platform.OS, o.Platform.Version, o.Platform.Architecture, o.Modeled, outcome, detail).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if o.Modeled {
		if err := r.toolchain(tx, o.Platform); err != nil {
			return err
		}
	}
	if o.Err != nil {
		return nil
	}
	local := r.localizer(o.Root)
	insert := func(query string, rows func(func(...any) error) error) error {
		statement, err := tx.Prepare(query)
		if err != nil {
			return err
		}
		defer statement.Close()
		return rows(func(args ...any) error {
			_, err := statement.Exec(append([]any{id}, args...)...)
			return err
		})
	}
	if err := insert(`INSERT INTO field (evaluation_id, name, value) VALUES (?, ?, ?)`, func(add func(...any) error) error {
		names := make([]string, 0, len(o.Info.Options))
		for name := range o.Info.Options {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := o.Info.Options[name]
			if name == "dockhand.code" {
				code, errs := syntax.DictValues(value)
				if len(errs) == 0 {
					for _, key := range keysOf(code) {
						if err := add("code:"+key, local(code[key])); err != nil {
							return err
						}
					}
					continue
				}
			}
			if err := add(name, local(value)); err != nil {
				return err
			}
		}
		for _, name := range keysOf(o.Info.OptionErrors) {
			if err := add("error:"+name, local(o.Info.OptionErrors[name])); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := insert(`INSERT INTO portgroup (evaluation_id, name, version, path, blob) VALUES (?, ?, ?, ?, nullif(?, ''))`, func(add func(...any) error) error {
		groups, errs := syntax.ListValues(o.Info.Options["dockhand.portgroups"])
		if len(errs) != 0 {
			return nil
		}
		for _, group := range groups {
			fields, errs := syntax.ListValues(group)
			if len(errs) != 0 || len(fields) != 3 {
				continue
			}
			if err := add(fields[0], fields[1], fields[2], r.trees[strings.TrimPrefix(fields[2], "<source>/")]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := insert(`INSERT INTO distfile (evaluation_id, ord, name, url) VALUES (?, ?, ?, ?)`, func(add func(...any) error) error {
		for i, d := range o.Observation.Distfiles {
			urls := d.URLs
			if len(urls) == 0 {
				urls = []string{""}
			}
			for _, address := range urls {
				if err := add(i, d.Name, local(address)); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := insert(`INSERT INTO read_count (evaluation_id, command, subcommand, kind, source, path_id, count) VALUES (?, ?, ?, ?, ?, ?, ?)`, func(add func(...any) error) error {
		// The evaluator keeps paths as asked; each is normalized here,
		// once, and the reads of one path asked two ways are one.
		type key struct {
			read macports.PathRead
			path string
		}
		counts := map[key]int{}
		var order []key
		for _, read := range o.Observation.Reads {
			k := key{macports.PathRead{Command: read.Command, Subcommand: read.Subcommand, Kind: read.Kind, Source: read.Source}, local(r.normalize(read.Path))}
			if counts[k] == 0 {
				order = append(order, k)
			}
			counts[k] += read.Count
		}
		for _, k := range order {
			path, err := r.pathID(tx, k.path)
			if err != nil {
				return err
			}
			if err := add(k.read.Command, k.read.Subcommand, k.read.Kind, k.read.Source, path, counts[k]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := insert(`INSERT INTO exec (evaluation_id, command, source, count) VALUES (?, ?, ?, ?)`, func(add func(...any) error) error {
		for _, run := range o.Observation.Execs {
			if err := add(local(run.Line), run.Source, run.Count); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := insert(`INSERT INTO ledger (evaluation_id, command, subcommand, source, subject, count) VALUES (?, ?, ?, ?, ?, ?)`, func(add func(...any) error) error {
		for _, entry := range o.Observation.Ledger {
			if err := add(entry.Command, entry.Subcommand, entry.Source, local(entry.Subject), entry.Count); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := insert(`INSERT INTO host_access (evaluation_id, problem, frames, count) VALUES (?, ?, ?, ?)`, func(add func(...any) error) error {
		type access struct{ problem, frames string }
		counts := map[access]int{}
		var order []access
		for _, d := range o.Observation.Declarations {
			if d.Command != "dockhand.host-access" || len(d.Values) == 0 {
				continue
			}
			var frames []macports.SourceFrame
			for _, frame := range d.Frames {
				if frame.File == "" {
					continue
				}
				command := frame.Command
				if len(command) > 200 {
					command = command[:200]
				}
				frames = append(frames, macports.SourceFrame{File: local(frame.File), Line: frame.Line, Command: local(command)})
			}
			data, err := json.Marshal(frames)
			if err != nil {
				return err
			}
			key := access{d.Values[0], string(data)}
			if counts[key] == 0 {
				order = append(order, key)
			}
			counts[key]++
		}
		for _, key := range order {
			if err := add(key.problem, key.frames, counts[key]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return insert(`INSERT INTO hook_body (evaluation_id, phase, origin, body_id) VALUES (?, ?, ?, ?)`, func(add func(...any) error) error {
		for _, hook := range o.Observation.Hooks {
			body, err := r.bodyID(tx, local(hook.Body))
			if err != nil {
				return err
			}
			if err := add(hook.Procedure, local(hook.Origin), body); err != nil {
				return err
			}
		}
		return nil
	})
}

// normalize is a path as Tcl's file normalize has it: cleaned, with its
// symbolic links resolved as far as it exists, as /etc's is to
// /private/etc. It is the writer's, and each path is resolved once.
func (r *Record) normalize(name string) string {
	if resolved, ok := r.normalized[name]; ok {
		return resolved
	}
	resolved := filepath.Clean(name)
	for rest, dir := "", resolved; ; {
		if found, err := filepath.EvalSymlinks(dir); err == nil {
			resolved = filepath.Join(found, rest)
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		rest, dir = filepath.Join(filepath.Base(dir), rest), parent
	}
	r.normalized[name] = resolved
	return resolved
}

// localizer writes the evaluation's root, as given and as the evaluator
// normalized it, <source>, so the record names the tree's files by their
// place in it whatever directory the tree was materialized in.
func (r *Record) localizer(root string) func(string) string {
	if root == "" {
		return func(s string) string { return s }
	}
	if cached, ok := r.roots.Load(root); ok {
		return cached.(*strings.Replacer).Replace
	}
	pairs := []string{root + "/", "<source>/"}
	if resolved := r.normalize(root); resolved != root {
		pairs = append([]string{resolved + "/", "<source>/"}, pairs...)
	}
	replacer := strings.NewReplacer(pairs...)
	r.roots.Store(root, replacer)
	return replacer.Replace
}

func (r *Record) pathID(tx *sql.Tx, name string) (int64, error) {
	return r.intern(tx, "path:"+name, `INSERT INTO path (path) VALUES (?) ON CONFLICT (path) DO UPDATE SET path = excluded.path RETURNING id`, name)
}

func (r *Record) bodyID(tx *sql.Tx, body string) (int64, error) {
	return r.intern(tx, "body:"+body, `INSERT INTO body (body) VALUES (?) ON CONFLICT (body) DO UPDATE SET body = excluded.body RETURNING id`, body)
}

// intern is the id of a value kept once, from the writer's cache where
// it has it.
func (r *Record) intern(tx *sql.Tx, key, query, value string) (int64, error) {
	if id, ok := r.paths[key]; ok {
		return id, nil
	}
	var id int64
	if err := tx.QueryRow(query, value).Scan(&id); err != nil {
		return 0, err
	}
	r.paths[key] = id
	return id, nil
}

// toolchain records the facts table's toolchain for a modelled context,
// once a run.
func (r *Record) toolchain(tx *sql.Tx, platform model.Platform) error {
	key := platform.Version + "/" + platform.Architecture
	if r.toolchains[key] {
		return nil
	}
	tools, err := macports.Toolchain(platform, "")
	if err != nil {
		r.toolchains[key] = true
		return nil
	}
	profile := "tools"
	if tools.Xcode != "" && tools.Xcode != "none" {
		profile = "xcode"
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO toolchain (run_id, darwin, arch, profile, xcode, tools, sdks, developer_dir) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.run, tools.Darwin, platform.Architecture, profile, tools.Xcode, tools.Tools, strings.Join(tools.SDKs, " "), tools.DeveloperDir); err != nil {
		return err
	}
	r.toolchains[key] = true
	return nil
}

// Bootstrap records what mportinit read, once a run.
func (r *Record) Bootstrap(b eval.Bootstrap) {
	_ = r.do(func(tx *sql.Tx) error {
		var held int
		if err := tx.QueryRow(`SELECT count(*) FROM bootstrap_read WHERE run_id = ?`, r.run).Scan(&held); err != nil || held > 0 {
			return err
		}
		for _, read := range b.Reads {
			if _, err := tx.Exec(`INSERT INTO bootstrap_read (run_id, command, path, value) VALUES (?, ?, ?, ?)`, r.run, read.Command, read.Path, read.Value); err != nil {
				return err
			}
		}
		for _, setting := range b.Settings {
			if _, err := tx.Exec(`INSERT INTO bootstrap_setting (run_id, name, value, origin) VALUES (?, ?, ?, ?)`, r.run, setting.Name, setting.Value, setting.Origin); err != nil {
				return err
			}
		}
		for _, alias := range b.Aliases {
			if _, err := tx.Exec(`INSERT INTO worker_alias (run_id, name, target) VALUES (?, ?, ?)`, r.run, alias.Name, alias.Target); err != nil {
				return err
			}
		}
		return nil
	}, nil)
}

// Finish records what the run cost and how many ports it holds. A
// continued run adds this invocation's time to what it had.
func (r *Record) Finish(wall, user, system float64) error {
	return r.do(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE run SET finished = ?, wall_seconds = coalesce(wall_seconds, 0) + ?, user_seconds = coalesce(user_seconds, 0) + ?,
				system_seconds = coalesce(system_seconds, 0) + ?,
				cores = (coalesce(user_seconds, 0) + ? + coalesce(system_seconds, 0) + ?) / nullif(coalesce(wall_seconds, 0) + ?, 0),
				ports = (SELECT count(DISTINCT port_id) FROM evaluation WHERE run_id = ?) WHERE id = ?`,
			time.Now().UTC().Format(time.RFC3339), wall, user, system, user, system, wall, r.run, r.run)
		return err
	}, nil)
}

func (r *Record) Close() error {
	close(r.jobs)
	<-r.done
	return r.db.Close()
}

// Installed are the ports the current run saw ask the registry about a
// port, and for each, the ports it asked about as they'd be installed:
// the version, revision, and epoch the current run evaluated them at in
// the Mac's own context, which is the port index's, and the variants a
// plain install selects there. A subject the run didn't evaluate is left
// out, and its questions are the fresh installation's to answer.
func (r *Record) Installed(ctx context.Context) ([]string, map[string]map[string]eval.InstalledPort, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT p.name, g.subject FROM registry g
		JOIN evaluation e ON e.id = g.evaluation_id JOIN port p ON p.id = e.port_id JOIN run r ON r.id = e.run_id
		WHERE r.mode = 'current' AND g.subject <> '' ORDER BY p.name`)
	if err != nil {
		return nil, nil, err
	}
	asked := map[string][]string{}
	var askers []string
	for rows.Next() {
		var name, subject string
		if err := rows.Scan(&name, &subject); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if _, seen := asked[name]; !seen {
			askers = append(askers, name)
		}
		asked[name] = append(asked[name], subject)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	lookup, err := r.db.PrepareContext(ctx, `SELECT f.name, f.value FROM field f JOIN evaluation e ON e.id = f.evaluation_id
		JOIN port p ON p.id = e.port_id JOIN run r ON r.id = e.run_id
		WHERE r.mode = 'current' AND e.modeled = 0 AND e.outcome = 'observed' AND p.name = ?
		AND f.name IN ('version', 'revision', 'epoch', 'canonical_active_variants')`)
	if err != nil {
		return nil, nil, err
	}
	defer lookup.Close()
	known := map[string]*eval.InstalledPort{}
	tables := map[string]map[string]eval.InstalledPort{}
	for _, asker := range askers {
		table := map[string]eval.InstalledPort{}
		for _, subject := range asked[asker] {
			held, seen := known[subject]
			if !seen {
				fields, err := queryFields(ctx, lookup, subject)
				if err != nil {
					return nil, nil, err
				}
				if version, ok := fields["version"]; ok {
					revision, _ := strconv.Atoi(fields["revision"])
					epoch, _ := strconv.Atoi(fields["epoch"])
					held = &eval.InstalledPort{Version: version, Revision: revision, Epoch: epoch, Variants: fields["canonical_active_variants"]}
				}
				known[subject] = held
			}
			if held != nil {
				table[subject] = *held
			}
		}
		tables[asker] = table
	}
	return askers, tables, nil
}

func queryFields(ctx context.Context, statement *sql.Stmt, name string) (map[string]string, error) {
	rows, err := statement.QueryContext(ctx, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := map[string]string{}
	for rows.Next() {
		var field, value string
		if err := rows.Scan(&field, &value); err != nil {
			return nil, err
		}
		fields[field] = value
	}
	return fields, rows.Err()
}

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
