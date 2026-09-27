package macports

import "context"

type VersionCandidate struct{ Version, MatchText, CaptureVersion string }
type VersionSelection struct {
	Indices    []int
	Comparison int
}

// VersionSession compares and extracts versions as MacPorts does, in one
// interpreter kept for the calls: a port's discovery makes several.
type VersionSession interface {
	SelectVersion(ctx context.Context, current, expression string, candidates []VersionCandidate) (VersionSelection, error)
	ExtractVersions(ctx context.Context, expression, page string, multiline bool) ([]string, error)
	Close() error
}

// VersionSessions opens a VersionSession; an evaluator that can keep an
// interpreter for a port's calls offers it.
type VersionSessions interface {
	VersionSession(ctx context.Context) (VersionSession, error)
}
