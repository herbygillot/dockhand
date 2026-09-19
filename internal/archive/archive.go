package archive

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
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

// Walk calls fn for every member in order. Gzip and bzip2 tar streams and
// zip files are recognized by their leading bytes; anything else is read as
// a plain tar stream.
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
