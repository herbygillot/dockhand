-- Q10: Portfiles naming paths with ${name} or ${subport}, which a rename
-- moves. Not a question of the record; run in the tree at the commit:
--
--   git -C <tree> grep -nE '\$\{(name|subport)\}' <commit> -- '*/Portfile'
SELECT "commit" FROM run LIMIT 1;
