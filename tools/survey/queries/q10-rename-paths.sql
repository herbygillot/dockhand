-- Q10: Portfiles naming paths with ${name} or ${subport}, which a rename
-- moves. Not a question of the record; run in the tree at the commit:
--
--   git -C <tree> grep -nE '(/\$\{(name|subport)\}|\$\{(name|subport)\}/)' <commit> -- '*/Portfile'
--
-- (4,546 Portfiles at bb1307e0be60, most of them under ${prefix}.)
SELECT "commit" FROM run LIMIT 1;
