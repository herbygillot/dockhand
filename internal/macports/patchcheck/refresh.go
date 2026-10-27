package patchcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/subprocess"
)

// Refresh is a patch as patchcheck re-emitted it from the tree its old
// form made of the new source (the reflections' B): a hunk patch placed at
// an offset, verbatim, moved to where it landed, and a hunk upstream
// already carries, which patch skipped as previously applied, left out
// (the docs dig's I22). Its header comment is kept, and each file's
// header lines. Its prediction is exact: the new source with the refreshed
// patch applied, every hunk where it says, equals the new source as the old
// patch made it, which Check verifies before it gives one.
type Refresh struct {
	Data []byte
	// Moved are the hunks moved, and Dropped those left out, as
	// "file.c hunk 2".
	Moved, Dropped []string
	// Generated are the files the patch changes that a build generates,
	// by a path rule, configure or Makefile.in, which a person looks at
	// where it changes: a hunk there that moved may be against output
	// upstream regenerates.
	Generated []string
}

// Words say what the refresh did: "hunk 2 of src/x.c moved; hunk 1 of
// Makefile.am left out, the new source carries it".
func (r Refresh) Words() string {
	var parts []string
	if len(r.Moved) > 0 {
		parts = append(parts, strings.Join(r.Moved, ", ")+" moved to where it now applies")
	}
	if len(r.Dropped) > 0 {
		parts = append(parts, strings.Join(r.Dropped, ", ")+" left out, the new source carries it")
	}
	if len(r.Generated) > 0 {
		parts = append(parts, "it changes "+strings.Join(r.Generated, ", ")+", which the build generates")
	}
	return strings.Join(parts, "; ")
}

// generated are the files a build generates from others, by their names,
// which a refreshed patch says it changes.
var generated = []string{"configure", "Makefile.in", "aclocal.m4", "config.h.in", "ltmain.sh", "config.guess", "config.sub"}

var (
	// patch names the file it's patching, quoted where its name came
	// after a comment: "Patching file 'dir/a.txt' using Plan A...".
	patchingFile = regexp.MustCompile(`^(?i)Patching file '?(.+?)'? using Plan`)
	hunkIgnored  = regexp.MustCompile(`(?i)previously applied|Reversed \(or previously applied\)`)
	hunkFailed   = regexp.MustCompile(`(?i)hunk #\d+ failed|hunks? failed|No file to patch|can't find file|malformed`)
)

// section is one file's part of a unified patch: its header lines, the
// name it patches as patch resolves it, and its hunks.
type section struct {
	header []string
	name   string
	hunks  []hunk
}

// hunk is one hunk's text, its @@ line on, and its before and after
// blocks: the context with the removed lines, and with the added.
type hunk struct {
	text          []string
	before, after []string
}

