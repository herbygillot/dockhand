package state

import (
	"context"
	"time"
)

// Maintenance operates on the whole database, independently of repository or provider setup.
type Maintenance interface {
	Backup(context.Context, string) (Backup, error)
	Check(context.Context) error
}

// Backup identifies a complete standalone snapshot. It excludes external files and services.
type Backup struct {
	Path        string
	Bytes       int64
	CompletedAt time.Time
}
