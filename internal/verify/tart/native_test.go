package tart

import (
	"archive/tar"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/stretchr/testify/require"
)

func TestImageDigestTracksContentDespiteRestoredModificationTime(t *testing.T) {
	root := t.TempDir()
	vm := filepath.Join(root, "vms", "base")
	require.NoError(t, os.MkdirAll(vm, 0700))
	for _, name := range []string{"config.json", "disk.img", "nvram.bin"} {
		require.NoError(t, os.WriteFile(filepath.Join(vm, name), []byte("original"), 0600))
	}
	executable := filepath.Join(root, "tart")
	require.NoError(t, os.WriteFile(executable, []byte(`#!/bin/sh
printf '%s\n' '[{"Name":"base","Source":"local","State":"stopped"}]'
`), 0700))
	p := &Provider{Config: Config{Home: root, Image: "base", ArtifactDirectory: filepath.Join(root, "artifacts"), Executable: executable, Platform: testPlatform}}
	first, err := p.DescribeEnvironment(t.Context())
	require.NoError(t, err)
	again, err := p.DescribeEnvironment(t.Context())
	require.NoError(t, err)
	require.Equal(t, first, again)
	disk := filepath.Join(vm, "disk.img")
	info, err := os.Stat(disk)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(disk, []byte("modified"), 0600))
	require.NoError(t, os.Chtimes(disk, info.ModTime(), info.ModTime()))
	changed, err := p.DescribeEnvironment(t.Context())
	require.NoError(t, err)
	require.NotEqual(t, first.Digest, changed.Digest)
	require.NoError(t, os.Remove(disk))
	_, err = p.DescribeEnvironment(t.Context())
	require.Error(t, err, "cached identity cannot conceal a missing image")
}
func TestStagedInputUsesAcceptedGitObjects(t *testing.T) {
	f, _ := singleRun(t)
	config, err := f.provider.settings()
	require.NoError(t, err)
	directory := t.TempDir()
	archive, err := makeInput(t.Context(), f.provider.Repo, f.request, config, directory)
	require.NoError(t, err)
	file, err := os.Open(archive)
	require.NoError(t, err)
	defer file.Close()
	entries := map[string][]byte{}
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		entries[header.Name] = data
	}
	require.Equal(t, "PortSystem 1.0\nname fixture\nversion 1\n", string(entries["ports/devel/fixture/Portfile"]))
	var input guestInput
	require.NoError(t, json.Unmarshal(entries["input.json"], &input))
	require.Equal(t, f.request.Spec, input.Spec)
	require.Equal(t, buildDigest(f.request.Spec), input.Digest)
	require.Equal(t, guestScript, entries["guest.tcl"])
	require.NotEmpty(t, entries["guest.plist"])
	changed := f.request
	changed.Spec.Source.Tree = record.ObjectID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	_, err = makeInput(t.Context(), f.provider.Repo, changed, config, directory)
	require.ErrorContains(t, err, "commit and tree disagree")
}
func TestRunningMarkerDoesNotHideExitedGuestRunner(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "tart")
	require.NoError(t, os.WriteFile(executable, []byte(`#!/bin/sh
case "$1" in
list) printf '%s\n' '[{"Name":"vm","Source":"local","State":"running"}]' ;;
exec)
 case "$5" in
  /bin/launchctl) printf '%s\n' 'state = not running' ;;
  *) printf '%s\n' '{"State":"running","Protocol":1,"ID":"fixture","Digest":"fixture"}' ;;
 esac ;;
esac
`), 0700))
	n := &native{config: Config{Home: root, Executable: executable}}
	result, err := n.Inspect(t.Context(), "vm")
	require.NoError(t, err)
	require.Equal(t, "runner-exited", result.State)
}
