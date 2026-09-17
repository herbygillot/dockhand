package dependency

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"github.com/herbygillot/dockhand/internal/subprocess"
	"path/filepath"
	"time"
)

type Input struct {
	Archive, Worksrcdir, Package, Tag string
	// Git states which Git crates a Cargo port declares; see GitPolicy.
	Git GitPolicy
}

// GeneratedBlocks holds helper-derived declarations. Git lists the crates
// cargo.crates_github declares once their archive checksums are known; Online
// lists the crates the port leaves to Cargo's network resolution at build time.
type GeneratedBlocks struct {
	Values map[string][]string
	Git    []GitCrate
	Online []GitCrate
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
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: filepath.Base(executable), Path: executable, Args: args, Dir: directory, Limit: maxManifestBytes})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("dependency: %w", err)
	}
	return result.Output, nil
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
