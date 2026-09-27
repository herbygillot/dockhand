// Package staging prepares indexed source archives for the providers of
// build environments that carry a check's files into a guest, as the Tart
// provider does.
//
// Archive materializes an immutable Git tree, stages its PortIndex, checks target
// coverage, and atomically installs an archive with caller-supplied payload files.
// It owns temporary source lifetime and packaging. Providers own capacity,
// environment creation, guest execution, and the payload protocol.
package staging
