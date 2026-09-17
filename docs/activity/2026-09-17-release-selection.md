# Stage 4 of the resolution consolidation: identity and selection

`record.Release` carried the frozen fact of a release, how it was chosen, and how it classifies in one flat struct. The selection fields (requested spelling, current version, no-update, stability, leaves-stable) now live in `record.Selection`, embedded in `Release`. Field promotion keeps every reader unchanged, composite literals name the selection, and a test proves the stored JSON stays flat, so existing state reads back as before. No behavior changed.
