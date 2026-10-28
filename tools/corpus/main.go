package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
)

func main() {
	if err := corpus(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(1)
	}
}

func corpus(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: corpus refresh -ports <checkout> -commit <commit> | corpus replay -dockhand <binary> [-port <name>]")
	}
	switch args[0] {
	case "refresh":
		flags := flag.NewFlagSet("refresh", flag.ExitOnError)
		ports := flags.String("ports", "", "a macports-ports checkout with the rows' commits")
		commit := flags.String("commit", "", "the commit whose _resources and LICENSE the slice takes")
		data := flags.String("data", "testdata/corpus", "the corpus's directory")
		_ = flags.Parse(args[1:])
		if *ports == "" || *commit == "" {
			return errors.New("refresh needs -ports and -commit")
		}
		return refresh(*ports, *commit, *data)
	case "replay":
		flags := flag.NewFlagSet("replay", flag.ExitOnError)
		dockhand := flags.String("dockhand", "", "the dockhand binary to replay with")
		data := flags.String("data", "testdata/corpus", "the corpus's directory")
		only := flags.String("port", "", "replay this row alone")
		_ = flags.Parse(args[1:])
		if *dockhand == "" {
			return errors.New("replay needs -dockhand")
		}
		return replay(*dockhand, *data, *only)
	}
	return fmt.Errorf("unknown subcommand %q", args[0])
}

// row is one exemplar: a port, its directory, the commit of MacPorts'
// that bumped it, the version it bumped to, and what the replay should
// do, "plans" or "refuses: <words its refusal says>", and why it's in
// the corpus.
type row struct {
	Port, Dir, Bump, Version, Expect, Why string
}

// rows reads rows.tsv: a row a line, its fields tab-separated, # a
// comment.
func rows(data string) ([]row, error) {
	file, err := os.Open(filepath.Join(data, "rows.tsv"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []row
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			return nil, fmt.Errorf("rows.tsv: %q has %d fields, not 6", line, len(fields))
		}
		out = append(out, row{fields[0], fields[1], fields[2], fields[3], fields[4], fields[5]})
	}
	return out, scanner.Err()
}

// refresh rewrites the slice from a ports checkout: _resources and
// MacPorts' LICENSE at commit, and each row's directory as it was before
// its bump, beside the Portfile the bump made. The commit goes in
// COMMIT. A refresh is a diff to review: a PortGroup's change upstream
// arrives here, not in the field.
func refresh(ports, commit, data string) error {
	ctx := context.Background()
	list, err := rows(data)
	if err != nil {
		return err
	}
	repo, err := git.Open(ctx, ports, git.Program{Env: os.Environ()})
	if err != nil {
		return err
	}
	// The trees each part comes from: commit's, and each bump's and its
	// parent's.
	full, err := repo.Resolve(ctx, commit)
	if err != nil {
		return err
	}
	commits := []string{full}
	before, after := map[string]string{}, map[string]string{}
	for _, r := range list {
		if after[r.Bump], err = repo.Resolve(ctx, r.Bump); err != nil {
			return fmt.Errorf("%s: %w", r.Port, err)
		}
		if before[r.Bump], err = repo.Resolve(ctx, r.Bump+"^"); err != nil {
			return fmt.Errorf("%s: %w", r.Port, err)
		}
		commits = append(commits, after[r.Bump], before[r.Bump])
	}
	trees, err := repo.CommitTrees(ctx, commits)
	if err != nil {
		return err
	}
	for _, dir := range []string{"tree", "bumped"} {
		if err := os.RemoveAll(filepath.Join(data, dir)); err != nil {
			return err
		}
	}
	tree := filepath.Join(data, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		return err
	}
	if _, err := repo.MaterializeInto(ctx, trees[full], tree, []string{"_resources"}, nil); err != nil {
		return err
	}
	license, err := file(ctx, repo, trees[full], "LICENSE")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(data, "LICENSE"), license, 0o644); err != nil {
		return err
	}
	for _, r := range list {
		if _, err := repo.MaterializeInto(ctx, trees[before[r.Bump]], tree, []string{r.Dir}, nil); err != nil {
			return fmt.Errorf("%s: %w", r.Port, err)
		}
		bumped, err := file(ctx, repo, trees[after[r.Bump]], r.Dir+"/Portfile")
		if err != nil {
			return fmt.Errorf("%s: %w", r.Port, err)
		}
		into := filepath.Join(data, "bumped", r.Dir)
		if err := os.MkdirAll(into, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(into, "Portfile"), bumped, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(data, "COMMIT"), []byte(full+"\n"), 0o644)
}

// file is a tree's file at a path.
func file(ctx context.Context, repo *git.Repository, tree, path string) ([]byte, error) {
	files, err := repo.ReadFiles(ctx, tree, []string{path})
	if err != nil {
		return nil, err
	}
	data, ok := files[path]
	if !ok {
		return nil, fmt.Errorf("the tree has no %s", path)
	}
	return data, nil
}

// envelope is the part of dockhand's --json output the replay reads.
type envelope struct {
	ExitCode int     `json:"exit_code"`
	Error    *string `json:"error"`
	Result   struct {
		Diff string `json:"diff"`
	} `json:"result"`
}

