# Source-bound version probing and discovery

Added `portedit.VersionProbe` over a caller-owned disposable workspace. It returns bound metadata and evaluates calculated versions without fetching archives, creating a job, or requiring a bump request. Its separate release check retains the stricter edit-fidelity policy used by preparation. The existing evaluator and carrier discovery remain in `portedit`.

Added `upstream.Service.Bind` with a small consumer-owned probe interface. Bound discovery supports assessment and explicit resolution without changing the shared service. Preparation composes this boundary and owns upstream identity checks before and after editing; the editor no longer depends on `upstream`. Removed the unused, unbound discovery service exposure from app wiring. No new command, dependency, or schema was added.

New tests exercise automatic and explicit discovery against calculated versions, source restoration, cancellation, and discovery of an update that cannot safely be auto-edited because it changes a sibling port. Existing preparation tests caught and corrected an intermediate regression: source revalidation must succeed before commit intent is returned. Validation covers editor, upstream, preparation, and app race tests, followed by the repository-wide test/vet/build checks.
