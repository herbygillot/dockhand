// Package atomicfile durably replaces small local files.
//
// Write uses a temporary sibling, syncs its contents, renames it into place, and
// syncs the parent directory. Callers supply the destination and permissions and
// coordinate concurrent writers when replacement requires stronger preconditions.
package atomicfile
