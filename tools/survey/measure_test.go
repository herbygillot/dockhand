package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
)

// The notices of Then's item 11, measured over a whole tree, for their
// false positives: I7's scan of a Portfile's comments, and the survey
// rules, restated, path-style, and ticket-trailer, and the survey's other
// lint-shaped notices, license-spdx, noarch-platforms, commented-code,
// and unreferenced-file, each match printed, so
// a person reads them (the roadmap's measure; no snapshot is kept). It runs
// only where MACPORTS_TREE names a ports tree, a Git checkout:
//
//	MACPORTS_TREE=~/Source/macports-ports go test ./tools/survey -run TestTheNoticesOverATree -v -timeout 2h
//
// Every Portfile is read as a new port's, every line one its change adds,
// the most a rule could say of it. restated evaluates, with MacPorts'
// port-tclsh, the first DOCKHAND_MEASURE_RESTATED Portfiles of every
// stride through the tree (200 unless given; 0 for all of them), each
// without each line it may say; path-style's providers are read from
// the tree's text, a -devel sibling or pkgconf whose conflicts names the
// port; ticket-trailer reads the tree's last DOCKHAND_MEASURE_COMMITS
// commits (20,000 unless given).
func TestTheNoticesOverATree(t *testing.T) {
	root := os.Getenv("MACPORTS_TREE")
	if root == "" {
		t.Skip("set MACPORTS_TREE to a ports tree to measure the notices over it")
	}
	root, err := filepath.Abs(root)
	require.NoError(t, err)
	files, err := filepath.Glob(filepath.Join(root, "*", "*", "Portfile"))
	require.NoError(t, err)
	var paths []string
	for _, file := range files {
		rel, _ := filepath.Rel(root, file)
		if macports.IsCategory(strings.Split(rel, string(filepath.Separator))[0]) {
			paths = append(paths, filepath.ToSlash(rel))
		}
	}
	slices.Sort(paths)
	texts := map[string][]byte{}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		texts[path] = data
	}
	t.Logf("%d Portfiles in %s", len(paths), root)

	// I7: a maintainer's instruction in the comments.
	instructions := 0
	for _, path := range paths {
		for _, found := range portfile.Instructions(texts[path], "") {
			instructions++
			t.Logf("instruction %s:%d: %s", path, found.Line, found.Text)
		}
	}

	// path-style, its providers from the text: a port named X-devel, or
	// pkgconf for pkgconfig, in a Portfile whose conflicts names X.
	conflicts := regexp.MustCompile(`(?m)^\s*conflicts(?:-append)?\s+(.+)$`)
	provides := map[string][]string{}
	for _, path := range paths {
		text := string(texts[path])
		for _, m := range conflicts.FindAllStringSubmatch(text, -1) {
			for _, port := range strings.Fields(m[1]) {
				for _, candidate := range []string{port + "-devel", map[string]string{"pkgconfig": "pkgconf"}[port]} {
					if candidate != "" && (filepath.Base(filepath.Dir(path)) == candidate || regexp.MustCompile(`\bsubport\s+`+regexp.QuoteMeta(candidate)+`\b`).MatchString(text)) && !slices.Contains(provides[port], candidate) {
						provides[port] = append(provides[port], candidate)
					}
				}
			}
		}
	}
	// And the spec the tree writes most for each port, as the engine's
	// treeSpec reads it.
	specs := regexp.MustCompile(`path:[^\s]+:([^\s}\]]+)`)
	counted := map[string]map[string]int{}
	for _, path := range paths {
		for _, m := range specs.FindAllStringSubmatch(string(texts[path]), -1) {
			if counted[m[1]] == nil {
				counted[m[1]] = map[string]int{}
			}
			counted[m[1]][m[0]]++
		}
	}
	most := func(port string) string {
		best := ""
		for spec, n := range counted[port] {
			if n > counted[port][best] || n == counted[port][best] && spec < best {
				best = spec
			}
		}
		return best
	}
	pathStyle := 0
	for _, path := range paths {
		for _, finding := range commitrules.CheckDependencies([]commitrules.Portfile{{Path: path, After: string(texts[path])}}, func(port string) ([]string, string) { return provides[port], most(port) }) {
			pathStyle++
			t.Logf("path-style %s", finding)
		}
	}

	// restated, over a sample, each evaluated in a scratch copy of the
	// tree's _resources and the port's directory.
	sample := 200
	if given := os.Getenv("DOCKHAND_MEASURE_RESTATED"); given != "" {
		sample, err = strconv.Atoi(given)
		require.NoError(t, err)
	}
	chosen := paths
	if sample > 0 && sample < len(paths) {
		chosen = nil
		for i := 0; i < len(paths) && len(chosen) < sample; i += len(paths) / sample {
			chosen = append(chosen, paths[i])
		}
	}
	tclsh := os.Getenv("DOCKHAND_TEST_MACPORTS_TCLSH")
	if tclsh == "" {
		tclsh = "/opt/local/bin/port-tclsh"
	}
	scratch := t.TempDir()
	require.NoError(t, exec.Command("cp", "-R", filepath.Join(root, "_resources"), scratch).Run())
	tree, err := macports.NewTree(model.Source{Tree: "measure"}, scratch, model.Platform{})
	require.NoError(t, err)
	evaluator := &eval.Evaluator{Executable: tclsh, Env: os.Environ()}
	session, err := evaluator.Open(t.Context(), tree)
	require.NoError(t, err)
	defer session.Close()
	restated, evaluated := 0, 0
	for _, path := range chosen {
		directory := filepath.Join(scratch, filepath.Dir(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(directory), 0o755))
		require.NoError(t, exec.Command("cp", "-R", filepath.Join(root, filepath.Dir(path)), filepath.Dir(directory)).Run())
		name := filepath.Base(filepath.Dir(path))
		evaluate := func(_ string, text []byte) (macports.PortInfo, bool) {
			if err := os.WriteFile(filepath.Join(scratch, path), text, 0o644); err != nil {
				return macports.PortInfo{}, false
			}
			selected, err := tree.Select(model.Target{Name: name, Portfile: path})
			if err != nil {
				return macports.PortInfo{}, false
			}
			snapshot, err := session.Evaluate(t.Context(), selected)
			if err != nil {
				return macports.PortInfo{}, false
			}
			info, ok := snapshot.Ports[name]
			return info, ok
		}
		evaluated++
		for _, finding := range commitrules.CheckRestated([]commitrules.Portfile{{Path: path, After: string(texts[path])}}, evaluate) {
			restated++
			t.Logf("restated %s", finding)
		}
		require.NoError(t, os.RemoveAll(directory))
	}

	// The survey's other lint-shaped notices, each Portfile with every
	// file under its files/ as ones the change adds.
	notices := map[string]int{}
	for _, path := range paths {
		var added []string
		directory := filepath.Join(root, filepath.Dir(path), "files")
		_ = filepath.WalkDir(directory, func(file string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				rel, _ := filepath.Rel(directory, file)
				added = append(added, filepath.ToSlash(rel))
			}
			return nil
		})
		for _, finding := range commitrules.CheckNotices([]commitrules.Portfile{{Path: path, After: string(texts[path]), Files: added}}) {
			notices[finding.Code]++
			t.Logf("%s %s", finding.Code, finding)
		}
	}

	// ticket-trailer, over the tree's last commits.
	commits := "20000"
	if given := os.Getenv("DOCKHAND_MEASURE_COMMITS"); given != "" {
		commits = given
	}
	log, err := exec.Command("git", "-C", root, "log", "-n", commits, "--format=%H%x00%B%x1e").Output()
	require.NoError(t, err)
	ticketed, read := 0, 0
	for record := range bytes.SplitSeq(log, []byte("\x1e")) {
		id, message, ok := strings.Cut(strings.TrimLeft(string(record), "\n"), "\x00")
		if !ok {
			continue
		}
		read++
		for _, finding := range commitrules.CheckCommits([]commitrules.Commit{{ID: id, Message: message}}) {
			if finding.Code == "ticket-trailer" {
				ticketed++
				t.Logf("ticket-trailer %s: %s", id[:10], strings.SplitN(message, "\n", 2)[0])
			}
		}
	}
	fmt.Printf("instructions %d of %d Portfiles; path-style %d; restated %d of %d evaluated; ticket-trailer %d of %d commits\n",
		instructions, len(paths), pathStyle, restated, evaluated, ticketed, read)
	fmt.Printf("license-spdx %d; noarch-platforms %d; commented-code %d; unreferenced-file %d\n",
		notices["license-spdx"], notices["noarch-platforms"], notices["commented-code"], notices["unreferenced-file"])
}
