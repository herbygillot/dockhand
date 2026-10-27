package binaryarchive

import (
	"archive/tar"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// An archive is named as MacPorts' buildbots name one, variants signed in
// name order.
func TestAnArchiveIsNamedAsMacPortsNamesIt(t *testing.T) {
	t.Parallel()
	require.Equal(t, "jq-1.8.1_0.darwin_25.arm64.tbz2", ArchiveName("jq", "1.8.1", "0", nil, 25, "arm64"))
	require.Equal(t, "exiv2-0.28.3_1+docs+universal.darwin_23.x86_64.tbz2", ArchiveName("exiv2", "0.28.3", "1", []string{"universal", "docs"}, 23, "x86_64"))
	require.Equal(t, "https://packages.macports.org/jq/jq-1.8.1_0.darwin_25.arm64.tbz2", URL("jq", ArchiveName("jq", "1.8.1", "0", nil, 25, "arm64")))
}

// An archive's files are read as it installs them, its metadata left out,
// and its libraries' install names and compatibility versions from their
// LC_ID_DYLIB, with Go's standard library alone (Then's item 9).
func TestAnArchivesFilesAndLibrariesAreRead(t *testing.T) {
	t.Parallel()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no cc to build a library with")
	}
	bzip, err := exec.LookPath("bzip2")
	if err != nil {
		t.Skip("no bzip2 to pack an archive with")
	}
	dir := t.TempDir()
	source, library := filepath.Join(dir, "x.c"), filepath.Join(dir, "libx.1.dylib")
	require.NoError(t, os.WriteFile(source, []byte("int x(void) { return 1; }\n"), 0o644))
	out, err := exec.CommandContext(t.Context(), cc, "-dynamiclib", "-install_name", "/opt/local/lib/libx.1.dylib", "-compatibility_version", "1.2.0", "-o", library, source).CombinedOutput()
	if err != nil {
		t.Skipf("cc couldn't build a library: %s", out)
	}
	data, err := os.ReadFile(library)
	require.NoError(t, err)
	var packed bytes.Buffer
	w := tar.NewWriter(&packed)
	for _, member := range []struct {
		name string
		body []byte
	}{{"+CONTENTS", []byte("@name x-1_0\n")}, {"./opt/local/lib/libx.1.dylib", data}, {"./opt/local/bin/x", []byte("#!/bin/sh\n")}} {
		require.NoError(t, w.WriteHeader(&tar.Header{Name: member.name, Mode: 0o755, Size: int64(len(member.body)), Typeflag: tar.TypeReg}))
		_, err := w.Write(member.body)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	command := exec.CommandContext(t.Context(), bzip, "-c")
	command.Stdin = &packed
	archive, err := command.Output()
	require.NoError(t, err)
	files, libraries, err := Contents(bytes.NewReader(archive))
	require.NoError(t, err)
	require.Equal(t, []string{"/opt/local/lib/libx.1.dylib", "/opt/local/bin/x"}, files, "+CONTENTS is metadata")
	require.Equal(t, []model.Library{{File: "/opt/local/lib/libx.1.dylib", Name: "/opt/local/lib/libx.1.dylib", Compatibility: "1.2.0"}}, libraries)
}
