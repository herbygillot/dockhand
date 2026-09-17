# Retired rows hide until asked for

Observed by the user on 2026-09-17: merged contributions kept full weight in the status table long after `refresh` had retired them and cleaned their branches, so the table did not answer "what is still mine to do".

`workflow.Current` keeps the rows whose leading contribution is open. Plain `status` and `--json` print those unless `--all`, and the plain output ends with "N retired hidden (--all shows them)" so nothing vanishes silently. The live table starts the same way, counts the hidden rows in its header, and the `h` key toggles them; the cursor stays within whatever is visible. Earlier contributions of a port that is still open were already folded under its row, so hiding only affects ports with nothing open.
