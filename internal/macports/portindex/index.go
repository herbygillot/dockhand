package portindex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/subprocess"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

const portIndexName = "PortIndex"
const quickIndexName = "PortIndex.quick"
const runtimeProbeTimeout = 30 * time.Second
const maxPortIndexBytes = 128 << 20

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Config freezes the indexer identity and names the cache shared by every
// consumer. Runtime is the MacPorts Base the executable loads; ResolveTool
// probes it because hashing the launcher alone would miss a Base upgrade.
type Config struct {
	Executable     string
	Digest         string
	Runtime        string
	CacheDirectory string
	// Mirror enables the MacPorts mirror's index as a bootstrap seed for a
	// cache with no usable generation; nil keeps indexing offline, which
	// assess and outdated rely on.
	Mirror *Mirror
}

// DefaultMirrorURL returns the MacPorts mirror index for a platform. Recorded
// provider settings keep it as bootstrap provenance; staging does not use it
// because a mirrored index cannot prove which source tree it describes.
//
// The mirror names an index by the kernel architecture MacPorts reports as
// os.arch, arm or i386, not by the build architecture a platform record
// carries, so arm64 maps to arm and x86_64 to i386.
func DefaultMirrorURL(platform record.Platform) (string, error) {
	for _, value := range []string{platform.OS, platform.Version, platform.Architecture} {
		if value == "" || strings.ContainsAny(value, "/\\\x00\r\n\t ") {
			return "", fmt.Errorf("portindex: complete platform required for mirror")
		}
	}
	arch, ok := mirrorArchitectures[platform.Architecture]
	if !ok {
		return "", fmt.Errorf("portindex: no mirror index is published for the %s architecture", platform.Architecture)
	}
	profile := strings.Join([]string{platform.OS, platform.Version, arch}, "_")
	return "https://ftp.fau.de/macports/release/tarballs/PortIndex_" + profile + "/PortIndex", nil
}

// mirrorArchitectures maps a build architecture to the kernel architecture
// the mirror's index directories are named by.
var mirrorArchitectures = map[string]string{"arm64": "arm", "x86_64": "i386", "i386": "i386", "ppc": "powerpc", "powerpc": "powerpc"}

// ResolveTool records and verifies the selected portindex executable identity.
func ResolveTool(ctx context.Context, c Config) (Config, error) {
	path := c.Executable
	if path == "" {
		var err error
		path, err = exec.LookPath("portindex")
		if err != nil {
			return c, fmt.Errorf("portindex: locating executable: %w", err)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return c, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return c, fmt.Errorf("portindex: locating executable: %w", err)
	}
	file, err := os.Open(abs)
	if err != nil {
		return c, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return c, err
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return c, fmt.Errorf("portindex: executable is not an executable regular file")
	}
	hash := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if err = ctx.Err(); err != nil {
			return c, err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return c, readErr
		}
	}
	identity := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if c.Digest != "" && c.Digest != identity {
		return c, fmt.Errorf("portindex: executable changed after the build was accepted")
	}
	runtime, err := runtimeIdentity(ctx, abs)
	if err != nil {
		return c, err
	}
	c.Executable, c.Digest, c.Runtime = abs, identity, runtime
	return c, nil
}

// runtimeIdentity names the MacPorts Base loaded by a Tcl launcher. Other
// executables, such as test stand-ins, carry no runtime beyond their digest.
func runtimeIdentity(ctx context.Context, executable string) (string, error) {
	file, err := os.Open(executable)
	if err != nil {
		return "", err
	}
	line, err := bufio.NewReader(file).ReadString('\n')
	file.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if !strings.HasPrefix(line, "#!") {
		return "", nil
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 || !strings.HasPrefix(filepath.Base(fields[0]), "tclsh") {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeProbeTimeout)
	defer cancel()
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "tclsh", Path: fields[0], Args: fields[1:], Stdin: strings.NewReader("package require macports\nputs [macports::version]\n"), Env: indexerEnvironment("")})
	if err != nil {
		return "", fmt.Errorf("portindex: probing the MacPorts runtime of %s: %w", executable, err)
	}
	version := strings.TrimSpace(string(result.Output))
	if version == "" || strings.ContainsAny(version, " \t\r\n\x00") {
		return "", fmt.Errorf("portindex: MacPorts runtime version is unavailable for %s", executable)
	}
	return "macports-" + version, nil
}

func indexerEnvironment(configuration string) []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PORTSRC=") && !strings.HasPrefix(entry, "LC_ALL=") {
			env = append(env, entry)
		}
	}
	if configuration != "" {
		env = append(env, "PORTSRC="+configuration)
	}
	return append(env, "LC_ALL=C")
}

