package macports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Input is one thing an evaluation read or ran, as its record names it
// (PathRead, CommandRun), with the identity it had: a file's contents, a
// directory's names, a path's absence, a glob's sorted matches, or what a
// host program answered, every one of which only reports (HostPrograms). Path is relative to the
// evaluation's root where the tree holds it, Tree saying so, and absolute
// otherwise. What an evaluation reads is one of its outputs, so a reuse of
// it is a depfile's: the inputs it named, each taken again, all as they
// were. The evaluation memo keys on them (eval.Memo), and build reuse is
// to key on the same rows (the oracle rethink's correction 1).
type Input struct {
	Command  string
	Path     string
	Tree     bool `json:",omitempty"`
	Identity string
}

// Identities takes inputs' identities, keeping each path's while its size
// and time are as they were, so a look takes each shared file once. It is
// safe for concurrent use.
type Identities struct {
	mu      sync.Mutex
	stamps  map[string]stamp
	answers map[string]string
}

type stamp struct {
	size     int64
	modTime  time.Time
	dir      bool
	identity string
}

// Inputs are the inputs of what an evaluation in root read: each read the
// host answered, the tree's and the Mac's own files alike, once. What the
// fresh installation or the facts table answered is the evaluator's model,
// not an input. A glob whose pattern Go can't read as Tcl does leaves
// the reads with no inputs, and why.
func (ids *Identities) Inputs(root string, reads []PathRead, runs []CommandRun) ([]Input, string) {
	seen := map[[2]string]bool{}
	var inputs []Input
	for _, run := range runs {
		if run.Source != "host" || seen[[2]string{"exec", run.Line}] {
			continue
		}
		seen[[2]string{"exec", run.Line}] = true
		if _, err := commandLine(run.Line); err != nil {
			return nil, "it ran " + run.Line + ", which " + err.Error()
		}
		inputs = append(inputs, Input{Command: "exec", Path: run.Line, Identity: ids.identity(root, "exec", run.Line)})
	}
	for _, read := range reads {
		if read.Source != "host" || !filepath.IsAbs(read.Path) {
			continue
		}
		command := "path"
		if read.Command == "glob" {
			if strings.ContainsAny(read.Path, "{}\\") {
				return nil, "it globbed " + read.Path + ", a pattern Go doesn't match as Tcl does"
			}
			command = "glob"
		}
		key := [2]string{command, read.Path}
		if seen[key] {
			continue
		}
		seen[key] = true
		input := Input{Command: command, Path: read.Path}
		input.Identity = ids.identity(root, command, read.Path)
		if rel, err := filepath.Rel(root, read.Path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			input.Path, input.Tree = filepath.ToSlash(rel), true
		}
		inputs = append(inputs, input)
	}
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Path != inputs[j].Path {
			return inputs[i].Path < inputs[j].Path
		}
		return inputs[i].Command < inputs[j].Command
	})
	return inputs, ""
}

// Changed is the first input in root whose identity isn't as it was; ok is
// false where every one is.
func (ids *Identities) Changed(root string, inputs []Input) (Input, bool) {
	for _, input := range inputs {
		path := input.Path
		if input.Tree {
			path = filepath.Join(root, filepath.FromSlash(input.Path))
		}
		if ids.identity(root, input.Command, path) != input.Identity {
			return input, true
		}
	}
	return Input{}, false
}

// identity is what path holds now, a glob's matches named from root
// where it holds them, so evaluations in two directories match; or, for
// exec, what the command line answers, asked once.
func (ids *Identities) identity(root, command, path string) string {
	switch command {
	case "glob":
		matches, err := filepath.Glob(path)
		if err != nil {
			return "unreadable"
		}
		for i, match := range matches {
			if rel, err := filepath.Rel(root, match); err == nil && !strings.HasPrefix(rel, "..") {
				matches[i] = filepath.ToSlash(rel)
			}
		}
		sort.Strings(matches)
		sum := sha256.Sum256([]byte(strings.Join(matches, "\x00")))
		return "glob:" + hex.EncodeToString(sum[:])
	case "exec":
		return ids.answer(path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "absent"
	}
	ids.mu.Lock()
	known, ok := ids.stamps[path]
	ids.mu.Unlock()
	if ok && known.size == info.Size() && known.modTime.Equal(info.ModTime()) && known.dir == info.IsDir() {
		return known.identity
	}
	identity := contents(path, info)
	ids.mu.Lock()
	if ids.stamps == nil {
		ids.stamps = map[string]stamp{}
	}
	ids.stamps[path] = stamp{size: info.Size(), modTime: info.ModTime(), dir: info.IsDir(), identity: identity}
	ids.mu.Unlock()
	return identity
}

// contents is a path's identity: a directory's names, a file's contents,
// or another kind's mode.
func contents(path string, info os.FileInfo) string {
	sum := sha256.New()
	switch {
	case info.IsDir():
		entries, err := os.ReadDir(path)
		if err != nil {
			return "unreadable"
		}
		for _, entry := range entries {
			_, _ = io.WriteString(sum, entry.Name()+"\x00")
		}
		return "dir:" + hex.EncodeToString(sum.Sum(nil))
	case info.Mode().IsRegular():
		file, err := os.Open(path)
		if err != nil {
			return "unreadable"
		}
		defer file.Close()
		if _, err := io.Copy(sum, file); err != nil {
			return "unreadable"
		}
		return "file:" + hex.EncodeToString(sum.Sum(nil))
	}
	return "mode:" + info.Mode().Type().String()
}

// answer is what a host program's command line answers, its output and
// exit status, asked once while ids lives: a look asks clang or perl once,
// however many ports ran it.
func (ids *Identities) answer(line string) string {
	ids.mu.Lock()
	known, ok := ids.answers[line]
	ids.mu.Unlock()
	if ok {
		return known
	}
	answer := "unrunnable"
	if words, err := commandLine(line); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		output, err := exec.CommandContext(ctx, words[0], words[1:]...).CombinedOutput()
		cancel()
		status := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			status = exit.ExitCode()
		} else if err != nil {
			status = -1
		}
		sum := sha256.Sum256(append(output, []byte("\x00"+strconv.Itoa(status))...))
		answer = "exec:" + hex.EncodeToString(sum[:])
	}
	ids.mu.Lock()
	if ids.answers == nil {
		ids.answers = map[string]string{}
	}
	ids.answers[line] = answer
	ids.mu.Unlock()
	return answer
}

// commandLine is a recorded command line's words, where it can be asked
// again as it was: whole, as the record keeps the first 200 characters,
// its program named absolutely, with no pipe, and without its standard
// error's redirection, which the answer takes with its output.
func commandLine(line string) ([]string, error) {
	if len(line) >= 200 {
		return nil, errors.New("the record cut short")
	}
	words, errs := syntax.ListValues(line)
	if len(errs) > 0 || len(words) == 0 {
		return nil, errors.New("isn't a command line")
	}
	var kept []string
	for _, word := range words {
		switch {
		case word == "|" || strings.HasPrefix(word, "|"):
			return nil, errors.New("pipes")
		case word == "2>@1" || word == "2>&1" || word == "2>/dev/null" || word == "2>@stdout":
			continue
		case strings.HasPrefix(word, "<") || strings.HasPrefix(word, ">") || strings.HasPrefix(word, "2>") || strings.HasPrefix(word, "@"):
			return nil, errors.New("redirects " + word)
		}
		kept = append(kept, word)
	}
	if !filepath.IsAbs(kept[0]) {
		return nil, errors.New("names its program relatively")
	}
	return kept, nil
}