// replay runs each row's bump through dockhand's own command, update
// <port> <version> --new --plan, in a git repository made of the slice,
// with a HOME and database of its own, and compares what the plan writes
// with what the bump wrote: the version, checksums, and dependency
// blocks, as words where order means nothing. A row that should refuse
// must refuse, saying what its row says. It needs the network, for the
// forge's tags, the archives, and the dependency helpers.
func replay(dockhand, data, only string) error {
	list, err := rows(data)
	if err != nil {
		return err
	}
	dockhand, err = filepath.Abs(dockhand)
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "dockhand-corpus-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	tree := filepath.Join(scratch, "ports")
	if err := os.CopyFS(tree, os.DirFS(filepath.Join(data, "tree"))); err != nil {
		return err
	}
	if err := commitSlice(tree); err != nil {
		return err
	}
	home := filepath.Join(scratch, "home")
	if err := os.MkdirAll(filepath.Join(home, ".dockhand"), 0o755); err != nil {
		return err
	}
	env := []string{"HOME=" + home, "DOCKHAND_UPSTREAM=" + tree, "PATH=" + os.Getenv("PATH")}
	for _, name := range []string{"TMPDIR", "GH_TOKEN", "GITHUB_TOKEN", "DOCKHAND_TEST_MACPORTS_TCLSH"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	started := time.Now()
	failed, ran := 0, 0
	for _, r := range list {
		if only != "" && r.Port != only {
			continue
		}
		ran++
		began := time.Now()
		problem := replayRow(context.Background(), dockhand, tree, filepath.Join(scratch, "db"), env, data, r)
		took := time.Since(began).Round(time.Second)
		if problem != "" {
			failed++
			fmt.Printf("FAIL %s %s (%s): %s\n", r.Port, r.Version, took, problem)
			continue
		}
		fmt.Printf("ok   %s %s (%s): %s\n", r.Port, r.Version, took, r.Expect)
	}
	fmt.Printf("%d of %d rows as their bumps, in %s\n", ran-failed, ran, time.Since(started).Round(time.Second))
	if failed > 0 {
		return fmt.Errorf("%d rows differ from their bumps", failed)
	}
	return nil
}

// commitSlice makes the slice a repository whose master is one commit of
// it, as a ports tree's is.
func commitSlice(tree string) error {
	ctx := context.Background()
	repo, err := git.Init(ctx, tree, "master", git.Program{Env: os.Environ()})
	if err != nil {
		return err
	}
	if err := repo.AddAll(ctx); err != nil {
		return err
	}
	index, err := repo.IndexTree(ctx)
	if err != nil {
		return err
	}
	corpus := git.Signature{Name: "corpus", Email: "corpus@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(ctx, git.Commit{Tree: index, Message: "the corpus's slice\n", Author: corpus, Committer: corpus})
	if err != nil {
		return err
	}
	return repo.CreateBranch(ctx, "master", commit)
}

// replayRow replays one row, and says how it differs from its bump; empty
// where it doesn't.
func replayRow(ctx context.Context, dockhand, tree, db string, env []string, data string, r row) string {
	cmd := exec.CommandContext(ctx, dockhand, "--db", db, "--tree", tree, "--json", "update", r.Port, r.Version, "--new", "--plan")
	cmd.Dir, cmd.Env = tree, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	_ = cmd.Run()
	var out envelope
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return fmt.Sprintf("no JSON result (%v): %s", err, lastLine(stderr.String()))
	}
	refusal := ""
	if out.Error != nil {
		refusal = *out.Error
	}
	if want, ok := strings.CutPrefix(r.Expect, "refuses: "); ok {
		switch {
		case out.ExitCode == 0:
			return "planned, where it should refuse: " + want
		case !strings.Contains(refusal, want):
			return fmt.Sprintf("refused, but not for %q: %s", want, refusal)
		}
		return ""
	}
	if out.ExitCode != 0 && out.ExitCode != 3 {
		return fmt.Sprintf("exited %d: %s", out.ExitCode, refusal)
	}
	path := filepath.Join(tree, r.Dir, "Portfile")
	before, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	// The plan's diff of the Portfile, applied to a copy of it, which
	// leaves the slice as it was for the next row. A patch the update
	// re-emitted, or dropped, under files/ is no part of what a bump
	// compares: mariadb-10.11's and qemu's refreshed patches made the
	// whole diff fail to apply to the Portfile alone (4ba655e2 on).
	result := filepath.Join(filepath.Dir(db), r.Port+".Portfile")
	apply := exec.CommandContext(ctx, "patch", "--silent", "--force", "-o", result, path)
	apply.Stdin = strings.NewReader(portfileDiff(out.Result.Diff, r.Dir))
	if output, err := apply.CombinedOutput(); err != nil {
		return fmt.Sprintf("its plan's diff doesn't apply: %s", strings.TrimSpace(string(output)))
	}
	planned, err := os.ReadFile(result)
	if err != nil {
		return err.Error()
	}
	if bytes.Equal(before, planned) {
		return "its plan changed nothing"
	}
	bumped, err := os.ReadFile(filepath.Join(data, "bumped", r.Dir, "Portfile"))
	if err != nil {
		return err.Error()
	}
	want, err := read(bumped)
	if err != nil {
		return "the bump's " + err.Error()
	}
	got, err := read(planned)
	if err != nil {
		return "the plan's " + err.Error()
	}
	return strings.Join(differences(want, got), "; ")
}

// portfileDiff is the part of a plan's diff that changes the Portfile in
// dir: its "diff --git" section, up to the next file's.
func portfileDiff(diff, dir string) string {
	var kept []string
	keeping := false
	for _, line := range strings.SplitAfter(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			keeping = strings.HasPrefix(line, "diff --git a/"+dir+"/Portfile ")
		}
		if keeping {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return diff
	}
	return strings.Join(kept, "")
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return lines[len(lines)-1]
}
