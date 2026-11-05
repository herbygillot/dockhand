package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestWalkReadsTarGzipAndZip(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./root/a.txt", Mode: 0600, Size: 3, Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte("abc"))
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "a.txt"}))
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	tarball := filepath.Join(t.TempDir(), "s.tar.gz")
	require.NoError(t, os.WriteFile(tarball, data.Bytes(), 0600))
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	w, err := zw.Create("root/a.txt")
	require.NoError(t, err)
	_, _ = w.Write([]byte("abc"))
	header := &zip.FileHeader{Name: "root/link"}
	header.SetMode(fs.ModeSymlink | 0o777)
	w, err = zw.CreateHeader(header)
	require.NoError(t, err)
	_, _ = w.Write([]byte("a.txt"))
	require.NoError(t, zw.Close())
	zipfile := filepath.Join(t.TempDir(), "s.zip")
	require.NoError(t, os.WriteFile(zipfile, zipped.Bytes(), 0600))
	for _, file := range []string{tarball, zipfile} {
		var names []string
		links := map[string]string{}
		require.NoError(t, Walk(t.Context(), file, func(m Member) error {
			clean, ok := m.Clean()
			require.True(t, ok)
			if m.Regular {
				require.Empty(t, m.Link)
				digest, err := m.Digest()
				require.NoError(t, err)
				require.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", digest, "abc's SHA-256")
			}
			if m.Link != "" {
				links[clean] = m.Link
			}
			names = append(names, clean)
			return nil
		}))
		require.Equal(t, "root/a.txt", names[0])
		require.Equal(t, map[string]string{"root/link": "a.txt"}, links, "a symbolic link's target, in tar and zip")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, Walk(ctx, tarball, func(Member) error { return nil }), context.Canceled)
	for _, name := range []string{"/abs", "../up", "a/../b", "."} {
		_, ok := Member{Name: name}.Clean()
		require.False(t, ok, name)
	}
}

// An archive past the scan limit says so, whether the limit falls between
// members or within one, rather than "unexpected EOF": rustc's source, 3.5
// GiB uncompressed, read as broken (the rust and cargo run).
func TestAWalkPastTheLimitSaysSo(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"root/a.txt", "root/b.txt"} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: 2048, Typeflag: tar.TypeReg}))
		_, _ = tw.Write(bytes.Repeat([]byte("x"), 2048))
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	tarball := filepath.Join(t.TempDir(), "source-123")
	require.NoError(t, os.WriteFile(tarball, data.Bytes(), 0600))

	saved := scanLimit
	t.Cleanup(func() { scanLimit = saved })
	for _, limit := range []int64{1024, 3072} {
		scanLimit = limit
		err := Walk(t.Context(), tarball, func(m Member) error {
			_, err := io.Copy(io.Discard, m.Body)
			return err
		})
		require.ErrorIs(t, err, errScanLimit, "a limit of %d", limit)
		require.NotContains(t, err.Error(), "source-123", "the temporary file isn't named")
	}
	scanLimit = 1 << 20
	require.NoError(t, Walk(t.Context(), tarball, func(Member) error { return nil }), "within the limit, the archive ends as it does")

	// A zip archive's members count as they're read, under the same limit,
	// where it had none (the limits sweep); one not read costs nothing.
	var zipped bytes.Buffer
	zw := zip.NewWriter(&zipped)
	for _, name := range []string{"root/a.txt", "root/b.txt"} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
	}
	require.NoError(t, zw.Close())
	zipfile := filepath.Join(t.TempDir(), "source-456")
	require.NoError(t, os.WriteFile(zipfile, zipped.Bytes(), 0600))
	scanLimit = 3000
	err := Walk(t.Context(), zipfile, func(m Member) error {
		_, err := io.Copy(io.Discard, m.Body)
		return err
	})
	require.ErrorIs(t, err, errScanLimit)
	require.ErrorContains(t, err, "the uncompressed archive exceeds the 3 KB read limit")
	require.NoError(t, Walk(t.Context(), zipfile, func(Member) error { return nil }))
}

