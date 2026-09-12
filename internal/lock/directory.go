package lock

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Directory struct{ path string }

func NewDirectory(path string) (*Directory, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("lock: an absolute directory path is required")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, fmt.Errorf("lock: creating directory: %w", err)
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("lock: resolving directory: %w", err)
	}
	return &Directory{path: path}, nil
}

func (d *Directory) File(scope, identity, name string) (*File, error) {
	if d == nil || d.path == "" || !validName(scope) || identity == "" || !validName(name) {
		return nil, errors.New("lock: a directory, scope, resource identity, and operation name are required")
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	path := filepath.Join(d.path, scope, key, name+".lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("lock: creating resource directory: %w", err)
	}
	for _, filePath := range []string{path, path + ".gate"} {
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("lock: initializing %s: %w", filePath, err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("lock: closing %s: %w", filePath, err)
		}
	}
	return &File{path: path}, nil
}

func validName(name string) bool {
	return name != "" && strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") == ""
}
