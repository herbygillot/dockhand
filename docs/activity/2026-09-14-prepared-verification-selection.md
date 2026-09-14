# Prepared verification selection

Prepared bump jobs can now reuse verification without requiring `--image` at acceptance. An image-free request records build requirements for Tart, the native platform, the test policy, and the source-build policy. These requirements are immutable job intent and authorize selection of recorded evidence only; they cannot start a new build.

After branch integration makes the candidate tree known, the driver examines repository-scoped terminal attempts for that exact tree and target. It selects only a configuration satisfying the accepted requirements and applies the full existing reuse comparison, including variants, environment and verifier digests, provider settings, and artifact inputs. The newest matching negative result blocks an older pass. A miss preserves the prepared branch and reports that an image is required. An explicitly selected image whose setup fails is not replaced with historical configuration.

The reused attempt remains the durable source of the exact selected configuration. The job records its partial requirements, selected attempt, explanation, and verification plan atomically. Publication revalidates both the accepted requirements and the full evidence applicability before any remote effect. Job requirements use the existing SQLite options JSON, so no schema migration or new dependency was needed.

Implementation authored for Dockhand v2 in this change includes the `record.BuildRequirements` concept, pure validation/comparison in `verify`, deferred selection in `workflow`, state persistence, publication boundary checks, CLI binding behavior, and focused workflow, CLI, and policy tests. The Tart provider name constant replaces new cross-package string duplication introduced by this path.
