package macports

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/model"
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
	Source     model.Source
	Target     model.Target
	Platform   model.Platform
	Ports      map[string]PortInfo
	ObservedAt time.Time
}

type Reader interface {
	Evaluate(context.Context, Context) (Snapshot, error)
	Resolve(context.Context, model.Source, string) ([]model.Target, error)
}

type Evaluator struct {
	Executable string
	Prefix     string
}

func (e *Evaluator) Evaluate(ctx context.Context, source Context) (Snapshot, error) {
	return Snapshot{}, ErrNotImplemented
}

func (e *Evaluator) Resolve(ctx context.Context, source model.Source, selector string) ([]model.Target, error) {
	return nil, ErrNotImplemented
}

var _ Reader = (*Evaluator)(nil)
