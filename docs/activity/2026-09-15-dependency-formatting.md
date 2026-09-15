# Preserve unchanged dependency formatting

Dependency plans retain the original source spelling and restore blocks whose regenerated values are semantically unchanged. Preparation uses this operation after validating helper output, so an update with unchanged Cargo or Go dependencies does not rewrite those blocks.

Added a regression covering an unchanged multiline Cargo block alongside a version edit. The dependency suite and the full project suite passed during the rust-analyzer exercise and editor refactor. Both real rust-analyzer previews retained the Cargo block unchanged.
