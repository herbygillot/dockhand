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
)

// errScanLimit means the archive's uncompressed stream exceeded the walk limit.
var errScanLimit = errors.New("archive: exceeds scan limit")

// scanLimit bounds the uncompressed bytes one walk reads.
const scanLimit = 1 << 30

// Member is one archive entry. Body is valid only during the callback.
type Member struct {
	Name    string
	Regular bool
	Size    int64
	Body    io.Reader
}

// Clean returns the member path without a leading "./", or false when the
// path is absolute, unnormalized, or escapes the archive root.
func (m Member) Clean() (string, bool) {
	clean := strings.TrimPrefix(m.Name, "./")
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
	if len(header) >= 2 && string(header[:2]) == "PK" {
		stat, err := file.Stat()
		if err != nil {
			return err
		}
		zr, err := zip.NewReader(file, stat.Size())
		if err != nil {
			return err
		}
		for _, member := range zr.File {
			if err := ctx.Err(); err != nil {
				return err
			}
			body, err := member.Open()
			if err != nil {
				return err
			}
			err = fn(Member{Name: member.Name, Regular: member.Mode().IsRegular(), Size: int64(member.UncompressedSize64), Body: body})
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
	limited := &io.LimitedReader{R: input, N: scanLimit}
	tr := tar.NewReader(limited)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		member, err := tr.Next()
		if err == io.EOF {
			if limited.N == 0 {
				return errScanLimit
			}
			return nil
		}
		if err != nil {
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
// top directory every member shares, if they share one, which names the
// version. Links and other special members are left out, and a member
// whose path would leave the directory is refused. It returns how many
// files it wrote.
func Extract(ctx context.Context, filename, directory string) (int, error) {
	top := ""
	shared := true
	if err := Walk(ctx, filename, func(member Member) error {
		name, ok := member.Clean()
		if !ok {
			return nil
		}
		first, _, _ := strings.Cut(name, "/")
		switch {
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
