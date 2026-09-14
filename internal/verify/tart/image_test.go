package tart

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/stretchr/testify/require"
)

type countedImageStore struct {
	state.ProviderStore
	writes int
}

func (s *countedImageStore) PutImageDigest(ctx context.Context, value state.ImageDigest) error {
	s.writes++
	return s.ProviderStore.PutImageDigest(ctx, value)
}

type imageProcessResult struct {
	Environment Environment
	Published   bool
}

func TestImageDigestProcess(t *testing.T) {
	root := os.Getenv("DOCKHAND_IMAGE_FIXTURE")
	if root == "" {
		t.Skip("subprocess helper")
	}
	store, err := sqlite.Open(t.Context(), filepath.Join(root, "state.db"), sqlite.Options{})
	require.NoError(t, err)
	defer store.Close()
	counted := &countedImageStore{ProviderStore: store}
	p := imageFixtureProvider(root)
	p.State = counted
	environment, err := p.DescribeEnvironment(t.Context())
	require.NoError(t, err)
	data, err := json.Marshal(imageProcessResult{environment, counted.writes != 0})
	require.NoError(t, err)
	fmt.Printf("IMAGE_RESULT=%s\n", data)
}

func imageFixtureProvider(root string) *Provider {
	return &Provider{Config: Config{Home: root, Image: "base", ArtifactDirectory: filepath.Join(root, "artifacts"), Executable: filepath.Join(root, "tart"), Platform: testPlatform}}
}
func imageFixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "vms", "base"), 0700))
	for _, name := range []string{"config.json", "disk.img", "nvram.bin"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "vms", "base", name), []byte("original"), 0600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "tart"), []byte("#!/bin/sh\nprintf '%s\\n' '[{\"Name\":\"base\",\"Source\":\"local\",\"State\":\"stopped\"}]'\n"), 0700))
	return root
}
func imageProcess(t *testing.T, root string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestImageDigestProcess$", "-test.v")
	cmd.Env = append(os.Environ(), "DOCKHAND_IMAGE_FIXTURE="+root)
	return cmd
}
func imageResult(t *testing.T, output []byte) imageProcessResult {
	t.Helper()
	for _, line := range strings.Split(string(output), "\n") {
		if raw, ok := strings.CutPrefix(line, "IMAGE_RESULT="); ok {
			var result imageProcessResult
			require.NoError(t, json.Unmarshal([]byte(raw), &result))
			return result
		}
	}
	t.Fatalf("missing image result: %s", output)
	return imageProcessResult{}
}
func TestImageCacheSurvivesProcessesAndInvalidatesChangedFiles(t *testing.T) {
	root := imageFixture(t)
	run := func() imageProcessResult {
		output, err := imageProcess(t, root).CombinedOutput()
		require.NoError(t, err, "%s", output)
		return imageResult(t, output)
	}
	first := run()
	require.True(t, first.Published)
	again := run()
	require.False(t, again.Published, "a new process must use the persisted digest")
	require.Equal(t, first.Environment, again.Environment)
	disk := filepath.Join(root, "vms", "base", "disk.img")
	info, err := os.Stat(disk)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(disk, []byte("modified"), 0600))
	require.NoError(t, os.Chtimes(disk, info.ModTime(), info.ModTime()))
	changed := run()
	require.True(t, changed.Published)
	require.NotEqual(t, first.Environment.Digest, changed.Environment.Digest)
	replacement := disk + ".replacement"
	require.NoError(t, os.WriteFile(replacement, []byte("modified"), 0600))
	require.NoError(t, os.Chtimes(replacement, info.ModTime(), info.ModTime()))
	require.NoError(t, os.Rename(replacement, disk))
	replaced := run()
	require.True(t, replaced.Published, "replacement requires hashing even when its bytes match")
	require.Equal(t, changed.Environment, replaced.Environment)
	require.NoError(t, os.Remove(disk))
	output, err := imageProcess(t, root).CombinedOutput()
	require.Error(t, err, "cache must not conceal a missing image: %s", output)
}
func TestImageCacheChecksConfigContentsWithoutRehashingForChangeTimeAlone(t *testing.T) {
	root := imageFixture(t)
	run := func() imageProcessResult {
		output, err := imageProcess(t, root).CombinedOutput()
		require.NoError(t, err, "%s", output)
		return imageResult(t, output)
	}
	first := run()
	config := filepath.Join(root, "vms", "base", "config.json")
	info, err := os.Stat(config)
	require.NoError(t, err)
	require.NoError(t, os.Chtimes(config, info.ModTime(), info.ModTime()))
	unchanged := run()
	require.False(t, unchanged.Published, "metadata-only configuration touches must not rehash the disk")
	require.Equal(t, first.Environment, unchanged.Environment)
	require.NoError(t, os.WriteFile(config, []byte("modified"), 0600))
	require.NoError(t, os.Chtimes(config, info.ModTime(), info.ModTime()))
	changed := run()
	require.True(t, changed.Published)
	require.NotEqual(t, first.Environment.Digest, changed.Environment.Digest, "configuration bytes must be checked even with unchanged size and modification time")
}

func TestConcurrentImageCacheProcessesPublishConsistentDigests(t *testing.T) {
	root := imageFixture(t)
	store, err := sqlite.Open(t.Context(), filepath.Join(root, "state.db"), sqlite.Options{})
	require.NoError(t, err)
	defer store.Close()
	cmds := make([]*exec.Cmd, 4)
	outputs := make([]bytes.Buffer, 4)
	for i := range cmds {
		cmds[i] = imageProcess(t, root)
		cmds[i].Stdout, cmds[i].Stderr = &outputs[i], &outputs[i]
		require.NoError(t, cmds[i].Start())
	}
	_, err = store.RegisterRepository(t.Context(), filepath.Join(root, "another-repository"))
	require.NoError(t, err, "cache population must allow ordinary state writes")
	var digest string
	for i, cmd := range cmds {
		require.NoError(t, cmd.Wait(), "%s", outputs[i].String())
		result := imageResult(t, outputs[i].Bytes())
		if digest == "" {
			digest = result.Environment.Digest
		}
		require.Equal(t, digest, result.Environment.Digest)
	}
	cached, err := store.ImageDigest(t.Context(), "tart", filepath.Join(root, "vms", "base"))
	require.NoError(t, err)
	require.Equal(t, digest, cached.Digest)
}

type interruptedImageCache struct {
	lookup func()
	writes int
}

func (s *interruptedImageCache) ImageDigest(context.Context, string, string) (state.ImageDigest, error) {
	s.lookup()
	return state.ImageDigest{}, state.ErrNotFound
}
func (s *interruptedImageCache) PutImageDigest(context.Context, state.ImageDigest) error {
	s.writes++
	return nil
}
func TestInterruptedOrChangedImageDoesNotPublishDigest(t *testing.T) {
	for _, mode := range []string{"canceled", "changed"} {
		t.Run(mode, func(t *testing.T) {
			root := imageFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cache := &interruptedImageCache{lookup: func() {
				if mode == "canceled" {
					cancel()
				} else {
					require.NoError(t, os.WriteFile(filepath.Join(root, "vms", "base", "disk.img"), []byte("different image"), 0600))
				}
			}}
			n := &native{config: imageFixtureProvider(root).Config, images: &imageCache{}, cache: cache}
			_, err := n.Environment(ctx)
			require.Error(t, err)
			if mode == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorContains(t, err, "image changed")
			}
			require.Zero(t, cache.writes)
		})
	}
}
