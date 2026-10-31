package patchcheck

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
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
	"github.com/herbygillot/dockhand/internal/scratch"
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
	// Mkdir is extract.mkdir's: MacPorts makes the source directory and
	// unpacks each archive in it, so a member's path is its path below the
	// source directory, as the archive has it.
	Mkdir bool
	// PatchDir is patch.dir relative to the source directory; empty means the source directory.
	PatchDir string
	// PreArgs are the evaluated patch.pre_args tokens, normally -t -N -p0.
	PreArgs []string
	Patches []Patch
	// Fuzz refreshes a patch that applies only with fuzz too, for a
	// person to look over: an attended edit's (Refresh.Fuzzed).
	Fuzz bool
}

// Result is the verdict for one patch. Checked is false when the check could
// not model the patch, which is reported rather than treated as a failure.
type Result struct {
	Name    string
	Checked bool
	Applies bool
	// Merged is a patch that doesn't apply because the source already
	// holds it: it applies in reverse, whole, as upstream merging it
	// leaves it.
	Merged bool
	Detail string
	// Hunks are the hunks patch placed elsewhere than the patch says: at
	// an offset, verbatim, or with fuzz, its context lines loosened, where
	// a fuzzed hunk can land in the wrong place and still build (v1's
	// D12). A patch that applies only so says it in Detail.
	Hunks []Hunk `json:",omitempty"`
	// Refresh is the patch re-emitted to apply as written, where it
	// applied at an offset or with a hunk the source carries; nil where
	// it needs none, or can't be (refresh).
	Refresh *Refresh `json:",omitempty"`
}

// Hunk is one hunk patch placed other than as written: its number in the
// patch, the line it landed at, how far that is from where the patch
// said, and the fuzz it took, 0 for a verbatim one.
type Hunk struct {
	Number, Line, Offset, Fuzz int
}

// Fuzzed reports a patch that applies only with fuzz in some hunk.
func (r Result) Fuzzed() bool {
	return slices.ContainsFunc(r.Hunks, func(h Hunk) bool { return h.Fuzz > 0 })
}

// placedHunk is patch's line for a hunk placed other than as written, in
// Apple's and GNU patch's words alike: "Hunk #1 succeeded at 4 with fuzz
// 1 (offset 3 lines)."
var placedHunk = regexp.MustCompile(`Hunk #(\d+) succeeded at (\d+)(?: with fuzz (\d+))?(?: \(offset (-?\d+) lines?\))?`)

// placed are the hunks patch's verbose output says it placed elsewhere
// than written.
func placed(output []byte) []Hunk {
	var hunks []Hunk
	for _, m := range placedHunk.FindAllStringSubmatch(string(output), -1) {
		var h Hunk
		h.Number, _ = strconv.Atoi(m[1])
		h.Line, _ = strconv.Atoi(m[2])
		h.Fuzz, _ = strconv.Atoi(m[3])
		h.Offset, _ = strconv.Atoi(m[4])
		if h.Fuzz > 0 || h.Offset != 0 {
			hunks = append(hunks, h)
		}
	}
	return hunks
}

// HunkWords say the hunks a patch placed elsewhere: "hunk 2 at an offset
// of 3 lines, hunk 3 with fuzz 1".
func HunkWords(hunks []Hunk) string {
	var words []string
	for _, h := range hunks {
		switch {
		case h.Fuzz > 0:
			words = append(words, fmt.Sprintf("hunk %d with fuzz %d", h.Number, h.Fuzz))
		default:
			words = append(words, fmt.Sprintf("hunk %d at an offset of %d lines", h.Number, h.Offset))
		}
	}
	return strings.Join(words, ", ")
}

