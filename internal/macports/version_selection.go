package macports

type VersionCandidate struct{ Version, MatchText, CaptureVersion string }
type VersionSelection struct {
	Indices    []int
	Comparison int
}
