package portindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/macports"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

const (
	cacheLayout          = "portindex-generations-1"
	cacheLockName        = "cache.lock"
	environmentFileName  = "environment.json"
	generationsDirectory = "generations"
	generationFileName   = "generation.json"
	latestFileName       = "latest"
	recentSeedLimit      = 8
)

// A generation is one completed index of an immutable source tree. Strict
// generations indexed every changed port; others may omit ports that failed to
// parse, so a strict request never reuses them.
type generation struct {
	Tree    string    `json:"tree"`
	Seed    string    `json:"seed,omitempty"`
	Changed int       `json:"changed_paths"`
	Full    bool      `json:"full"`
	Strict  bool      `json:"strict"`
	Built   time.Time `json:"built_at"`
	// Duration is how long the indexer ran, in milliseconds, so the cost
	// of a full pass is a measurement rather than a figure.
	Duration int64 `json:"duration_ms"`
	// Mirror is the provenance of a generation seeded from the mirror's
	// index rather than from a generation of this cache.
	Mirror *mirrorProvenance `json:"mirror,omitempty"`
}

type environment struct {
	Layout     string          `json:"layout"`
	Executable string          `json:"executable"`
	Digest     string          `json:"digest"`
	Runtime    string          `json:"runtime,omitempty"`
	Platform   record.Platform `json:"platform"`
	// Variables is what the indexer was told the platform looks like.
	Variables string `json:"variables,omitempty"`
}

// cache is one indexing environment within the shared cache root. Its shared
// lock keeps collection from removing generations while they are in use;
// builders of one generation serialize on that generation's own lock.
type cache struct {
	config    Config
	platform  record.Platform
	variables string
	directory string
	guard     *os.File
}

func openCache(ctx context.Context, c Config, platform record.Platform) (*cache, error) {
	for _, value := range []string{platform.OS, platform.Version, platform.Architecture} {
		if value == "" || strings.ContainsAny(value, "/\\\x00\r\n\t ") {
			return nil, fmt.Errorf("portindex: complete platform required for indexing")
		}
	}
	if c.Digest == "" {
		return nil, fmt.Errorf("portindex: resolved executable identity required")
	}
	// A generation is a function of what the indexer was told about the
	// platform, so the variables are part of the identity: an index built
	// under an earlier description is not reused under this one.
	variables, err := macports.PlatformVariables(platform)
	if err != nil {
		return nil, err
	}
	identity := digest([]byte(strings.Join([]string{cacheLayout, c.Digest, c.Runtime, platform.OS, platform.Version, platform.Architecture, variables}, "\x00")))
	directory := filepath.Join(c.CacheDirectory, identity)
	guard, err := filelock.Acquire(ctx, filepath.Join(directory, cacheLockName), filelock.Shared)
	if err != nil {
		return nil, err
	}
	result := &cache{config: c, platform: platform, variables: variables, directory: directory, guard: guard}
	if err := result.describe(); err != nil {
		guard.Close()
		return nil, err
	}
	return result, nil
}

func (c *cache) Close() error { return c.guard.Close() }

// describe records the environment beside its generations for inspection.
func (c *cache) describe() error {
	path := filepath.Join(c.directory, environmentFileName)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	encoded, err := json.Marshal(environment{Layout: cacheLayout, Executable: c.config.Executable, Digest: c.config.Digest, Runtime: c.config.Runtime, Platform: c.platform, Variables: c.variables})
	if err != nil {
		return err
	}
	return writeAtomically(path, append(encoded, '\n'))
}

func (c *cache) generation(tree string) string {
	return filepath.Join(c.directory, generationsDirectory, tree)
}

func readGeneration(directory string) (generation, bool) {
	var meta generation
	data, err := os.ReadFile(filepath.Join(directory, generationFileName))
	if err != nil || json.Unmarshal(data, &meta) != nil || !git.ValidObjectID(meta.Tree) {
		return generation{}, false
	}
	return meta, true
}

// usable reports whether a completed generation satisfies the request. Files
// alone never certify coverage; the recorded metadata must agree.
func (c *cache) usable(tree string, strict bool) bool {
	directory := c.generation(tree)
	if !validIndexEntry(directory) {
		return false
	}
	meta, ok := readGeneration(directory)
	return ok && meta.Tree == tree && (!strict || meta.Strict)
}