// Port checks a port's patches against archives as MacPorts would apply
// them, by the port as evaluated: its worksrcdir, extract.rename,
// patch.dir, and patch.pre_args, then patch.args, which MacPorts' patch
// command line gives after them, so herdr's patch.args -p1 is the strip
// level (field testing at 5eb101a3). A patch.dir that leaves the source
// directory can't be modelled, so each patch is unchecked there.
//
// fuzz refreshes a patch that applies only with fuzz too (Request.Fuzz).
func Port(ctx context.Context, info macports.PortInfo, archives []string, patches []Patch, fuzz bool) ([]Result, error) {
	dir := info.Options["patch.dir"]
	if dir != "" && dir != "@worksrc@" && !strings.HasPrefix(dir, "@worksrc@/") {
		results := make([]Result, 0, len(patches))
		for _, patch := range patches {
			results = append(results, Result{Name: patch.Name, Detail: "patch.dir leaves the source directory"})
		}
		return results, nil
	}
	pre, _ := syntax.ListValues(info.Options["patch.pre_args"])
	args, _ := syntax.ListValues(info.Options["patch.args"])
	pre = append(pre, args...)
	rename, err := info.Bool("extract.rename")
	if err != nil {
		return nil, err
	}
	mkdir, err := info.Bool("extract.mkdir")
	if err != nil {
		return nil, err
	}
	return Check(ctx, Request{
		Archives: archives, Worksrcdir: filepath.ToSlash(info.Options["worksrcdir"]), Rename: rename, Mkdir: mkdir,
		PatchDir: strings.TrimPrefix(strings.TrimPrefix(dir, "@worksrc@"), "/"), PreArgs: pre, Patches: patches, Fuzz: fuzz,
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
	// Each patch is checked on the source the ones before it left, as
	// MacPorts applies them in patchfiles order: each that applies is
	// applied for real before the next is checked, where every one was
	// dry-run on the pristine source and one building on another failed,
	// advancemame's cxxflags.diff on Makefile.in.diff's output (field
	// testing's outdated-ports session). One after a patch that didn't
	// apply is checked on the source as it stands, and says so.
	failed := ""
	defer func() {
		if failed == "" {
			return
		}
		after := false
		for i := range results {
			if after && results[i].Checked {
				results[i].Detail += "; checked after " + failed + ", which didn't apply"
			}
			after = after || results[i].Name == failed
		}
	}()
	for i := range request.Patches {
		if contents[i] == nil {
			continue
		}
		// --verbose says each hunk patch placed elsewhere than written,
		// which a check of applies alone passed over (v1's D12).
		args := append([]string{flag, "--verbose", "-t", "-N", "-p" + strconv.Itoa(strip)}, extra...)
		// patch's exit status is its verdict, and its output only the
		// detail: output past the first MiB is dropped, not a failure of
		// the check. Before, a patch that applied with that much to say
		// failed every patch's check (the limits sweep, 2026-10-01).
		output, err := subprocess.Run(ctx, subprocess.Spec{Tool: "patch", Path: command, Args: args, Dir: dir, Stdin: bytes.NewReader(contents[i]), Combined: true, Limit: 1 << 20, Drain: true})
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		results[i].Checked = true
		apply := append([]string{"--verbose", "-t", "-N", "-p" + strconv.Itoa(strip)}, extra...)
		if err == nil {
			results[i].Applies = true
			results[i].Detail = "applies"
			results[i].Hunks = placed(output.Output)
			switch {
			case results[i].Fuzzed():
				results[i].Detail = "applies only with fuzz: " + HunkWords(results[i].Hunks)
			case len(results[i].Hunks) > 0:
				results[i].Detail = "applies, " + HunkWords(results[i].Hunks)
			}
			if len(results[i].Hunks) > 0 && (!results[i].Fuzzed() || request.Fuzz) {
				if results[i].Refresh, err = refresh(ctx, command, apply, dir, contents[i], strip, request.Fuzz); err != nil {
					return nil, err
				}
			}
			// Applied for real, for the next to be checked on.
			if _, err := subprocess.Run(ctx, subprocess.Spec{Tool: "patch", Path: command, Args: apply, Dir: dir, Stdin: bytes.NewReader(contents[i]), Combined: true, Limit: 1 << 20, Drain: true}); err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, fmt.Errorf("patchcheck: applying %s, which its check applied: %w", results[i].Name, err)
			}
			continue
		}
		var failure *subprocess.Error
		var exit *exec.ExitError
		if !errors.As(err, &failure) || !errors.As(failure.Cause, &exit) {
			return nil, err
		}
		results[i].Detail = describe(output.Output)
		// Apple's patch, which MacPorts runs, doesn't read git's quoted
		// form of a name, as git writes one with a space or a byte past
		// ASCII (the docs dig's I30, as done).
		if quotes(contents[i]) {
			results[i].Detail += "; it names a file in git's quoted form, which Apple's patch doesn't read"
		}
		// A file the patch targets that the source doesn't have is named,
		// where patch's "No file to patch" said none, as hashcat 8.0's
		// lacks bridge_python_generic_hash_mp.c (field testing's
		// outdated-ports session).
		if strings.Contains(results[i].Detail, "No file to patch") || strings.Contains(results[i].Detail, "can't find file") {
			if missing := absent(dir, targets(contents[i], strip)); len(missing) > 0 {
				results[i].Detail = "the source has no " + strings.Join(missing, ", ") + ", which it patches"
			}
		}
		// One the source already holds applies in reverse, every hunk:
		// upstream merged it, and the port can drop it.
		reverse := append([]string{flag, "-t", "-R", "-p" + strconv.Itoa(strip)}, extra...)
		if _, err := subprocess.Run(ctx, subprocess.Spec{Tool: "patch", Path: command, Args: reverse, Dir: dir, Stdin: bytes.NewReader(contents[i]), Combined: true, Limit: 1 << 20, Drain: true}); err == nil {
			results[i].Merged = true
			results[i].Detail = "is already in the source: it applies in reverse"
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		} else if results[i].Refresh, err = refresh(ctx, command, apply, dir, contents[i], strip, false); err != nil {
			// One with some hunks the source carries applies without them.
			return nil, err
		}
		// One the source already holds leaves it as applying it would.
		if !results[i].Merged && failed == "" {
			failed = results[i].Name
		}
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

// Compressed reports a patch file stored compressed, which a refresh,
// writing plain text, doesn't write over.
func Compressed(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".gz", ".bz2", ".xz", ".z":
		return true
	}
	return false
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
// the way patch -pN resolves them; /dev/null and unstrippable names are
// skipped. A name git quoted, as it quotes one with a space, a tab, or a
// byte past ASCII, "a/my file.c", is unquoted: it was skipped, so its
// file wasn't extracted and the patch read as not applying (the docs dig's
// I30).
func targets(data []byte, strip int) []string {
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		var candidates []string
		switch {
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "*** "):
			candidates = headerName(line[4:])
		case strings.HasPrefix(line, "diff --git "):
			candidates = gitNames(line[len("diff --git "):])
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

// LockfileOnly reports a patch every file of which is a Cargo.lock: a
// lockfile refresh, which a new release's own lock may make unneeded, not
// a change to what the project depends on (field testing's batch 15,
// meli's patch-Cargo.lock.diff).
func LockfileOnly(patch []byte) bool {
	names := targets(patch, 0)
	// The original's side often names the file's .orig copy.
	return len(names) > 0 && !slices.ContainsFunc(names, func(name string) bool { return path.Base(strings.TrimSuffix(name, ".orig")) != "Cargo.lock" })
}

// quotes reports a patch whose file headers name a file in git's quoted
// form.
func quotes(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "--- \"") || strings.HasPrefix(line, "+++ \"") || strings.HasPrefix(line, "diff --git \"") {
			return true
		}
	}
	return false
}

// headerName is the name a ---, +++, or *** line gives: git's quoted form
// unquoted, else the text before its tab and date.
func headerName(rest string) []string {
	if strings.HasPrefix(rest, "\"") {
		if name, _, ok := quotedName(rest); ok {
			return []string{name}
		}
		return nil
	}
	return []string{rest}
}

// gitNames are the two names of a diff --git line, each bare or in git's
// quoted form; a bare pair splits at its space, which a bare name has
// none of, as git quotes one that does.
func gitNames(rest string) []string {
	var names []string
	for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
		if strings.HasPrefix(rest, "\"") {
			name, after, ok := quotedName(rest)
			if !ok {
				return names
			}
			names, rest = append(names, name), after
			continue
		}
		name, after, _ := strings.Cut(rest, " ")
		names, rest = append(names, name), after
	}
	return names
}

// quotedName is git's quoted name at the start of text, unquoted, as Go
// reads a quoted string with C's escapes and octal bytes, and what
// follows it.
func quotedName(text string) (name, rest string, ok bool) {
	for i := 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			name, err := strconv.Unquote(text[:i+1])
			return name, text[i+1:], err == nil
		}
	}
	return "", "", false
}

