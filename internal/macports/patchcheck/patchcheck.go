package patchcheck

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/scratch"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/herbygillot/dockhand/internal/archive"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/subprocess"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Patch is one declared patch file as stored in the port's files directory.
type Patch struct {
	Name string
	Data []byte
}

// Request describes the candidate source and how MacPorts would patch it.
type Request struct {
	// Archives are the downloaded source archives; members outside Worksrcdir are ignored.
	Archives []string
	// Worksrcdir is the evaluated archive-relative source directory.
	Worksrcdir string
	// Rename accepts a differing top-level directory, as extract.rename does.
	Rename bool
	// PatchDir is patch.dir relative to the source directory; empty means the source directory.
	PatchDir string
	// PreArgs are the evaluated patch.pre_args tokens, normally -t -N -p0.
	PreArgs []string
	Patches []Patch
}

// Result is the verdict for one patch. Checked is false when the check could
// not model the patch, which is reported rather than treated as a failure.
type Result struct {
	Name    string
	Checked bool
	Applies bool
	Detail  string
}

// Port checks a port's patches against archives as MacPorts would apply
// them, by the port as evaluated: its worksrcdir, extract.rename,
// patch.dir, and patch.pre_args. A patch.dir that leaves the source
// directory can't be modelled, so each patch is unchecked there.
func Port(ctx context.Context, info macports.PortInfo, archives []string, patches []Patch) ([]Result, error) {
	dir := info.Options["patch.dir"]
	if dir != "" && dir != "@worksrc@" && !strings.HasPrefix(dir, "@worksrc@/") {
		results := make([]Result, 0, len(patches))
		for _, patch := range patches {
			results = append(results, Result{Name: patch.Name, Detail: "patch.dir leaves the source directory"})
		}
		return results, nil
	}
	pre, _ := syntax.ListValues(info.Options["patch.pre_args"])
	rename, err := info.Bool("extract.rename")
	if err != nil {
		return nil, err
	}
	return Check(ctx, Request{
		Archives: archives, Worksrcdir: filepath.ToSlash(info.Options["worksrcdir"]), Rename: rename,
		PatchDir: strings.TrimPrefix(strings.TrimPrefix(dir, "@worksrc@"), "/"), PreArgs: pre, Patches: patches,
	})
}

// Rejected lists the patches that were checked and do not apply.
func Rejected(results []Result) []Result {
	var rejected []Result
	for _, result := range results {
		if result.Checked && !result.Applies {
			rejected = append(rejected, result)
		}
	}
	return rejected
}

