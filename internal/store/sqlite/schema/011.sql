-- Schema 11: an execution's environment names its developer tools, when
-- its provider states them: Tart's release image, with Xcode or the Command
-- Line Tools alone. Empty for executions made before it, and for providers
-- whose builders have their own. A target an environment can't build for
-- want of Xcode is the plan's to say (Plan.Unmet), so schema 10's result
-- detail, which said it, goes.
ALTER TABLE executions ADD COLUMN developer_tools TEXT NOT NULL DEFAULT '';
ALTER TABLE results DROP COLUMN detail;
