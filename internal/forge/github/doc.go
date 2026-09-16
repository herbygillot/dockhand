// Package github adapts GitHub repositories and pull requests to forge contracts.
//
// It uses go-github for tag and release catalogs, repository metadata, and pull
// request observation and writes. The shared internal/github client supplies
// authentication and transport policy; upstream and publish interpret the
// observations for a contribution.
package github
