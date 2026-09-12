package macports

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

var ErrNotImplemented = errors.New("macports: evaluation is not implemented")

type Dependency struct {
	Port         string
	Phase        string
	Alternatives []string
}

type PortInfo struct {
	Name         string
	Version      string
	Revision     int
	Epoch        int
	Options      map[string]string
	Dependencies []Dependency
}

type Snapshot struct {
	Source     record.Source
	Target     record.Target
	Platform   record.Platform
	Ports      map[string]PortInfo
	ObservedAt time.Time
}

type Reader interface {
	Evaluate(context.Context, Context) (Snapshot, error)
	Resolve(context.Context, record.Source, string) ([]record.Target, error)
}

type Evaluator struct {
	Executable string
	Prefix     string
}

func (e *Evaluator) Evaluate(ctx context.Context, source Context) (Snapshot, error) {
	return Snapshot{}, ErrNotImplemented
}

func (e *Evaluator) Resolve(ctx context.Context, source record.Source, selector string) ([]record.Target, error) {
	return nil, ErrNotImplemented
}

var _ Reader = (*Evaluator)(nil)
