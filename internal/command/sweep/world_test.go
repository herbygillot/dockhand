package sweep

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
)

// A world is what a run leaves, by section, each a sorted list of lines,
// normalized so two runs of the same workflow read the same: the GitHub
// stand-in's pull requests, every repository's refs, each worktree's
// head, index and files, and every table of the database
// (docs/designs/fault-sweep.md, "How two worlds are compared").
type world map[string][]string

// snapshot reads the world a scenario's World and GitHub stand-in leave.
func snapshot(t *testing.T, w cmdtest.World, g *forgetest.GitHub) world {
	t.Helper()
	out := world{}
	names := &commitNames{names: map[string]string{}}
	repositories := map[string]string{"upstream": w.Upstream, "fork": filepath.Join(filepath.Dir(w.Upstream), "fork.git"), "clone": w.Clone}
	worktrees, _ := filepath.Glob(filepath.Join(w.Home, "Source", "macports-branches", "*"))
	for _, worktree := range worktrees {
		repositories["worktree "+filepath.Base(worktree)] = worktree
	}
	for _, label := range slices.Sorted(maps.Keys(repositories)) {
		path := repositories[label]
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if !strings.HasPrefix(label, "worktree ") {
			out["refs "+label] = refs(t, path, names)
		}
		if label == "clone" || strings.HasPrefix(label, "worktree ") {
			out["files "+label] = files(t, path, names)
		}
	}
	if g != nil {
		out["github"] = pullRequests(g)
	}
	for table, rows := range database(t, filepath.Join(w.Home, ".dockhand", "dockhand.db")) {
		out["table "+table] = rows
	}
	return normalize(renumber(nameCommits(t, out, names, w.Clone)), filepath.Dir(w.Home))
}

var (
	checkpointRow  = regexp.MustCompile(`(?:^| )number=(\d+) .* state="([a-z]+)"`)
	checkpointName = regexp.MustCompile(`\b(tidy|rebase)-(\d+)\b`)
	checkpointCell = regexp.MustCompile(`(^| )number=(\d+) `)
)

// renumber names each checkpoint by its place among the repository's
// checkpoints that aren't abandoned, wherever its number appears: its
// row, its refs, an event's words, a rebase's base. An abandoned attempt
// rightly takes a number, so the retry's is tidy-2 where the clean run's
// is tidy-1, and giving it 1 again would be the bug; the abandoned row is
// an allowed extra (differences) and takes a name outside the sequence,
// so a ref left under it still shows. Tidy's and rebase's are one
// sequence (architecture, #129).
func renumber(w world) world {
	var kept []int
	abandoned := map[int]bool{}
	for _, line := range w["table checkpoints"] {
		match := checkpointRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		n, _ := strconv.Atoi(match[1])
		if match[2] == "abandoned" {
			abandoned[n] = true
		} else {
			kept = append(kept, n)
		}
	}
	slices.Sort(kept)
	name := func(n int) string {
		if abandoned[n] {
			return fmt.Sprintf("abandoned%d", n)
		}
		if at, found := slices.BinarySearch(kept, n); found {
			return strconv.Itoa(at + 1)
		}
		return strconv.Itoa(n)
	}
	out := world{}
	for section, lines := range w {
		renamed := make([]string, len(lines))
		for i, line := range lines {
			line = checkpointName.ReplaceAllStringFunc(line, func(m string) string {
				match := checkpointName.FindStringSubmatch(m)
				n, _ := strconv.Atoi(match[2])
				return match[1] + "-" + name(n)
			})
			if section == "table checkpoints" {
				line = checkpointCell.ReplaceAllStringFunc(line, func(m string) string {
					match := checkpointCell.FindStringSubmatch(m)
					n, _ := strconv.Atoi(match[2])
					return match[1] + "number=" + name(n) + " "
				})
			}
			renamed[i] = line
		}
		out[section] = renamed
	}
	return out
}

var (
	fullHashes  = regexp.MustCompile(`\b[0-9a-f]{40}\b`)
	shortHashes = regexp.MustCompile(`\b[0-9a-f]{7,12}\b`)
)

