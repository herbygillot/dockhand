package tart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/ledger"
)

func (p *Provider) ReadLog(ctx context.Context, run record.ProviderRun, offset int64, limit int) (verify.LogChunk, error) {
	if offset < 0 || limit < 1 || limit > 65536 {
		return verify.LogChunk{}, fmt.Errorf("tart: invalid log range")
	}
	o, v, _, err := p.openRun(ctx, run)
	if err != nil {
		return verify.LogChunk{}, err
	}
	defer o.entry.Close()
	data, next, _, err := ledger.ReadChunk(filepath.Join(o.directory(v), "build.log"), offset, limit)
	if err == nil {
		return verify.LogChunk{Data: data, Next: next, Complete: len(v.Result) > 0 && len(data) < limit}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return verify.LogChunk{}, err
	}
	if len(v.Result) > 0 {
		return verify.LogChunk{}, verify.ErrLogUnavailable
	}
	reader, ok := o.machine.(interface {
		ReadLog(context.Context, string, int64, int) ([]byte, error)
	})
	if !ok {
		return verify.LogChunk{}, fmt.Errorf("tart: native log reader unavailable")
	}
	data, err = reader.ReadLog(ctx, v.Resource, offset, limit)
	return verify.LogChunk{Data: data, Next: offset + int64(len(data))}, err
}
func (n *native) ReadLog(ctx context.Context, vm string, offset int64, limit int) ([]byte, error) {
	script := fmt.Sprintf("if [ -f /var/tmp/dockhand2/build.log ]; then /usr/bin/tail -c +%d /var/tmp/dockhand2/build.log | /usr/bin/head -c %d; fi", offset+1, limit)
	return n.guest(ctx, vm, nil, "sudo", "-n", "/bin/sh", "-c", script)
}
