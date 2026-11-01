-- Schema 54: a kept dependency archive says the PortGroup files the guest
-- built it with, where it built it from source (model.DependencyArchive's
-- PortGroups), as a JSON object of each file's blob ID by its path in the
-- ports tree, so a check offers it only while its revision holds the same
-- files: after a PortGroup it loaded changes, the archive is MacPorts'
-- build of what the ports tree no longer says. Null for an archive
-- installed from MacPorts' own, which names it by digest alone, and for
-- those kept before.

ALTER TABLE dependency_archives ADD COLUMN port_groups TEXT;
