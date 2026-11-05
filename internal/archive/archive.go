package archive

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/herbygillot/dockhand/internal/prose"
)

// errScanLimit means the archive's uncompressed stream exceeded the walk limit.
var errScanLimit = errors.New("archive: exceeds scan limit")

// ErrBinary is a file that holds a built program or an installer package,
// not source: a macOS installer package (xar), as 1password-cli fetches,
// or a Mach-O executable. Nothing in it is upstream source to compare
// (field testing's batch 10, finding 2).
var ErrBinary = errors.New("a binary package, with no source to compare")

// BinaryError is a file that's a binary, ErrBinary's: the file, and what
// its first bytes say it is, "a macOS installer package". A caller with a
// better name for the file, the distfile's own where the file is a
// download's temporary one, sets File.
type BinaryError struct {
	File, Kind string
}

func (e *BinaryError) Error() string {
	return fmt.Sprintf("%v: %s is %s", ErrBinary, e.File, e.Kind)
}

func (e *BinaryError) Is(target error) bool { return target == ErrBinary }

// binaryKind names the binary a file's first bytes say it is, or nothing.
func binaryKind(header []byte) string {
	if len(header) < 4 {
		return ""
	}
	switch string(header[:4]) {
	case "xar!":
		return "a macOS installer package"
	case "\xcf\xfa\xed\xfe", "\xce\xfa\xed\xfe", "\xca\xfe\xba\xbe", "\xfe\xed\xfa\xcf":
		return "a Mach-O executable"
	}
	return ""
}

// scanLimit bounds the uncompressed bytes one walk reads; a variable for
// tests. 4 GiB, at the person's word (2026-10-01): rustc's source holds
// 3.5 GiB, past the 1 GiB it had. A tar stream has no index, so a walk
// reads every member to reach the next; a zip archive's members are read
// only as asked, so what's read of them counts.
var scanLimit int64 = 4 << 30

// scanLimited is a stream that stops at the scan limit, and says so where
// more follows, rather than ending as if the archive had: tar read a
// member cut short as "unexpected EOF", and rustc's source, 3.5 GiB
// uncompressed, was said as broken (the rust and cargo run).
type scanLimited struct {
	r io.Reader
	n int64
}

func (s *scanLimited) Read(p []byte) (int, error) {
	if s.n <= 0 {
		var one [1]byte
		k, err := io.ReadFull(s.r, one[:])
		switch {
		case k > 0:
			return 0, fmt.Errorf("%w: the uncompressed archive exceeds the %s read limit", errScanLimit, prose.Bytes(scanLimit))
		case err != nil && !errors.Is(err, io.EOF):
			return 0, err
		}
		return 0, io.EOF
	}
	if int64(len(p)) > s.n {
		p = p[:s.n]
	}
	k, err := s.r.Read(p)
	s.n -= int64(k)
	return k, err
}

// Member is one archive entry. Body is valid only during the callback.
// Link is a symbolic link's target, empty for any other member.
type Member struct {
	Name    string
	Regular bool
	Link    string
	Size    int64
	Body    io.Reader
}

// linkLimit bounds a zip symbolic link's target, which zip stores as the
// member's body; PATH_MAX, as tar's own fields allow no more in practice.
const linkLimit = 4096

