-- Schema 57: a passed result keeps what each port its depends_lib names
-- installs that a build could link, its dynamic libraries and pkg-config
-- modules, and how many Mach-O files the build installed
-- (model.TargetResult.Declared, as JSON, NULL where its provider didn't
-- read them, as every earlier result's): what a declaration nothing links
-- is read from, and the pkg-config path an undeclared link came by.

ALTER TABLE results ADD COLUMN declared TEXT CHECK(declared IS NULL OR json_valid(declared));
