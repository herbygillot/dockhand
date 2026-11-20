-- Schema 64 keeps the time of a pull request's recorded push. Earlier
-- pushes have no known time, so their column remains null (#248).
ALTER TABLE branches ADD COLUMN pr_pushed_at INTEGER;
