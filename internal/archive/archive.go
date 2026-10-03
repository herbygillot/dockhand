package archive

import (
	"archive/tar"
	"archive/zip"
	"bufio"
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
			return 0, fmt.Errorf("%w: it holds more than the %s dockhand reads of one, uncompressed", errScanLimit, prose.Bytes(scanLimit))
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
type Member struct {
	Name    string
	Regular bool
	Size    int64
	Body    io.Reader
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

// Walk calls fn for every member in order. Gzip, bzip2, xz, zstd, and lzip
// tar streams and zip files are recognized by their leading bytes; anything
// else is read as a plain tar stream.
func Walk(ctx context.Context, filename string, fn func(Member) error) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	header, _ := reader.Peek(4)
	if kind := binaryKind(header); kind != "" {
		return fmt.Errorf("%w: %s is %s", ErrBinary, path.Base(filename), kind)
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
			err = fn(Member{Name: member.Name, Regular: member.Mode().IsRegular(), Size: int64(member.UncompressedSize64), Body: budget})
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
	if len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return err
		}
		defer gz.Close()
		input = gz
	} else if len(header) >= 3 && string(header[:3]) == "BZh" {
		input = bzip2.NewReader(reader)
	} else if tool := decompressor(reader); tool != "" {
		// xz, zstd, and lzip have no reader in Go's library; the system's
		// own tool decompresses, and the members are read here as always.
		command := exec.CommandContext(ctx, tool, "-dc")
		command.Stdin = reader
		var stderr strings.Builder
		command.Stderr = &stderr
		out, err := command.StdoutPipe()
		if err != nil {
			return err
		}
		if err := command.Start(); err != nil {
			return fmt.Errorf("archive: reading %s needs %s: %w", path.Base(filename), tool, err)
		}
		defer func() {
			_ = out.Close()
			_ = command.Wait()
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
			return nil
		case errors.Is(err, errScanLimit):
			// Said of the archive by whoever named it: filename may be
			// a temporary one.
			return err
		case err != nil:
			return fmt.Errorf("archive: reading %s: %w", path.Base(filename), err)
		}
		if err := fn(Member{Name: member.Name, Regular: member.Typeflag == tar.TypeReg, Size: member.Size, Body: tr}); err != nil {
			return err
		}
	}
}

// decompressor is the system tool for a stream Go's library can't read.
func decompressor(reader *bufio.Reader) string {
	header, _ := reader.Peek(6)
	switch {
	case bytes.HasPrefix(header, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}):
		return "xz"
	case bytes.HasPrefix(header, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		return "zstd"
	case bytes.HasPrefix(header, []byte("LZIP")):
		return "lzip"
	}
	return ""
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