// Stage installs the PortIndex for an immutable source tree into an already
// materialized source root. Completed generations are shared by every consumer
// naming the same tree and indexing environment; a contribution's candidate
// derives from the generation of its recorded base.
func Stage(ctx context.Context, repo *git.Repository, source record.Source, platform record.Platform, c Config, root string) error {
	resolved, err := ResolveTool(ctx, c)
	if err != nil {
		return err
	}
	if resolved.CacheDirectory == "" {
		return fmt.Errorf("portindex: cache directory is required")
	}
	if !git.ValidObjectID(string(source.Tree)) {
		return fmt.Errorf("portindex: source tree is required")
	}
	cache, err := openCache(ctx, resolved, platform)
	if err != nil {
		return err
	}
	defer cache.Close()
	baseTree, err := sourceBaseTree(ctx, repo, source)
	if err != nil {
		return err
	}
	tree := string(source.Tree)
	var entry string
	if baseTree != "" && baseTree != tree {
		// The base generation is an exact seed for the candidate and the
		// preferred seed for later work from the same upstream. The base
		// names its commit when it is one, which is what the mirror
		// bootstrap brackets.
		baseCommit := ""
		if string(source.Base) != baseTree {
			baseCommit = string(source.Base)
		}
		if _, err := cache.ensure(ctx, repo, baseTree, "", false, nil, true, baseCommit); err != nil {
			return err
		}
		entry, err = cache.ensure(ctx, repo, tree, root, true, []string{baseTree}, false, string(source.Commit))
	} else {
		entry, err = cache.ensure(ctx, repo, tree, root, source.Base != "", nil, true, string(source.Commit))
	}
	if err != nil {
		return err
	}
	return install(entry, root)
}

func sourceBaseTree(ctx context.Context, repo *git.Repository, source record.Source) (string, error) {
	if source.Base == "" {
		return "", nil
	}
	if repo == nil {
		return "", fmt.Errorf("portindex: repository required to resolve the source base")
	}
	typ, err := repo.ObjectType(ctx, string(source.Base))
	if err != nil {
		return "", err
	}
	if typ == "tree" {
		return string(source.Base), nil
	}
	if typ != "commit" {
		return "", fmt.Errorf("portindex: source base is not a commit or tree")
	}
	trees, err := repo.CommitTrees(ctx, []string{string(source.Base)})
	return trees[string(source.Base)], err
}

func requiresFullIndex(paths []string) bool {
	for _, name := range paths {
		if name == "_resources" || strings.HasPrefix(name, "_resources/") {
			return true
		}
	}
	return false
}

// install copies a completed generation into the staged root, skipping files
// already installed from the same generation. Each file lands by rename, so
// a reader of a root shared with another consumer never sees a truncated
// index.
func install(entry, root string) error {
	for _, name := range []string{portIndexName, quickIndexName} {
		source, destination := filepath.Join(entry, name), filepath.Join(root, name)
		if sameFile(source, destination) {
			continue
		}
		if err := copyIndexFile(source, destination); err != nil {
			return err
		}
	}
	return nil
}