// ensure returns the directory of a usable generation for the tree, building
// it when needed. An empty root materializes the tree privately for indexing.
// Seeds name preferred trees to derive from; without them the latest pointer
// and recently used generations are tried, and then, for a non-strict request
// with the mirror enabled, the mirror's index bracketed to the tree's commit.
// The latest pointer advances only when requested, so candidate trees never
// displace an upstream seed.
func (c *cache) ensure(ctx context.Context, repo *git.Repository, tree, root string, strict bool, seeds []string, advance bool, commit string) (string, error) {
	if !git.ValidObjectID(tree) {
		return "", fmt.Errorf("portindex: invalid source tree %q", tree)
	}
	target := c.generation(tree)
	if c.usable(tree, strict) {
		progress.DebugReport(ctx, "Using cached PortIndex for source %s", tree[:12])
		return target, touchEntry(target)
	}
	lockPath := target + ".lock"
	guard, err := filelock.TryExisting(ctx, lockPath, filelock.Exclusive)
	switch {
	case errors.Is(err, filelock.ErrBusy):
		progress.Report(ctx, "Waiting for another process to finish indexing this source")
		progress.VerboseReport(ctx, "Waiting for another process indexing source %s", tree[:12])
		fallthrough
	case errors.Is(err, os.ErrNotExist):
		guard, err = filelock.Acquire(ctx, lockPath, filelock.Exclusive)
	}
	if err != nil {
		return "", err
	}
	defer guard.Close()
	if c.usable(tree, strict) {
		return target, touchEntry(target)
	}
	if root == "" {
		if repo == nil {
			return "", fmt.Errorf("portindex: repository required to materialize source %s", tree)
		}
		snapshot, err := repo.Materialize(ctx, tree)
		if err != nil {
			return "", err
		}
		defer snapshot.Close()
		root = snapshot.Root
	}
	// A sparse workspace handed in as the root widens to the whole tree
	// before the indexer lists it.
	if err := workspace.WidenAt(ctx, root); err != nil {
		return "", err
	}
	seed, changed, err := c.selectSeed(ctx, repo, tree, seeds)
	if err != nil {
		return "", err
	}
	meta := generation{Tree: tree}
	if seed == "" && !strict && c.config.Mirror != nil && repo != nil && git.ValidObjectID(commit) {
		var provenance *mirrorProvenance
		seed, changed, provenance, err = c.mirrorSeed(ctx, repo, commit, tree)
		if err != nil {
			return "", err
		}
		if seed != "" {
			defer os.RemoveAll(seed)
			meta.Mirror = provenance
		}
	}
	if err := buildPortIndex(ctx, c.config, c.platform, root, target, seed, changed, strict, guard, meta); err != nil {
		return "", err
	}
	if advance {
		if err := writeAtomically(filepath.Join(c.directory, latestFileName), []byte(tree+"\n")); err != nil {
			return "", err
		}
	}
	return target, nil
}

// selectSeed picks the first usable seed whose tree diff can be established
// and does not touch shared resources. It reads Git only for candidates that
// exist as completed generations.
func (c *cache) selectSeed(ctx context.Context, repo *git.Repository, tree string, preferred []string) (string, []string, error) {
	candidates := append([]string(nil), preferred...)
	if len(candidates) == 0 {
		if latest := c.latest(); latest != "" {
			candidates = append(candidates, latest)
		}
		recent, err := c.recentGenerations(recentSeedLimit)
		if err != nil {
			return "", nil, err
		}
		candidates = append(candidates, recent...)
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		if candidate == "" || candidate == tree || seen[candidate] || !git.ValidObjectID(candidate) || repo == nil {
			continue
		}
		seen[candidate] = true
		directory := c.generation(candidate)
		if !validIndexEntry(directory) {
			continue
		}
		if typ, err := repo.ObjectType(ctx, candidate); err != nil || typ != "tree" {
			if ctx.Err() != nil {
				return "", nil, ctx.Err()
			}
			continue
		}
		paths, err := repo.ChangedPaths(ctx, candidate, tree)
		if err != nil {
			if ctx.Err() != nil {
				return "", nil, ctx.Err()
			}
			continue
		}
		if requiresFullIndex(paths) {
			continue
		}
		return directory, paths, nil
	}
	return "", nil, nil
}

func (c *cache) latest() string {
	data, err := os.ReadFile(filepath.Join(c.directory, latestFileName))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(data))
	if !git.ValidObjectID(value) {
		return ""
	}
	return value
}

// recentGenerations lists completed generations by most recent use.
func (c *cache) recentGenerations(limit int) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(c.directory, generationsDirectory))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type candidate struct {
		tree string
		used time.Time
	}
	var found []candidate
	for _, entry := range entries {
		if !entry.IsDir() || !git.ValidObjectID(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found = append(found, candidate{tree: entry.Name(), used: info.ModTime()})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].used.After(found[j].used) })
	var result []string
	for _, item := range found {
		if len(result) == limit {
			break
		}
		result = append(result, item.tree)
	}
	return result, nil
}

func writeAtomically(path string, data []byte) error { return atomicfile.Write(path, data, 0600) }
