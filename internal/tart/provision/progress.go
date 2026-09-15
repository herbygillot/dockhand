package provision

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

type progressWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

func (n *native) during(ctx context.Context, label string, fn func() error) error {
	return reportProgress(ctx, n.progress, label, 30*time.Second, fn)
}

func reportProgress(ctx context.Context, out io.Writer, label string, interval time.Duration, fn func() error) error {
	if out == nil {
		return fn()
	}
	stop, done := make(chan struct{}), make(chan struct{})
	started := time.Now()
	go func() {
		defer close(done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				_, _ = fmt.Fprintf(out, "%s: still working (%s elapsed)...\n", label, time.Since(started).Round(time.Second))
			}
		}
	}()
	defer func() { close(stop); <-done }()
	return fn()
}