// extract writes the wanted members of every archive under directory,
// keyed by their path relative to the source directory.
func extract(ctx context.Context, request Request, wanted map[string]bool, directory string) error {
	worksrc := strings.Trim(request.Worksrcdir, "/")
	// The project's directory below an archive's top: none for a GOPATH
	// worksrcdir, which a renamed top directory is moved to whole, as
	// golang's moves maddy-0.9.6 to gopath/src/github.com/foxcpp/maddy
	// with extract.rename (field testing at a9cef1f8: its patch read as
	// finding no file to patch, and built).
	subdir := macports.SourceSubdirectory(worksrc)
	// A source directory no archive holds is one a PortGroup's
	// post-extract moves an archive's top directory to, as golang's moves
	// lima-2.2.1 to gopath/src/github.com/lima-vm/lima: it's read as that
	// top directory, as extract.rename reads one (field testing, batch 11:
	// lima's patches read as finding no file to patch, and built).
	moved := false
	if !request.Rename && !request.Mkdir {
		held, err := holds(ctx, request.Archives, worksrc)
		if err != nil {
			return err
		}
		moved, subdir = !held, ""
	}
	relative := func(member string) (string, bool) {
		// Under extract.mkdir, each archive is unpacked in the source
		// directory, so its members are paths below it as they are:
		// abook's tarball, which has no top directory, read as no file to
		// patch, a patch-rejected hold, where the build patched it (the
		// abook session, 2026-10-28).
		if request.Mkdir {
			return member, true
		}
		if rest, ok := strings.CutPrefix(member, worksrc+"/"); ok {
			return rest, true
		}
		if !request.Rename && !moved {
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

// holds says whether any archive has a member inside a directory.
func holds(ctx context.Context, archives []string, directory string) (bool, error) {
	errFound := errors.New("found")
	for _, filename := range archives {
		err := archive.Walk(ctx, filename, func(member archive.Member) error {
			if clean, ok := member.Clean(); ok && strings.HasPrefix(clean, directory+"/") {
				return errFound
			}
			return nil
		})
		if errors.Is(err, errFound) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
	return false, nil
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

// absent are the files of names a directory doesn't have.
func absent(dir string, names []string) []string {
	var missing []string
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); errors.Is(err, os.ErrNotExist) && !slices.Contains(missing, name) {
			missing = append(missing, name)
		}
	}
	return missing
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