func sameFile(source, destination string) bool {
	a, err := os.Stat(source)
	if err != nil {
		return false
	}
	b, err := os.Lstat(destination)
	return err == nil && b.Mode().IsRegular() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// buildPortIndex runs the indexer for one immutable source root and publishes
// the complete result atomically. An empty seed requests a full pass; otherwise
// the seed's entries are reused and the changed port directories are reindexed.
// The guard, when present, is inherited by the indexer so the generation lock
// outlives a parent that exits mid-build.
func buildPortIndex(ctx context.Context, c Config, platform record.Platform, sourceRoot, destination, seed string, changed []string, strict bool, guard *os.File, meta generation) (err error) {
	short := meta.Tree
	if len(short) > 12 {
		short = short[:12]
	}
	if scope, ok := workspace.ScopeOf(sourceRoot); ok && !scope.All {
		// The indexer lists what the root holds; a port absent from a
		// sparse projection would be indexed as absent from the tree and
		// cached under the tree id for every later process.
		return fmt.Errorf("portindex: index generation needs the whole tree; the workspace holds only %v", scope.Ports)
	}
	if seed == "" {
		progress.Report(ctx, "Building the PortIndex; this may take several minutes")
		progress.VerboseReport(ctx, "Generating full PortIndex for source %s", short)
	} else {
		progress.VerboseReport(ctx, "Updating PortIndex for source %s from %d changed paths", short, len(changed))
	}
	started := time.Now()
	err = atomicfile.ReplaceDirectory(destination, func(temp string) (err error) {
		configRoot, err := os.MkdirTemp(filepath.Dir(destination), ".portindex-config-")
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, os.RemoveAll(configRoot)) }()
		args := []string{"-q"}
		if seed == "" {
			args = append(args, "-f")
		} else {
			if err = copyIndexFile(filepath.Join(seed, portIndexName), filepath.Join(temp, portIndexName)); err != nil {
				return err
			}
			quick := filepath.Join(seed, quickIndexName)
			if _, statErr := os.Stat(quick); statErr == nil {
				if err = copyIndexFile(quick, filepath.Join(temp, quickIndexName)); err != nil {
					return err
				}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
			if err = touchEntry(seed); err != nil {
				return err
			}
			indexInfo, err := os.Stat(filepath.Join(temp, portIndexName))
			if err != nil {
				return err
			}
			if err = setPortfileTimes(sourceRoot, changed, indexInfo.ModTime()); err != nil {
				return err
			}
		}
		if strict {
			args = append(args, "-e")
		}
		args = append(args, "-p", strings.Join([]string{platform.OS, platform.Version, platform.Architecture}, "_"), "-o", temp, sourceRoot)
		configuration := filepath.Join(configRoot, "macports.conf")
		sources := filepath.Join(configRoot, "sources.conf")
		variants := filepath.Join(configRoot, "variants.conf")
		portdb := filepath.Join(configRoot, "portdb")
		if err = os.MkdirAll(filepath.Join(portdb, "registry"), 0700); err != nil {
			return err
		}
		if err = os.WriteFile(sources, []byte((&url.URL{Scheme: "file", Path: sourceRoot}).String()+" [default,nosync]\n"), 0600); err != nil {
			return err
		}
		if err = os.WriteFile(variants, nil, 0600); err != nil {
			return err
		}
		configurationText := fmt.Sprintf("sources_conf %s\nvariants_conf %s\nportdbpath %s\n", sources, variants, portdb)
		if err = os.WriteFile(configuration, []byte(configurationText), 0600); err != nil {
			return err
		}
		_, runErr := subprocess.Run(ctx, subprocess.Spec{Tool: "portindex", Path: c.Executable, Args: args, Dir: sourceRoot, Env: indexerEnvironment(configuration), Combined: true, ExtraFiles: []*os.File{guard}})
		if runErr != nil {
			var exit *exec.ExitError
			if ctx.Err() == nil && strict && seed != "" && errors.As(runErr, &exit) && exit.ExitCode() == 2 {
				if coverageErr := validateIncrementalCoverage(seed, temp, sourceRoot, changed); coverageErr == nil {
					runErr = nil
				} else {
					runErr = errors.Join(runErr, coverageErr)
				}
			}
			if runErr != nil {
				return runErr
			}
		}
		if !validIndexEntry(temp) {
			return fmt.Errorf("portindex: executable produced an incomplete index")
		}
		meta.Strict, meta.Changed, meta.Full, meta.Built = strict, len(changed), seed == "", time.Now().UTC()
		meta.Duration = time.Since(started).Milliseconds()
		if seed != "" && meta.Mirror == nil {
			meta.Seed = filepath.Base(seed)
		}
		encoded, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(temp, generationFileName), encoded, 0600)
	})
	if err != nil {
		return err
	}
	pass := "incremental"
	if seed == "" {
		pass = "full"
	} else if meta.Mirror != nil {
		pass = "incremental from the mirror"
	}
	progress.VerboseReport(ctx, "PortIndex generated (%s pass, %s)", pass, time.Since(started).Round(time.Second))
	return nil
}

func setPortfileTimes(root string, changed []string, indexTime time.Time) error {
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && entry.Name() == "Portfile" {
			return os.Chtimes(path, indexTime, indexTime)
		}
		return nil
	}); err != nil {
		return err
	}
	newer := indexTime.Add(time.Second)
	seen := map[string]bool{}
	for _, name := range changed {
		parts := strings.Split(filepath.ToSlash(name), "/")
		if len(parts) < 3 || parts[0] == "_resources" {
			continue
		}
		portfile := filepath.Join(root, filepath.FromSlash(parts[0]+"/"+parts[1]+"/Portfile"))
		if seen[portfile] {
			continue
		}
		seen[portfile] = true
		if err := os.Chtimes(portfile, newer, newer); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func validIndexEntry(directory string) bool {
	for _, name := range []string{portIndexName, quickIndexName} {
		info, err := os.Lstat(filepath.Join(directory, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return false
		}
	}
	return true
}

// copyIndexFile writes the source's bytes and mtime to a temporary name
// beside the destination and renames it into place.
func copyIndexFile(source, destination string) (err error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.Join(err, fmt.Errorf("portindex: cached index is not a regular file"))
	}
	output, err := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+".*")
	if err != nil {
		return err
	}
	temp := output.Name()
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(temp))
		}
	}()
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil {
		copyErr = os.Chtimes(temp, info.ModTime(), info.ModTime())
	}
	if copyErr == nil {
		copyErr = os.Rename(temp, destination)
	}
	return copyErr
}
