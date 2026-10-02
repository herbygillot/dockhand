package buildlog

import (
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/atomicfile"
)

// Compressed is what a log kept compressed adds to its name: a check's
// logs are kept with gzip once it ends, where they had taken 802 MB across
// 62 checks, a build log shrinking some sixteen times (D6). gzip, since Go
// writes it, where it only reads bzip2.
const Compressed = ".gz"

// Compression is what compressing logs did: how many, and their bytes
// before and after.
type Compression struct {
	Logs          int
	Before, After int64
}

// Compress keeps a log compressed, at its name with Compressed added, and
// removes it once that is whole and synced. One already compressed, or
// gone, is left as it is.
func Compress(path string) (Compression, error) {
	if strings.HasSuffix(path, Compressed) {
		return Compression{}, nil
	}
	source, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Compression{}, nil
	}
	if err != nil {
		return Compression{}, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return Compression{}, err
	}
	if err := atomicfile.Create(path+Compressed, info.Mode().Perm(), func(file *os.File) error {
		writer, err := gzip.NewWriterLevel(file, gzip.BestCompression)
		if err != nil {
			return err
		}
		if _, err := io.Copy(writer, source); err != nil {
			return err
		}
		return writer.Close()
	}); err != nil {
		return Compression{}, err
	}
	compressed, err := os.Stat(path + Compressed)
	if err != nil {
		return Compression{}, err
	}
	return Compression{Logs: 1, Before: info.Size(), After: compressed.Size()}, os.Remove(path)
}

// CompressAll compresses every log, each file named *.log, below a
// directory.
func CompressAll(directory string) (Compression, error) {
	var total Compression
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return err
		case !entry.Type().IsRegular() || !strings.HasSuffix(path, ".log"):
			return nil
		}
		done, err := Compress(path)
		total.Logs, total.Before, total.After = total.Logs+done.Logs, total.Before+done.Before, total.After+done.After
		return err
	})
	return total, err
}

// Where is where a log is on disk, by the path it was written at: that
// path, or the compressed one where it's been kept compressed. The path
// it was written at where neither is there.
func Where(path string) string {
	if _, err := os.Stat(path); err != nil {
		if _, err := os.Stat(path + Compressed); err == nil {
			return path + Compressed
		}
	}
	return path
}

// Open opens a log by the path it was written at, read as it was written
// whether or not it's been kept compressed since.
func Open(path string) (io.ReadCloser, error) {
	where := Where(path)
	file, err := os.Open(where)
	if err != nil || !strings.HasSuffix(where, Compressed) || strings.HasSuffix(path, Compressed) {
		return file, err
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		file.Close()
		return nil, err
	}
	return compressedLog{Reader: reader, file: file}, nil
}

// ReadFile reads a whole log, as Open reads it.
func ReadFile(path string) ([]byte, error) {
	file, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

// compressedLog reads a log kept compressed, and closes its file.
type compressedLog struct {
	*gzip.Reader
	file *os.File
}

func (c compressedLog) Close() error { return errors.Join(c.Reader.Close(), c.file.Close()) }