// Summary describes the results in one line, naming rejected and unchecked patches.
func Summary(results []Result) string {
	var parts []string
	for _, result := range results {
		switch {
		case !result.Checked:
			parts = append(parts, result.Name+" unchecked ("+result.Detail+")")
		case !result.Applies:
			parts = append(parts, result.Name+" "+result.Detail)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d patches apply", len(results))
	}
	return strings.Join(parts, "; ")
}

const maxPatchedFile = 64 << 20

var (
	stripArg   = regexp.MustCompile(`^-p([0-9]+)$`)
	allowedArg = map[string]bool{"-t": true, "-N": true, "-l": true, "-E": true, "-f": true, "-u": true, "-c": true, "--binary": true}
	problems   = regexp.MustCompile(`(?i)hunks? (?:failed|ignored)|No file to patch|can't find file|malformed patch|Reversed|Only garbage|Skipping patch`)
)

// Check runs every patch in check mode against the files it names.
func Check(ctx context.Context, request Request) ([]Result, error) {
	strip, extra, unsupported := parseArgs(request.PreArgs)
	results := make([]Result, len(request.Patches))
	for i, patch := range request.Patches {
		results[i] = Result{Name: patch.Name}
	}
	if unsupported != "" {
		for i := range results {
			results[i].Detail = "patch.pre_args " + unsupported + " is not modeled"
		}
		return results, nil
	}
	contents := make([][]byte, len(request.Patches))
	wanted := map[string]bool{}
	for i, patch := range request.Patches {
		data, err := decompress(patch)
		if err != nil {
			results[i].Detail = err.Error()
			continue
		}
		contents[i] = data
		for _, target := range targets(data, strip) {
			wanted[path.Join(request.PatchDir, target)] = true
		}
	}
	directory, err := scratch.Dir("patchcheck-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	if err := extract(ctx, request, wanted, directory); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// An unreadable archive is reported, not treated as a failing patch.
		for i := range results {
			results[i] = Result{Name: results[i].Name, Detail: "source archive not readable: " + err.Error()}
		}
		return results, nil
	}
	command, err := exec.LookPath("patch")
	if err != nil {
		return nil, fmt.Errorf("patchcheck: %w", err)
	}
	flag, err := checkFlag(ctx, command)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(directory, filepath.FromSlash(request.PatchDir))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	for i := range request.Patches {
		if contents[i] == nil {
			continue
		}
		args := append([]string{flag, "-t", "-N", "-p" + strconv.Itoa(strip)}, extra...)
		// patch's exit status is its verdict, and its output only the
		// detail: output past the first MiB is dropped, not a failure of
		// the check. Before, a patch that applied with that much to say
		// failed every patch's check (the limits sweep, 2026-10-01).
		output, err := subprocess.Run(ctx, subprocess.Spec{Tool: "patch", Path: command, Args: args, Dir: dir, Stdin: bytes.NewReader(contents[i]), Combined: true, Limit: 1 << 20, Drain: true})
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		results[i].Checked = true
		if err == nil {
			results[i].Applies = true
			results[i].Detail = "applies"
			continue
		}
		var failure *subprocess.Error
		var exit *exec.ExitError
		if !errors.As(err, &failure) || !errors.As(failure.Cause, &exit) {
			return nil, err
		}
		results[i].Detail = describe(output.Output)
	}
	return results, nil
}

func parseArgs(args []string) (strip int, extra []string, unsupported string) {
	for _, arg := range args {
		if m := stripArg.FindStringSubmatch(arg); m != nil {
			strip, _ = strconv.Atoi(m[1])
			continue
		}
		if arg == "-t" || arg == "-N" {
			continue
		}
		if !allowedArg[arg] {
			return 0, nil, strconv.Quote(arg)
		}
		extra = append(extra, arg)
	}
	return strip, extra, ""
}

func decompress(patch Patch) ([]byte, error) {
	var reader io.Reader
	switch strings.ToLower(path.Ext(patch.Name)) {
	case ".gz":
		gz, err := gzip.NewReader(bytes.NewReader(patch.Data))
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		reader = gz
	case ".bz2":
		reader = bzip2.NewReader(bytes.NewReader(patch.Data))
	case ".xz", ".z":
		return nil, fmt.Errorf("%s compression is not modeled", strings.TrimPrefix(path.Ext(patch.Name), "."))
	default:
		return patch.Data, nil
	}
	// A patch that decompresses past the bound is refused, leaving it
	// unchecked, rather than cut: a cut patch was checked as its first 64
	// MiB, and could read as applying (the limits sweep, 2026-10-01).
	data, err := io.ReadAll(io.LimitReader(reader, maxPatchedFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPatchedFile {
		return nil, fmt.Errorf("decompresses to more than the %d MiB dockhand checks of a patch", maxPatchedFile>>20)
	}
	return data, nil
}

// targets lists the paths a patch names after stripping leading components,
// the way patch -pN resolves them; /dev/null and unstrippable names are skipped.
func targets(data []byte, strip int) []string {
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		var candidates []string
		switch {
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "*** "):
			candidates = []string{line[4:]}
		case strings.HasPrefix(line, "diff --git "):
			candidates = strings.Fields(line[len("diff --git "):])
		case strings.HasPrefix(line, "Index: "):
			candidates = []string{line[len("Index: "):]}
		}
		for _, candidate := range candidates {
			name, _, _ := strings.Cut(candidate, "\t")
			name = strings.TrimSpace(name)
			if name == "" || name == "/dev/null" || strings.HasPrefix(name, "'") || strings.HasPrefix(name, "\"") {
				continue
			}
			parts := strings.Split(name, "/")
			if len(parts) <= strip {
				continue
			}
			stripped := path.Clean(strings.Join(parts[strip:], "/"))
			if stripped == "." || path.IsAbs(stripped) || strings.HasPrefix(stripped, "../") {
				continue
			}
			if !slices.Contains(names, stripped) {
				names = append(names, stripped)
			}
		}
	}
	return names
}

// extract writes the wanted members of every archive under directory,
// keyed by their path relative to the source directory.
func extract(ctx context.Context, request Request, wanted map[string]bool, directory string) error {
	worksrc := strings.Trim(request.Worksrcdir, "/")
	_, subdir, _ := strings.Cut(worksrc, "/")
	relative := func(member string) (string, bool) {
		if rest, ok := strings.CutPrefix(member, worksrc+"/"); ok {
			return rest, true
		}
		if !request.Rename {
			return "", false
		}
		_, rest, nested := strings.Cut(member, "/")
		if !nested {
			return "", false
		}
		if subdir == "" {
			return rest, true
		}
		return strings.CutPrefix(rest, subdir+"/")
	}
	for _, filename := range request.Archives {
		err := archive.Walk(ctx, filename, func(member archive.Member) error {
			clean, ok := member.Clean()
			if !ok || !member.Regular {
				return nil
			}
			rel, ok := relative(clean)
			if !ok || !wanted[rel] {
				return nil
			}
			if member.Size > maxPatchedFile {
				return fmt.Errorf("patchcheck: %s exceeds the size limit", rel)
			}
			target := filepath.Join(directory, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			_, err = io.Copy(file, io.LimitReader(member.Body, maxPatchedFile))
			return errors.Join(err, file.Close())
		})
		if err != nil {
			return err
		}
	}
	return nil
}

var (
	flagOnce sync.Once
	flagName string
	flagErr  error
)

// checkFlag selects the dry-run flag: GNU patch spells it --dry-run, the
// BSD-derived Apple patch spells it -C.
func checkFlag(ctx context.Context, command string) (string, error) {
	flagOnce.Do(func() {
		output, err := subprocess.Run(ctx, subprocess.Spec{Tool: "patch", Path: command, Args: []string{"--version"}, Combined: true, Limit: 1 << 16})
		if err != nil {
			flagErr = fmt.Errorf("patchcheck: %w", err)
			return
		}
		flagName = "-C"
		if bytes.Contains(output.Output, []byte("GNU")) {
			flagName = "--dry-run"
		}
	})
	return flagName, flagErr
}

func describe(output []byte) string {
	var lines []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if problems.MatchString(line) && !slices.Contains(lines, line) {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "does not apply"
	}
	detail := strings.Join(lines, "; ")
	if len(detail) > 300 {
		detail = detail[:297] + "..."
	}
	return detail
}
