package tart

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify"
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
 case "$9" in
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

func TestTerminalResultPublishedBetweenMarkerAndRunnerReadsWins(t *testing.T) {
	for _, verdict := range []record.Verdict{record.VerdictPassed, record.VerdictFailed} {
		t.Run(string(verdict), func(t *testing.T) {
			root := t.TempDir()
			executable := filepath.Join(root, "tart")
			script := `#!/bin/sh
case "$1" in
list) printf '%s\n' '[{"Name":"vm","Source":"local","State":"running"}]' ;;
exec)
 case "$9" in
 /bin/launchctl) printf '%s\n' 'state = not running' ;;
 /bin/cat) printf '%s\n' '{"State":"finished","Verdict":"` + string(verdict) + `","Protocol":1,"ID":"fixture","Digest":"fixture"}' ;;
 *) printf '%s\n' '{"State":"running","Protocol":1,"ID":"fixture","Digest":"fixture"}' ;;
 esac ;;
esac
`
			require.NoError(t, os.WriteFile(executable, []byte(script), 0700))
			n := &native{config: Config{Home: root, Executable: executable}}
			result, err := n.Inspect(t.Context(), "vm")
			require.NoError(t, err)
			require.Equal(t, "finished", result.State)
			require.Equal(t, verdict, result.Verdict)
			require.Equal(t, "fixture", result.ID)
		})
	}
}

func TestTreeOnlyInputArchivesTheFrozenEditAndRejectsMissingObjects(t *testing.T) {
	f, _ := singleRun(t)
	before, data, err := f.provider.Repo.File(t.Context(), string(f.request.Spec.Source.Tree), "devel/fixture/Portfile")
	require.NoError(t, err)
	edited := append(data, []byte("revision 7\n")...)
	tree, err := f.provider.Repo.EditTree(t.Context(), string(f.request.Spec.Source.Tree), []git.FileEdit{{Path: "devel/fixture/Portfile", Before: before, After: edited, Mode: before.Mode}})
	require.NoError(t, err)
	f.request.Spec.Source = record.Source{Tree: record.ObjectID(tree)}
	require.NoError(t, validateRequest(f.request))
	config, err := f.provider.settings()
	require.NoError(t, err)
	archive, err := makeInput(t.Context(), f.provider.Repo, f.request, config, t.TempDir())
	require.NoError(t, err)
	file, err := os.Open(archive)
	require.NoError(t, err)
	defer file.Close()
	reader := tar.NewReader(file)
	found := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if header.Name == "ports/devel/fixture/Portfile" {
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, edited, data)
			found = true
		}
	}
	require.True(t, found)
	result, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, verify.Admitted, result.State)
	f.request.Spec.Source.Tree = record.ObjectID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	_, err = makeInput(t.Context(), f.provider.Repo, f.request, config, t.TempDir())
	require.Error(t, err)
}

func TestRecordedVerifierIdentityRejectsChangedExecutionCode(t *testing.T) {
	f, m := singleRun(t)
	config, err := f.provider.BuildConfig(t.Context(), testPlatform, BuildOptions{Tests: record.TestDeclared})
	require.NoError(t, err)
	require.NotEmpty(t, config.VerifierDigest)
	f.request.Spec.Config = config
	f.request.Spec.Config.VerifierDigest = "sha256:older-verifier"
	result, err := f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, verify.Unsupported, result.State)
	require.Contains(t, result.Detail, "verifier implementation changed")
	require.Zero(t, m.calls["clone"])
	f.request.Spec.Config = config
	result, err = f.provider.Submit(t.Context(), f.request)
	require.NoError(t, err)
	require.Equal(t, verify.Admitted, result.State)
}

func TestGuestCommandsCloseInheritedDescriptorsAndPreserveInputAndArguments(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "tart")
	require.NoError(t, os.WriteFile(executable, []byte(`#!/bin/sh
set -eu
[ "$1" = exec ]
shift
[ "$1" = -i ]
shift 2
exec 9>/dev/null
exec "$@"
`), 0700))
	n := &native{config: Config{Home: root, Executable: executable}}
	var output bytes.Buffer
	argument := "spaces; $(do-not-execute) 'literal'"
	_, err := n.execGuest(t.Context(), "vm", strings.NewReader("payload\n"), &output, "/bin/sh", "-c", `
[ ! -e /dev/fd/9 ] || exit 42
read -r input
printf '%s\n%s\n' "$input" "$1"
`, "check", argument)
	require.NoError(t, err)
	require.Equal(t, "payload\n"+argument+"\n", output.String())
}
