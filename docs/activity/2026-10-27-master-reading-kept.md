# Master's archive: the reading kept, not the archive

2026-10-27, item 9's archive keeping (prioritization, 7fe1ad6f).

- The archive is read as it downloads, bzip2 over the response's body and
  each library's header from its tar entry (engine `masterReading`,
  binaryarchive `Contents`), and only the reading is kept, its files and
  libraries as JSON by the archive's name under the archive directory's
  `master/`: a large port's archive kept whole stayed while anything was
  added beside it, the directory's prune reading `master/` as one entry.
- One the site doesn't serve is marked, `<name>.json.none`, and asked again
  once the mark is a day old (`notServedFor`), since the buildbots build a
  version some hours after it lands on master.
- TestAFirstCheckComparesAgainstMastersArchive: the reading kept and no
  archive, a second read from it without fetching, and a day-old mark
  asked again.
