package dependency

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

const maxManifestBytes = 16 << 20

var ErrManifestMissing = errors.New("dependency: manifest missing")

// Manifest reads a regular archive member without extracting files onto the host.
func Manifest(ctx context.Context, filename, worksrcdir, name string) ([]byte, string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	header, _ := reader.Peek(4)
	found := map[string][]byte{}
	read := func(member string, regular bool, size int64, body io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if path.Base(member) != name {
			return nil
		}
		clean := strings.TrimPrefix(member, "./")
		if !regular || path.IsAbs(clean) || clean != path.Clean(clean) || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("dependency: invalid archive member %s", member)
		}
		if size < 0 || size > maxManifestBytes {
			return fmt.Errorf("dependency: oversized %s", name)
		}
		if _, exists := found[clean]; exists {
			return fmt.Errorf("dependency: duplicate archive member %s", member)
		}
		data, err := io.ReadAll(io.LimitReader(body, maxManifestBytes+1))
		if err != nil {
			return err
		}
		if len(data) > maxManifestBytes {
			return fmt.Errorf("dependency: oversized %s", name)
		}
		found[clean] = data
		return nil
	}
	if len(header) >= 2 && string(header[:2]) == "PK" {
		stat, err := file.Stat()
		if err != nil {
			return nil, "", err
		}
		archive, err := zip.NewReader(file, stat.Size())
		if err != nil {
			return nil, "", err
		}
		for _, member := range archive.File {
			if path.Base(member.Name) != name {
				continue
			}
			body, err := member.Open()
			if err != nil {
				return nil, "", err
			}
			err = read(member.Name, member.Mode().IsRegular(), int64(member.UncompressedSize64), body)
			closeErr := body.Close()
			if err != nil {
				return nil, "", err
			}
			if closeErr != nil {
				return nil, "", closeErr
			}
		}
	} else {
		var input io.Reader = reader
		if len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b {
			gz, err := gzip.NewReader(reader)
			if err != nil {
				return nil, "", err
			}
			defer gz.Close()
			input = gz
		} else if len(header) >= 3 && string(header[:3]) == "BZh" {
			input = bzip2.NewReader(reader)
		}
		limited := &io.LimitedReader{R: input, N: 1 << 30}
		archive := tar.NewReader(limited)
		for {
			if err := ctx.Err(); err != nil {
				return nil, "", err
			}
			member, err := archive.Next()
			if err == io.EOF {
				if limited.N == 0 {
					return nil, "", fmt.Errorf("dependency: archive exceeds scan limit")
				}
				break
			}
			if err != nil {
				return nil, "", fmt.Errorf("dependency: reading source archive: %w", err)
			}
			if err = read(member.Name, member.Typeflag == tar.TypeReg, member.Size, archive); err != nil {
				return nil, "", err
			}
		}
	}
	wanted := path.Join(worksrcdir, name)
	if data, ok := found[wanted]; ok {
		return data, wanted, nil
	}
	_, subdir, _ := strings.Cut(strings.Trim(worksrcdir, "/"), "/")
	relative := path.Join(subdir, name)
	var selected string
	for member := range found {
		_, suffix, nested := strings.Cut(member, "/")
		if (nested && suffix != relative) || (!nested && member != relative) {
			continue
		}
		if selected != "" {
			return nil, "", fmt.Errorf("dependency: multiple %s files; select an unambiguous worksrcdir", name)
		}
		selected = member
	}
	if selected == "" {
		return nil, "", fmt.Errorf("%w: source archive has no %s for %s", ErrManifestMissing, name, worksrcdir)
	}
	return found[selected], selected, nil
}
