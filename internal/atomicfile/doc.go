// Package atomicfile durably replaces local files.
//
// Write uses a temporary sibling, syncs its contents, renames it into place, and
// syncs the parent directory; Place does the same for a file already written,
// as a large download is. Callers supply the destination and permissions and
// coordinate concurrent writers when replacement requires stronger preconditions.
package atomicfile