// nameCommits names each commit a world mentions by hash, whole or
// abbreviated, as refs name them: the clean run and a recovered one each
// build their world afresh, so their fixtures' commits have other hashes
// and the same names. Anything else hex is left as it is.
func nameCommits(t *testing.T, w world, names *commitNames, repo string) world {
	resolved := map[string]string{}
	resolve := func(hex string) string {
		if name, ok := resolved[hex]; ok {
			return name
		}
		name := hex
		if object := strings.TrimSpace(gitOr(repo, "rev-parse", "--verify", "-q", hex+"^{commit}")); object != "" && (len(hex) == 40 || strings.HasPrefix(object, hex)) {
			name = names.name(t, repo, object)
		}
		resolved[hex] = name
		return name
	}
	out := world{}
	for section, lines := range w {
		named := make([]string, len(lines))
		for i, line := range lines {
			if strings.HasPrefix(section, "files ") && strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "file ") {
				named[i] = line
				continue
			}
			line = fullHashes.ReplaceAllStringFunc(line, resolve)
			named[i] = shortHashes.ReplaceAllStringFunc(line, resolve)
		}
		out[section] = named
	}
	return out
}

func gitRead(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	require.NoError(t, err, "git %v in %s", args, dir)
	return string(out)
}

// commitNames name each commit by its tree, message, author and its
// parents' names in order, so a name covers its history: only the hash
// and the times are left out. Two commits with one tree and subject and
// different parents are different commits. The author's time is left
// out as well as the committer's, since dockhand writes both from the
// clock.
type commitNames struct{ names map[string]string }

var timeTail = regexp.MustCompile(` \d+ [+-]\d{4}$`)

func (c *commitNames) name(t *testing.T, repo, object string) string {
	if name, ok := c.names[object]; ok {
		return name
	}
	raw := gitRead(t, repo, "cat-file", "-p", object)
	header, message, _ := strings.Cut(raw, "\n\n")
	var parts []string
	for _, line := range strings.Split(header, "\n") {
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "tree":
			parts = append(parts, line)
		case "parent":
			parts = append(parts, "parent "+c.name(t, repo, value))
		case "author":
			parts = append(parts, "author "+timeTail.ReplaceAllString(value, ""))
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n") + "\n\n" + message))
	subject, _, _ := strings.Cut(message, "\n")
	name := fmt.Sprintf("commit:%x(%s)", sum[:6], subject)
	c.names[object] = name
	return name
}

// refs are a repository's refs, each with what it names: a commit by its
// name, anything else by its hash.
func refs(t *testing.T, repo string, names *commitNames) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(gitRead(t, repo, "for-each-ref", "--format=%(refname) %(objectname) %(objecttype)")), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// A fetch's scratch ref, which the fetch removes after reading it,
		// stays where that removal fails, and nothing reads it again; a
		// named equivalence (fetchMaster's update-ref, failed).
		if strings.HasPrefix(fields[0], "refs/dockhand/fetch/") {
			continue
		}
		named := fields[1]
		if fields[2] == "commit" {
			named = names.name(t, repo, fields[1])
		}
		lines = append(lines, fields[0]+" "+named)
	}
	slices.Sort(lines)
	return lines
}

// files are a worktree's head, its index, and each tracked and untracked
// file by content, mode and link target: two different edits both read
// " M" by status, and differ here.
func files(t *testing.T, dir string, names *commitNames) []string {
	head := strings.TrimSpace(gitRead(t, dir, "rev-parse", "--verify", "-q", "HEAD"))
	branch := strings.TrimSpace(gitOr(dir, "symbolic-ref", "-q", "HEAD"))
	lines := []string{"head " + branch + " " + names.name(t, dir, head)}
	for _, line := range strings.Split(strings.TrimSpace(gitRead(t, dir, "ls-files", "-s")), "\n") {
		if line != "" {
			lines = append(lines, "index "+line)
		}
	}
	for _, path := range strings.Split(gitRead(t, dir, "ls-files", "-co", "--exclude-standard", "-z"), "\x00") {
		if path == "" {
			continue
		}
		full := filepath.Join(dir, path)
		info, err := os.Lstat(full)
		switch {
		case err != nil:
			lines = append(lines, "file "+path+" missing")
		case info.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(full)
			lines = append(lines, "file "+path+" -> "+target)
		default:
			data, _ := os.ReadFile(full)
			lines = append(lines, fmt.Sprintf("file %s %o %x", path, info.Mode().Perm(), sha256.Sum256(data)))
		}
	}
	slices.Sort(lines[1:])
	return lines
}

func gitOr(dir string, args ...string) string {
	out, _ := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return string(out)
}

