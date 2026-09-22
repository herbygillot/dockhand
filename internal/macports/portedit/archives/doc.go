// Package archives fetches a port's source archives as the direct downloader
// dockhand uses in place of MacPorts' fetch: it maps evaluated distfiles to
// their single direct locations, refuses what the downloader cannot fetch as
// MacPorts would, downloads with bounded size and time while hashing, and
// rewrites checksum declarations from what it fetched. It knows nothing about
// workspaces, edits, or results; portedit composes it.
package archives