// Digest is a regular member's contents' SHA-256, in hex, as Tree keeps
// it: what the member holds, apart from its name, time, and owner. It
// reads Body.
func (m Member) Digest() (string, error) {
	digest := sha256.New()
	if _, err := io.Copy(digest, m.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// Clean returns the member path without a leading "./", or a directory's
// trailing "/", as tar names one; false when the path is absolute,
// unnormalized, escapes the archive root, or is the root itself.
func (m Member) Clean() (string, bool) {
	clean := strings.TrimSuffix(strings.TrimPrefix(m.Name, "./"), "/")
	if clean == "" || clean == "." || path.IsAbs(clean) || clean != path.Clean(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// Walk calls fn for every member in order. Gzip, bzip2, xz, lzma, zstd,
// and lzip tar streams, zip and 7z files are recognized by their leading
// bytes, and a disk image by its .dmg name; anything else is read as a
// plain tar stream: every archive Base extracts (macports.Extractions).
func Walk(ctx context.Context, filename string, fn func(Member) error) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	header, _ := reader.Peek(6)
	if kind := binaryKind(header); kind != "" {
		return &BinaryError{File: path.Base(filename), Kind: kind}
	}
	// A 7z archive and a disk image are no stream: each is unpacked, or
	// mounted, where it can be walked, by the tool Base extracts it with.
	if bytes.HasPrefix(header, sevenZip) {
		return walkSevenZip(ctx, filename, fn)
	}
	if strings.HasSuffix(filename, ".dmg") {
		return walkDiskImage(ctx, filename, fn)
	}
	if len(header) >= 2 && string(header[:2]) == "PK" {
		stat, err := file.Stat()
		if err != nil {
			return err
		}
		zr, err := zip.NewReader(file, stat.Size())
		if err != nil {
			return err
		}
		// What's read of the members counts against the scan limit, as a
		// tar stream's does; a zip archive had none (the limits sweep).
		budget := &scanLimited{n: scanLimit}
		for _, member := range zr.File {
			if err := ctx.Err(); err != nil {
				return err
			}
			body, err := member.Open()
			if err != nil {
				return err
			}
			budget.r = body
			entry := Member{Name: member.Name, Regular: member.Mode().IsRegular(), Size: int64(member.UncompressedSize64), Body: budget}
			if member.Mode()&fs.ModeSymlink != 0 {
				target, readErr := io.ReadAll(io.LimitReader(budget, linkLimit))
				if readErr != nil {
					_ = body.Close()
					return readErr
				}
				entry.Link, entry.Body = string(target), strings.NewReader("")
			}
			err = fn(entry)
			closeErr := body.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	}
	var input io.Reader = reader
	// finished is the decompressing tool's end, where one runs: its exit
	// says whether the stream was whole, which a tar reader's EOF doesn't,
	// a truncated xz read as complete and cached (Codex's sixth review,
	// #73).
	finished := func() error { return nil }
	if len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return err
		}
		defer gz.Close()
		input = gz
	} else if len(header) >= 3 && string(header[:3]) == "BZh" {
		input = bzip2.NewReader(reader)
	} else if tool := decompressor(reader); tool != nil {
		// xz, lzma, zstd, and lzip have no reader in Go's library; the
		// system's own tool decompresses, and the members are read here as
		// always.
		command := toolCommand(ctx, tool[0], tool[1:]...)
		command.Stdin = reader
		var stderr strings.Builder
		command.Stderr = &stderr
		out, err := command.StdoutPipe()
		if err != nil {
			return err
		}
		if err := command.Start(); err != nil {
			return fmt.Errorf("archive: reading %s needs %s: %w", path.Base(filename), tool[0], err)
		}
		waited := false
		finished = func() error {
			waited = true
			if err := command.Wait(); err != nil {
				return fmt.Errorf("archive: reading %s: %s: %w: %s", path.Base(filename), tool[0], err, strings.TrimSpace(stderr.String()))
			}
			return nil
		}
		defer func() {
			_ = out.Close()
			if !waited {
				_ = command.Wait()
			}
		}()
		input = out
	}
	tr := tar.NewReader(&scanLimited{r: input, n: scanLimit})
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		member, err := tr.Next()
		switch {
		case err == io.EOF:
			// The rest of the stream drained, so the tool can end.
			_, _ = io.Copy(io.Discard, input)
			return finished()
		case errors.Is(err, errScanLimit):
			// Said of the archive by whoever named it: filename may be
			// a temporary one.
			return err
		case err != nil:
			return fmt.Errorf("archive: reading %s: %w", path.Base(filename), err)
		}
		entry := Member{Name: member.Name, Regular: member.Typeflag == tar.TypeReg, Size: member.Size, Body: tr}
		if member.Typeflag == tar.TypeSymlink {
			entry.Link = member.Linkname
		}
		if err := fn(entry); err != nil {
			return err
		}
	}
}

// decompressor is the system tool for a stream Go's library can't read.
func decompressor(reader *bufio.Reader) []string {
	header, _ := reader.Peek(6)
	switch {
	case bytes.HasPrefix(header, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}):
		return []string{"xz", "-dc"}
	case bytes.HasPrefix(header, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		return []string{"zstd", "-dc"}
	case bytes.HasPrefix(header, []byte("LZIP")):
		return []string{"lzip", "-dc"}
	case bytes.HasPrefix(header, []byte{0x5d, 0x00, 0x00}):
		// lzma's own format, as Base's use_lzma extracts it, which xz
		// reads.
		return []string{"xz", "--format=lzma", "-dc"}
	}
	return nil
}

// toolCommand is a system tool an archive is read through, as Base
// extracts it, run with the process's environment.
func toolCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// sevenZip is a 7z archive's signature.
var sevenZip = []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}