// pullRequests are the stand-in's pull requests as GitHub would report
// them, draft state and reviewers asked again included, and its reviews
// and reruns.
func pullRequests(g *forgetest.GitHub) []string {
	var lines []string
	for number, pr := range g.PRs {
		// The body is written out, so normalize reads its times, ids and
		// commits as everywhere else.
		lines = append(lines, fmt.Sprintf("pr #%d %s:%s→%s %s draft=%t title=%q body=%q head=%s rerequested=%v",
			number, pr.HeadRepository, pr.HeadBranch, pr.BaseBranch, pr.State, g.Draft(number), pr.Title, pr.Body, pr.RemoteHead, g.RerequestedOn[number]))
	}
	for _, review := range g.Reviews {
		lines = append(lines, fmt.Sprintf("review #%d changes=%t body=%x", review.Ref.Number, review.RequestChanges, sha256.Sum256([]byte(review.Body))))
	}
	for _, run := range g.Rerun {
		lines = append(lines, fmt.Sprintf("rerun %d", run))
	}
	slices.Sort(lines)
	return lines
}

// database is each table's rows, a row its columns' values in order, its
// times marked for normalize by their column.
func database(t *testing.T, path string) map[string][]string {
	// A path is escaped in the URI: a subtest's directory, named for an
	// effect's address, holds a '#', which began the URI's fragment.
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String())
	require.NoError(t, err)
	defer db.Close()
	tables, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	require.NoError(t, err)
	var names []string
	for tables.Next() {
		var name string
		require.NoError(t, tables.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, tables.Close())
	out := map[string][]string{}
	for _, table := range names {
		query := `SELECT * FROM "` + table + `"`
		if table == "events" {
			query += ` ORDER BY sequence`
		}
		rows, err := db.Query(query)
		require.NoError(t, err)
		columns, err := rows.Columns()
		require.NoError(t, err)
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			require.NoError(t, rows.Scan(pointers...))
			var cells []string
			for i, column := range columns {
				cells = append(cells, column+"="+cell(column, values[i]))
			}
			out[table] = append(out[table], strings.Join(cells, " "))
		}
		require.NoError(t, rows.Close())
	}
	return out
}

// cell is a column's value as text, a time, by its column's name, marked
// for normalize as «t:millis».
func cell(column string, value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case []byte:
		return strconv.Quote(string(v))
	case int64:
		if strings.HasSuffix(column, "_at") || column == "heartbeat" || column == "at" {
			return fmt.Sprintf("«t:%d»", v)
		}
		return strconv.FormatInt(v, 10)
	case string:
		return strconv.Quote(v)
	}
	return fmt.Sprint(value)
}

var (
	// ids are store.NewID's: a prefix, an underscore, and sixteen base32
	// letters and digits.
	ids = regexp.MustCompile(`\b[a-z]{2,8}_[a-z2-7]{16}\b`)
	// marked times are cell's; written times are RFC 3339's.
	markedTimes  = regexp.MustCompile(`«t:(\d+)»`)
	writtenTimes = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)
)

// normalize makes two runs' worlds comparable: the temporary root a
// fixed word; a random record id a name for the record it identifies,
// the same wherever it appears, covering the names of the records it
// points at, and named in the order the world mentions it once its rows
// are sorted by what isn't an id; and each time its day as dockhand reads
// it, the local one, with its order among its row's times, which
// attempts' order and serve's daily limit read (#129's fix list, 5 and
// 6).
// settledFields are a branch's columns that settling may leave otherwise
// than a clean run, as a scenario declares (unreadable).
var settledFields = regexp.MustCompile(`\b(pr_provenance|pr_rerequested|publication)="(?:[^"\\]|\\.)*"`)

