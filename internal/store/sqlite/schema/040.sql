-- Schema 40: a decision's words, whoever wrote them (Then's item 3):
-- dockhand's refusal a hand edit answers, the update it couldn't make and
-- why, or, as the person's voice plans, the person's reason. Empty for a
-- decision that gave none, as every one before did.

ALTER TABLE decisions ADD COLUMN words TEXT NOT NULL DEFAULT '';
