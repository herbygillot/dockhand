// Package upstream selects and checks releases for interpreted MacPorts sources.
//
// Forge catalogs supply remote tags and releases; MacPorts source conventions and
// version selection determine which candidates apply. A bound Discovery uses a
// source-specific probe to assess calculated versions and resolve explicit or
// automatic updates. Results retain unknown and incomplete observations, and
// source checks detect a selected tag moving to a different commit. Callers own
// workspaces, preparation, and workflow acceptance.
package upstream
