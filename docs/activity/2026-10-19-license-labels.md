# A license file's labels and copyright lines aren't counted

The coalesced roadmap's first item: libxkbcommon 1.7.0 to 1.13.2 held,
"upstream's LICENSE was MIT and HPND and HPND-sell-variant by its text,
and now reads as no license that's known".

- **Built:** `coveredWords` (project/license.go) is how much of a license
  file's words the licenses licensecheck found cover, leaving out copyright
  lines and the labels a file of several heads each license with ("SPDX
  full name:", "SPDX short identifier:", "SPDX-License-Identifier:",
  "License text:") where no license's match takes them in. A template
  takes in the copyright line above its text, so dropping those lines
  before the scan lowered coverage (1.7.0's 79.5% to 67.7%); they're left
  out of the count instead. 1.7.0 reads 79.4%, known, as before. assess.Policy
  is 23.
- **Not reached, said:** 1.13.2 rises from 64.0% to 71.0%, short of
  `wholeText`'s 75, so it still holds. Its uncovered text isn't only
  labels: it adds a section of license statements for its test data, of
  terms no license licensecheck knows ("This software may be used,
  modified, copied, distributed, and sold … provided that the above
  copyright and these terms are retained", "Permission is granted to anyone
  to use, distribute and modify this file in any way …"), 241 words its
  1.7.0 LICENSE hasn't, beside its longer preamble. The roadmap's done-when,
  both files reading as the three licenses, would mean passing over new
  terms, which is the hold's own case; the person decides whether a section
  for files that aren't installed changes that. A paragraph of terms added
  beside a license still holds.
  TestLabelsAndCopyrightLinesAreNoLicensesWords, on the two tags' LICENSE
  files, kept in internal/project/testdata.
