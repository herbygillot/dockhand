# 2026-09-30: the assessment design, with Codex's critique

Codex critiqued the [assessment design](../assessment-design.md) at `278547e9`, appending it as the design's section 8 ([its note](2026-09-30-assessment-design-critique.md)). Its claims were checked against the code and documents: `sourcecompare` imports `macports` and `dependency`, `raiseGoToolchain` judges and edits in one function, the principles keep `status` observational, and the manifest readers have kept markers, repeated declarations, and Git sources since batch 17, which the design's §2 understated.

All eight points and both wording corrections were taken into the design's body, and the critique moved unchanged, but for two links made relative, to [reviews](../reviews/2026-09-30-assessment-design-critique.md). The design's new §8 maps each point to where it went. In short:
- a reading is kept by the source as observed, its root, and the reader's version, and `project` imports nothing of MacPorts;
- the assessment reads both sides in full with the port's facts at both, and classes concerns as introduced, resolved, already present, or of unknown baseline; only the first and last hold;
- the subject is a target in a context, the base is the revision's captured one, and new, removed, and sourceless ports are defined;
- a port's source is declared, observed, and built, kept apart, and a stealth update is judged from content;
- collection is by commands acting on a request, `status` only reports, and a check whose assessment can't finish still completes;
- the gate gathers concerns, each with an identity apart from its words, and each submission mode keeps its own rules, so nothing a person submits is stricter and D13 stands;
- relevance is kept apart from its treatment, so batch 9's scoping is a named policy;
- the Go toolchain judgment goes to `assess`, its edit stays in `portedit`.

Design v3's §3 and §11, architecture, the roadmap's item 9 and D14 follow. No code changed.