func normalize(w world, root string) world {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	// A time is its day, the local one, and its order among the times of
	// its own row: a rank in the whole run shifted with every row a retry
	// rightly adds, and a row's own order, an attempt's start before its
	// end, a publication's tried before its gone, survives them (#129's
	// fix list, 6). Order between rows is read where it's recorded, as an
	// event's or an attempt's number.
	stamp := func(line string) string {
		var values []int64
		read := func(m string) (int64, bool) {
			if sub := markedTimes.FindStringSubmatch(m); sub != nil {
				v, err := strconv.ParseInt(sub[1], 10, 64)
				return v, err == nil
			}
			parsed, err := time.Parse(time.RFC3339Nano, m)
			return parsed.UnixMilli(), err == nil
		}
		for _, m := range append(markedTimes.FindAllString(line, -1), writtenTimes.FindAllString(line, -1)...) {
			if v, ok := read(m); ok && !slices.Contains(values, v) {
				values = append(values, v)
			}
		}
		slices.Sort(values)
		rank := func(m string) string {
			v, ok := read(m)
			if !ok {
				return m
			}
			day := "time(" + time.UnixMilli(v).Local().Format("2006-01-02")
			if len(values) > 1 {
				day += fmt.Sprintf("#%d", slices.Index(values, v)+1)
			}
			return day + ")"
		}
		return writtenTimes.ReplaceAllStringFunc(markedTimes.ReplaceAllStringFunc(line, rank), rank)
	}
	// A record's own id is named by what the record holds, its times
	// masked, and the names of the records it points at in place of their
	// ids, so two records that differ only in their parents differ, and a
	// row a retry adds, which nothing points at, renames nothing (#129's
	// fix list, 5). Names are taken in rounds, each reading the last
	// round's names of what a record points at, as deep as a reference
	// chain runs.
	own := regexp.MustCompile(`(?:^| )id="([a-z]{2,8}_[a-z2-7]{16})"`)
	type record struct{ section, id, line string }
	var records []record
	for _, section := range slices.Sorted(maps.Keys(w)) {
		for _, line := range w[section] {
			if match := own.FindStringSubmatch(line); match != nil {
				masked := markedTimes.ReplaceAllString(writtenTimes.ReplaceAllString(strings.ReplaceAll(line, root, "ROOT"), "T"), "T")
				// How a pull request is known and what became of a reviewer
				// request are what a scenario may declare (unreadable), not
				// which branch a row is, so they don't name it.
				masked = settledFields.ReplaceAllString(masked, `$1="…"`)
				records = append(records, record{section: section, id: match[1], line: masked})
			}
		}
	}
	named := map[string]string{}
	for round := 0; round < 6; round++ {
		next := map[string]string{}
		for _, r := range records {
			text := ids.ReplaceAllStringFunc(r.line, func(id string) string {
				if id == r.id {
					return "SELF"
				}
				if name, ok := named[id]; ok {
					return name
				}
				return "ID"
			})
			sum := sha256.Sum256([]byte(r.section + text))
			prefix, _, _ := strings.Cut(r.id, "_")
			next[r.id] = fmt.Sprintf("%s:%x", prefix, sum[:4])
		}
		named = next
	}
	out := world{}
	for _, section := range slices.Sorted(maps.Keys(w)) {
		lines := slices.Clone(w[section])
		for i, line := range lines {
			lines[i] = stamp(strings.ReplaceAll(line, root, "ROOT"))
		}
		// Sorted by what isn't an id, so ids are named in an order that
		// doesn't depend on their random letters; events keep theirs, by
		// their sequence, which is how they're compared.
		if section != "table events" {
			slices.SortStableFunc(lines, func(a, b string) int {
				return strings.Compare(ids.ReplaceAllString(a, "ID"), ids.ReplaceAllString(b, "ID"))
			})
		}
		for i, line := range lines {
			lines[i] = ids.ReplaceAllStringFunc(line, func(id string) string {
				if name, ok := named[id]; ok {
					return name
				}
				prefix, _, _ := strings.Cut(id, "_")
				name := fmt.Sprintf("%s#%d", prefix, len(named)+1)
				named[id] = name
				return name
			})
		}
		out[section] = lines
	}
	return out
}

// differences are what one world has that another lacks, by section, at
// most a few lines of each, as a failure says them.
func differences(clean, recovered world) []string {
	var found []string
	for _, section := range slices.Sorted(maps.Keys(merge(clean, recovered))) {
		a, b := clean[section], recovered[section]
		if section == "table events" {
			found = append(found, eventDifferences(a, b)...)
			continue
		}
		// A session's row is a command's own: a failed run and its retry
		// rightly leave one the clean run lacks, ended or not, its process
		// gone; a named equivalence for the sessions table alone.
		if section == "table sessions" {
			continue
		}
		// An abandoned attempt's checkpoint is the retry's allowed extra
		// (renumber).
		if section == "table checkpoints" {
			b = slices.DeleteFunc(slices.Clone(b), func(line string) bool {
				return strings.Contains(line, `state="abandoned"`) && !slices.Contains(a, line)
			})
		}
		// Rows compare by count: a duplicate row is one the clean run
		// lacks (#129's fix list, 5).
		count := func(lines []string) map[string]int {
			counts := map[string]int{}
			for _, line := range lines {
				counts[line]++
			}
			return counts
		}
		inA, inB := count(a), count(b)
		before := len(found)
		for _, line := range a {
			if inB[line] < inA[line] {
				found = append(found, section+": clean has, recovered lacks: "+line)
				inB[line] = inA[line]
			}
		}
		for _, line := range b {
			if inA[line] < inB[line] {
				found = append(found, section+": recovered has, clean lacks: "+line)
				inA[line] = inB[line]
			}
		}
		if len(a) == len(b) && len(found) == before && !slices.Equal(a, b) {
			found = append(found, section+": the same lines in another order")
		}
	}
	return found
}

