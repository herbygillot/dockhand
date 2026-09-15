package dependency

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Input struct{ Archive, Worksrcdir, Package, Tag string }
type GitCrate struct{ Name, Repository, Branch, Commit string }

func (c GitCrate) Distfile() string { return c.Name + "-" + c.Commit + ".tar.gz" }

type GeneratedBlocks struct {
	Values map[string][]string
	Git    []GitCrate
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxManifestBytes {
		return 0, fmt.Errorf("dependency: helper output exceeds limit")
	}
	return b.Buffer.Write(p)
}
func run(ctx context.Context, executable, directory string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = directory
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("dependency: %s failed: %w: %s", filepath.Base(executable), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
func Generate(ctx context.Context, kind, executable string, in Input) (GeneratedBlocks, error) {
	if kind == Go {
		return generateGo(ctx, executable, in)
	}
	return generateCargo(ctx, executable, in)
}

func sha256Value(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == 32
}