// An installer package or an executable is said to be a binary, with no
// source in it, rather than misread as a broken tarball: 1password-cli's
// .pkg read as "archive/tar: invalid tar header" (field testing's batch
// 10, finding 2).
func TestABinaryIsSaidToBeOne(t *testing.T) {
	directory := t.TempDir()
	for name, data := range map[string]string{"op_apple_universal_v2.30.0.pkg": "xar!\x00\x1c\x00\x01rest", "tool": "\xcf\xfa\xed\xfe\x07\x00\x00\x01"} {
		file := filepath.Join(directory, name)
		require.NoError(t, os.WriteFile(file, []byte(data), 0o644))
		err := Walk(t.Context(), file, func(Member) error { return nil })
		require.ErrorIs(t, err, ErrBinary, name)
		require.ErrorContains(t, err, name+" is a ")
	}
}

// Every archive Base extracts is read: a .tar.lzma through xz's lzma
// format, a .7z unpacked by 7za, and, on macOS, a .dmg mounted read-only
// by hdiutil, each fixture made by its own tool, skipped where the host
// hasn't it.
func TestWalkReadsLzmaSevenZipAndADiskImage(t *testing.T) {
	source := filepath.Join(t.TempDir(), "root")
	require.NoError(t, os.MkdirAll(source, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "a.txt"), []byte("abc"), 0o644))
	files := func(t *testing.T, file string) map[string]string {
		t.Helper()
		found := map[string]string{}
		require.NoError(t, Walk(t.Context(), file, func(m Member) error {
			if clean, ok := m.Clean(); ok && m.Regular {
				digest, err := m.Digest()
				require.NoError(t, err)
				found[clean] = digest
			}
			return nil
		}))
		return found
	}
	abc := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	tool := func(t *testing.T, name string) {
		t.Helper()
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s isn't on this host", name)
		}
	}
	t.Run("lzma", func(t *testing.T) {
		tool(t, "xz")
		file := filepath.Join(t.TempDir(), "s.tar.lzma")
		out, err := exec.Command("/bin/sh", "-c", `COPYFILE_DISABLE=1 tar -cf - -C "$1" root | xz --format=lzma > "$2"`, "sh", filepath.Dir(source), file).CombinedOutput()
		require.NoError(t, err, string(out))
		require.Equal(t, map[string]string{"root/a.txt": abc}, files(t, file))
	})
	t.Run("7z", func(t *testing.T) {
		tool(t, "7za")
		file := filepath.Join(t.TempDir(), "s.7z")
		out, err := exec.Command("7za", "a", "-bd", file, source).CombinedOutput()
		require.NoError(t, err, string(out))
		require.Equal(t, map[string]string{"root/a.txt": abc}, files(t, file))
	})
	t.Run("dmg", func(t *testing.T) {
		if runtime.GOOS != "darwin" {
			file := filepath.Join(t.TempDir(), "s.dmg")
			require.NoError(t, os.WriteFile(file, []byte("not a tar"), 0o644))
			require.ErrorContains(t, Walk(t.Context(), file, func(Member) error { return nil }), "s.dmg is a disk image, which only macOS's hdiutil reads")
			return
		}
		file := filepath.Join(t.TempDir(), "s.dmg")
		out, err := exec.Command("/usr/bin/hdiutil", "create", "-quiet", "-srcfolder", source, "-format", "UDZO", file).CombinedOutput()
		if err != nil {
			t.Skipf("hdiutil create: %v: %s", err, out)
		}
		require.Equal(t, map[string]string{"a.txt": abc}, files(t, file), "the volume's files, at its root")
	})
}

// A decompressing tool that ends unsuccessfully fails the reading, with
// what it said, though the members it gave read as a whole archive: a
// truncated xz read as complete and was cached (Codex's sixth review,
// #73, its probe).
func TestADecompressorsFailureFailsTheReading(t *testing.T) {
	// The tool runs under process PATH, so this probe cannot run parallel.
	bin := t.TempDir()
	tarfile := testsupport.Tarball(t, "demo-1", map[string]string{"Cargo.toml": "[package]\nname = \"demo\"\n"})
	// Tarball is gzip; the fake xz emits its uncompressed valid tar, then
	// reports the failure a truncated/corrupt compressed stream would.
	testsupport.WriteExecutable(t, filepath.Join(bin, "xz"), "#!/bin/sh\n/usr/bin/gzip -dc '"+tarfile+"'\necho 'compressed data is corrupt' >&2\nexit 1\n")
	t.Setenv("PATH", bin)
	input := filepath.Join(t.TempDir(), "demo.tar.xz")
	require.NoError(t, os.WriteFile(input, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}, 0600))
	var names []string
	err := Walk(t.Context(), input, func(m Member) error { names = append(names, m.Name); return nil })
	require.ErrorContains(t, err, "compressed data is corrupt")
}