func merge(a, b world) world {
	out := world{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

var (
	sequence        = regexp.MustCompile(`^sequence=\d+ `)
	restoreEvent    = regexp.MustCompile(`^(.*kind="branch\.restore".* )message="restored (\S+)'s history from ((?:tidy|rebase)-[0-9a-z]+)"$`)
	checkpointEvent = regexp.MustCompile(`^(.*kind="branch\.(tidy|rebase)".* )message=".*\(checkpoint ((?:tidy|rebase)-[0-9a-z]+)\)"$`)
)

// stateEvents are the events dockhand reads back as state, which compare
// exactly: a run's newest progress or waiting is what status, wait and
// the check's driver say it's doing (LatestRunEvent). Every kind a store
// reader is asked for by kind outside tests is here, as
// TestTheEventsReadAsStateAreListed holds (#129's fix list, 7);
// serve.submit, which serve's count no longer reads, isn't.
var stateEvents = []string{"progress", "waiting"}

// eventDifferences hold the recovered run's events to the clean run's:
// the clean run's appear in order among them, extras allowed, but for
// the events read back as state, which are the same, as many times
// (docs/designs/fault-sweep.md, "Events that dockhand reads back as
// state compare exactly").
func eventDifferences(clean, recovered []string) []string {
	// A session's own events are its command's, as its row is (the
	// sessions table's equivalence): a failed run and its retry each
	// start and end one.
	strip := func(lines []string) []string {
		var out []string
		for _, line := range lines {
			if !strings.Contains(line, `kind="session.`) {
				out = append(out, sequence.ReplaceAllString(line, ""))
			}
		}
		return out
	}
	a, b := merged(strip(clean)), merged(strip(recovered))
	var found []string
	at := 0
	for _, line := range a {
		next := slices.Index(b[at:], line)
		// A change an earlier process made and didn't record is recorded
		// by the next in its own words: "recorded tidy-1: an earlier
		// Dockhand process applied its change before stopping" stands for
		// the clean run's "tidied ... (checkpoint tidy-1)", a named
		// equivalence (history.settle).
		if next < 0 {
			if match := checkpointEvent.FindStringSubmatch(line); match != nil {
				next = slices.Index(b[at:], match[1]+`message="recorded `+match[3]+`: an earlier Dockhand process applied its change before stopping"`)
			}
		}
		// A restore the next process records says so in its own words,
		// "recorded the restore of B's history from X: an earlier Dockhand
		// process restored it before stopping", where the clean run said
		// "restored B's history from X" (history.settle).
		if next < 0 {
			if match := restoreEvent.FindStringSubmatch(line); match != nil {
				prefix := match[1] + `message="recorded the restore of ` + match[2] + `'s history from ` + match[3] + `: an earlier Dockhand process restored it before stopping`
				next = slices.IndexFunc(b[at:], func(l string) bool { return strings.HasPrefix(l, prefix) })
			}
		}
		if next < 0 {
			found = append(found, "table events: clean has, recovered lacks in order: "+line)
			continue
		}
		at += next + 1
	}
	for _, kind := range stateEvents {
		count := func(lines []string) int {
			n := 0
			for _, line := range lines {
				if strings.Contains(line, `kind="`+kind+`"`) {
					n++
				}
			}
			return n
		}
		if count(a) != count(b) {
			found = append(found, fmt.Sprintf("table events: %s %d times, the clean run %d", kind, count(b), count(a)))
		}
	}
	return found
}

var cleanEvent = regexp.MustCompile(`^(.*kind="branch\.clean".* message=")removed (.*)"$`)

// merged reads clean's events as what they removed: one run's "removed A,
// B, C" and a retried run's two partial lines are the same removal, a
// named equivalence (docs/designs/fault-sweep.md, "Equivalences"). Each
// branch's clean events become one, its items sorted, where the first
// stood.
func merged(lines []string) []string {
	var out []string
	first := map[string]int{}
	items := map[string][]string{}
	for _, line := range lines {
		match := cleanEvent.FindStringSubmatch(line)
		if match == nil {
			out = append(out, line)
			continue
		}
		head := match[1]
		if _, ok := first[head]; !ok {
			first[head] = len(out)
			out = append(out, "")
		}
		for _, item := range strings.Split(match[2], ", ") {
			item = strings.TrimPrefix(strings.TrimSpace(item), "and ")
			if item != "" && !slices.Contains(items[head], item) {
				items[head] = append(items[head], item)
			}
		}
	}
	for head, at := range first {
		slices.Sort(items[head])
		out[at] = head + "removed " + strings.Join(items[head], ", ") + `"`
	}
	return out
}
