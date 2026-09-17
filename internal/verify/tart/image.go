package tart

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/verify"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/state"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
)

type imageCache struct {
	mu            sync.Mutex
	stamp, digest string
}

func validImageDigest(value string) bool {
	value, ok := strings.CutPrefix(value, "sha256:")
	if !ok || len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (n *native) Environment(ctx context.Context) (Environment, error) {
	if !safeToken(n.config.Image) || strings.Contains(n.config.Image, ":") {
		return Environment{}, fmt.Errorf("tart: a prepared local image is required")
	}
	guard, err := tartvm.AcquireImageRead(ctx, n.config.Home, n.config.Image)
	if err != nil {
		return Environment{}, err
	}
	defer guard.Close()
	exists, running, err := n.LocalVM(ctx, n.config.Image)
	if err != nil {
		var path *os.PathError
		if errors.Is(err, exec.ErrNotFound) || errors.As(err, &path) && path.Op == "fork/exec" && errors.Is(path.Err, os.ErrNotExist) {
			return Environment{}, errors.Join(ErrExecutableUnavailable, err)
		}
		return Environment{}, err
	}
	if !exists {
		return Environment{}, fmt.Errorf("%w: %s; run dockhand setup", ErrImageUnavailable, n.config.Image)
	}
	if running {
		return Environment{}, fmt.Errorf("tart: prepared image %s must be stopped", n.config.Image)
	}
	names := []string{"config.json", "disk.img", "nvram.bin"}
	stamp := func() (string, error) {
		var text strings.Builder
		text.WriteString("tart-image-v2|" + filepath.Join(n.config.Home, "vms", n.config.Image))
		for _, name := range names {
			info, err := os.Lstat(filepath.Join(n.config.Home, "vms", n.config.Image, name))
			if err != nil {
				return "", err
			}
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("tart: image file is not regular: %s", name)
			}
			fmt.Fprintf(&text, "|%s:%d:%d:%d", name, info.Size(), info.ModTime().UnixNano(), info.Mode())
			stat := reflect.Indirect(reflect.ValueOf(info.Sys()))
			fields := []string{"Dev", "Ino", "Ctimespec", "Ctim"}
			if name == "config.json" {
				data, err := os.ReadFile(filepath.Join(n.config.Home, "vms", n.config.Image, name))
				if err != nil {
					return "", err
				}
				fmt.Fprintf(&text, ":%x", sha256.Sum256(data))
				fields = fields[:2]
			}
			for _, field := range fields {
				if value := stat.FieldByName(field); value.IsValid() {
					fmt.Fprintf(&text, ":%v", value.Interface())
				}
			}
		}
		return text.String(), nil
	}
	n.images.mu.Lock()
	defer n.images.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Environment{}, err
	}
	before, err := stamp()
	if err != nil {
		return Environment{}, err
	}
	cached := state.ImageDigest{Provider: verify.ProviderTart, Path: filepath.Join(n.config.Home, "vms", n.config.Image)}
	if n.images.stamp == before {
		cached.Stamp, cached.Digest = before, n.images.digest
	} else if n.cache != nil {
		value, err := n.cache.ImageDigest(ctx, cached.Provider, cached.Path)
		if err != nil && !errors.Is(err, state.ErrNotFound) {
			return Environment{}, err
		}
		if err == nil {
			cached = value
		}
	}
	if cached.Stamp == before && validImageDigest(cached.Digest) {
		after, err := stamp()
		if err != nil {
			return Environment{}, err
		}
		if after != before {
			return Environment{}, fmt.Errorf("tart: image changed while checking cached digest")
		}
		n.images.stamp, n.images.digest = before, cached.Digest
		return Environment{Digest: cached.Digest, Platform: n.config.Platform}, nil
	}
	progress.Report(ctx, "Hashing Tart image %s; reading the disk image may take several minutes", n.config.Image)
	started := time.Now()
	hash := sha256.New()
	buffer := make([]byte, 1<<20)
	for _, name := range names {
		file, err := os.Open(filepath.Join(n.config.Home, "vms", n.config.Image, name))
		if err != nil {
			return Environment{}, err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return Environment{}, err
		}
		fmt.Fprintf(hash, "%s:%d\n", name, info.Size())
		for {
			if err = ctx.Err(); err != nil {
				file.Close()
				return Environment{}, err
			}
			count, readErr := file.Read(buffer)
			if count > 0 {
				_, _ = hash.Write(buffer[:count])
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				file.Close()
				return Environment{}, readErr
			}
		}
		if err = file.Close(); err != nil {
			return Environment{}, err
		}
	}
	after, err := stamp()
	if err != nil {
		return Environment{}, err
	}
	if after != before {
		return Environment{}, fmt.Errorf("tart: image changed while hashing")
	}
	cached.Stamp, cached.Digest = before, "sha256:"+hex.EncodeToString(hash.Sum(nil))
	if n.cache != nil {
		if err := n.cache.PutImageDigest(ctx, cached); err != nil {
			return Environment{}, err
		}
	}
	n.images.stamp, n.images.digest = before, cached.Digest
	progress.Report(ctx, "Tart image fingerprint ready (%s)", time.Since(started).Round(time.Second))
	return Environment{Digest: n.images.digest, Platform: n.config.Platform}, nil
}