// walkSevenZip walks a 7z archive unpacked into a scratch directory by
// 7za, as Base's use_7z extracts it.
func walkSevenZip(ctx context.Context, filename string, fn func(Member) error) error {
	scratch, err := os.MkdirTemp("", "dockhand-7z-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	command := toolCommand(ctx, "7za", "x", "-y", "-bd", "-o"+scratch, filename)
	if output, err := command.CombinedOutput(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("archive: reading %s needs 7za: %w", path.Base(filename), err)
		}
		return fmt.Errorf("archive: reading %s: 7za: %w: %s", path.Base(filename), err, strings.TrimSpace(string(output)))
	}
	return walkDirectory(ctx, scratch, fn)
}

// walkDiskImage walks a disk image mounted read-only at a scratch mount
// point by hdiutil, and detached after, as Base's use_dmg copies it; only
// macOS's hdiutil reads one.
func walkDiskImage(ctx context.Context, filename string, fn func(Member) error) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("archive: %s is a disk image, which only macOS's hdiutil reads", path.Base(filename))
	}
	mount, err := os.MkdirTemp("", "dockhand-dmg-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mount)
	if output, err := toolCommand(ctx, "/usr/bin/hdiutil", "attach", "-readonly", "-nobrowse", "-noverify", "-mountpoint", mount, filename).CombinedOutput(); err != nil {
		return fmt.Errorf("archive: reading %s: hdiutil attach: %w: %s", path.Base(filename), err, strings.TrimSpace(string(output)))
	}
	defer func() {
		_ = toolCommand(context.WithoutCancel(ctx), "/usr/bin/hdiutil", "detach", "-force", mount).Run()
	}()
	return walkDirectory(ctx, mount, fn)
}

// walkDirectory calls fn for each file, link, and directory below root,
// as an archive's members, named from it.
func walkDirectory(ctx context.Context, root string, fn func(Member) error) error {
	budget := &scanLimited{n: scanLimit}
	return filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || file == root {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(file)
			if err != nil {
				return err
			}
			return fn(Member{Name: name, Link: target, Body: strings.NewReader("")})
		case entry.IsDir():
			return fn(Member{Name: name + "/", Body: strings.NewReader("")})
		case !entry.Type().IsRegular():
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		body, err := os.Open(file)
		if err != nil {
			return err
		}
		defer body.Close()
		budget.r = body
		return fn(Member{Name: name, Regular: true, Size: info.Size(), Body: budget})
	})
}

// Extract writes an archive's regular files under a directory, less the one
// top directory every file shares, if they share one, which names the
// version. Links and other special members are left out, as the pax
// header GitHub's tarballs begin with is, and a member whose path would
// leave the directory is refused. It returns how many files it wrote.
func Extract(ctx context.Context, filename, directory string) (int, error) {
	top := ""
	shared := true
	if err := Walk(ctx, filename, func(member Member) error {
		name, ok := member.Clean()
		if !ok || !member.Regular {
			return nil
		}
		first, _, nested := strings.Cut(name, "/")
		switch {
		case !nested:
			shared = false
		case top == "":
			top = first
		case first != top:
			shared = false
		}
		return nil
	}); err != nil {
		return 0, err
	}
	written := 0
	err := Walk(ctx, filename, func(member Member) error {
		name, ok := member.Clean()
		if !ok {
			// A member for the archive's root, as ./ is in some, is
			// inside it.
			if root := strings.TrimSuffix(strings.TrimPrefix(member.Name, "./"), "/"); root == "" || root == "." {
				return nil
			}
			return fmt.Errorf("archive: %s has a member outside it: %s", path.Base(filename), member.Name)
		}
		if shared && top != "" {
			name = strings.TrimPrefix(strings.TrimPrefix(name, top), "/")
		}
		if !member.Regular || name == "" {
			return nil
		}
		target := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = io.Copy(file, member.Body)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		written++
		return err
	})
	return written, err
}