// sections reads a unified patch: the comment before its first file, and
// each file's part. A patch in another form, as a context diff, has none,
// and isn't refreshed.
func sections(data []byte, strip int) (comment []string, files []section, ok bool) {
	lines := strings.SplitAfter(string(data), "\n")
	var current *section
	var open *hunk
	for i, line := range lines {
		starts := strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "Index: ") ||
			strings.HasPrefix(line, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ ")
		body := strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(line, " "), "-"), "+"), "\n")
		switch {
		case line == "":
			// What follows the last line end.
		case strings.HasPrefix(line, "*** "):
			return nil, nil, false
		case starts && (current == nil || open != nil || len(current.hunks) > 0):
			files = append(files, section{header: []string{line}})
			current, open = &files[len(files)-1], nil
		case current == nil:
			comment = append(comment, line)
		case strings.HasPrefix(line, "@@"):
			current.hunks = append(current.hunks, hunk{text: []string{line}})
			open = &current.hunks[len(current.hunks)-1]
		case open != nil && strings.HasPrefix(line, " "), open != nil && line == "\n":
			open.text = append(open.text, line)
			open.before, open.after = append(open.before, body), append(open.after, body)
		case open != nil && strings.HasPrefix(line, "-"):
			open.text = append(open.text, line)
			open.before = append(open.before, body)
		case open != nil && strings.HasPrefix(line, "+"):
			open.text = append(open.text, line)
			open.after = append(open.after, body)
		case open != nil && strings.HasPrefix(line, `\`):
			open.text = append(open.text, line)
		default:
			if open != nil {
				// Anything else after a hunk begins another file's part.
				files = append(files, section{})
				current, open = &files[len(files)-1], nil
			}
			current.header = append(current.header, line)
		}
	}
	for i := range files {
		names := targets([]byte(strings.Join(files[i].header, "")), strip)
		if len(names) == 0 {
			return nil, nil, false
		}
		files[i].name = names[len(names)-1]
	}
	return comment, files, len(files) > 0
}

// carried reports a hunk the source already has: its after block is in
// the file, and its before block isn't, as upstream merging the change
// leaves it. patch's -N can't say it a hunk at a time: Apple's skips a
// whole file once one hunk reads as applied.
func (h hunk) carried(file []byte) bool {
	return !slices.Equal(h.before, h.after) && occurrences(file, h.after) > 0 && occurrences(file, h.before) == 0
}

// occurrences counts where block's lines occur in a file, in order.
func occurrences(file []byte, block []string) int {
	lines := strings.Split(strings.TrimSuffix(string(file), "\n"), "\n")
	count := 0
	for i := 0; i+len(block) <= len(lines); i++ {
		if slices.Equal(lines[i:i+len(block)], block) {
			count++
		}
	}
	return count
}

// refresh re-emits a patch that applies at an offset, or with a hunk the
// source already carries, from the tree it makes of the extracted source
// in dir; none for one that applies as written, takes fuzz, or fails a
// hunk. A moved hunk is moved only where its before block occurs once in
// the file, so where it landed is the one place it could.
func refresh(ctx context.Context, command string, args []string, dir string, data []byte, strip int) (*Refresh, error) {
	comment, files, ok := sections(data, strip)
	if !ok {
		return nil, nil
	}
	work, err := os.MkdirTemp(filepath.Dir(dir), "refresh-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	copies := func(root string) error {
		for _, file := range files {
			source, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file.name)))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			target := filepath.Join(root, filepath.FromSlash(file.name))
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			if err := os.WriteFile(target, source, 0600); err != nil {
				return err
			}
		}
		return nil
	}
	old, applied, check := filepath.Join(work, "a"), filepath.Join(work, "b"), filepath.Join(work, "c")
	for _, root := range []string{old, applied, check} {
		if err := os.MkdirAll(root, 0700); err != nil {
			return nil, err
		}
		if err := copies(root); err != nil {
			return nil, err
		}
	}
	// A hunk the source carries is left out of what's applied, and the
	// rest must apply: verbatim, or verbatim at an offset.
	result := &Refresh{}
	var kept strings.Builder
	kept.WriteString(strings.Join(comment, ""))
	numbers := map[string][]int{}
	for _, f := range files {
		original, _ := os.ReadFile(filepath.Join(old, filepath.FromSlash(f.name)))
		kept.WriteString(strings.Join(f.header, ""))
		for i, h := range f.hunks {
			if h.carried(original) {
				result.Dropped = append(result.Dropped, fmt.Sprintf("hunk %d of %s", i+1, f.name))
				continue
			}
			kept.WriteString(strings.Join(h.text, ""))
			numbers[f.name] = append(numbers[f.name], i)
		}
	}
	output, err := run(ctx, command, args, applied, []byte(kept.String()))
	if err != nil || hunkFailed.Match(output) || hunkIgnored.Match(output) {
		return nil, nil
	}
	hunks := placedByFile(output)
	for _, f := range files {
		original, _ := os.ReadFile(filepath.Join(old, filepath.FromSlash(f.name)))
		for _, h := range hunks[f.name] {
			// patch numbers the hunks it was given, the carried left out.
			if h.Fuzz > 0 || h.Number < 1 || h.Number > len(numbers[f.name]) {
				return nil, nil
			}
			index := numbers[f.name][h.Number-1]
			if occurrences(original, f.hunks[index].before) != 1 {
				return nil, nil
			}
			result.Moved = append(result.Moved, fmt.Sprintf("hunk %d of %s", index+1, f.name))
		}
		if slices.Contains(generated, path.Base(f.name)) {
			result.Generated = append(result.Generated, f.name)
		}
	}
	if len(result.Moved) == 0 && len(result.Dropped) == 0 {
		return nil, nil
	}
	// The new patch: the comment, then each file's header and the hunks
	// diff finds between the source and what the old patch made of it.
	var b strings.Builder
	b.WriteString(strings.Join(comment, ""))
	for _, f := range files {
		diff, err := diffFiles(ctx, filepath.Join(old, filepath.FromSlash(f.name)), filepath.Join(applied, filepath.FromSlash(f.name)))
		if err != nil {
			return nil, err
		}
		if len(diff) == 0 {
			continue
		}
		for _, line := range f.header {
			b.WriteString(line)
		}
		b.Write(diff)
	}
	refreshed := []byte(b.String())
	// The prediction, exactly: the refreshed patch applies as written, and
	// makes what the old one made.
	again, err := run(ctx, command, args, check, refreshed)
	if err != nil || len(placed(again)) > 0 || hunkIgnored.Match(again) {
		return nil, nil
	}
	for _, f := range files {
		made, _ := os.ReadFile(filepath.Join(applied, filepath.FromSlash(f.name)))
		got, _ := os.ReadFile(filepath.Join(check, filepath.FromSlash(f.name)))
		if !bytes.Equal(made, got) {
			return nil, nil
		}
	}
	result.Data = refreshed
	return result, nil
}

// run applies a patch for real in dir, verbosely.
func run(ctx context.Context, command string, args []string, dir string, data []byte) ([]byte, error) {
	output, err := subprocess.Run(ctx, subprocess.Spec{Tool: "patch", Path: command, Args: args, Dir: dir, Stdin: bytes.NewReader(data), Combined: true, Limit: 1 << 20, Drain: true})
	return output.Output, err
}

// diffFiles are the hunks of a unified diff from one file to another,
// their own header lines left out; none where they're the same.
func diffFiles(ctx context.Context, before, after string) ([]byte, error) {
	command, err := exec.LookPath("diff")
	if err != nil {
		return nil, err
	}
	output, err := subprocess.Run(ctx, subprocess.Spec{Tool: "diff", Path: command, Args: []string{"-u", before, after}, Limit: 64 << 20})
	var failure *subprocess.Error
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil, nil
	case !errors.As(err, &failure) || !errors.As(failure.Cause, &exit) || exit.ExitCode() != 1:
		return nil, err
	}
	// diff exits 1 where the files differ; its first two lines name them.
	lines := bytes.SplitAfterN(output.Output, []byte("\n"), 3)
	if len(lines) < 3 {
		return nil, fmt.Errorf("patchcheck: diff gave no hunks")
	}
	return lines[2], nil
}

// placedByFile are the hunks patch's verbose output placed elsewhere than
// written, by the file it was patching.
func placedByFile(output []byte) map[string][]Hunk {
	byFile := map[string][]Hunk{}
	file := ""
	for _, line := range strings.SplitAfter(string(output), "\n") {
		if m := patchingFile.FindStringSubmatch(line); m != nil {
			file = path.Clean(m[1])
			continue
		}
		byFile[file] = append(byFile[file], placed([]byte(line))...)
	}
	return byFile
}
