-- Schema 49: an adopted pull request keeps its author's login
-- (model.PullRequest.Author), whose Portfiles the trust rule names as
-- theirs (Then's item 12). Empty for one adopted before.

ALTER TABLE branches ADD COLUMN pr_author TEXT NOT NULL DEFAULT '';
