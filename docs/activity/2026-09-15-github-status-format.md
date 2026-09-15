# GitHub run IDs in status output

The live xplr exercise displayed Go formatting diagnostics instead of numeric run IDs and attempts. The status renderer escapes each argument into a string before formatting, so the GitHub line now uses string verbs consistently with the rest of the renderer.

Added a focused rendering regression with a real-size run ID and rerun attempt. Status tests passed and the binary was rebuilt. This changes presentation only; persisted identities and PR bodies were already correct.
