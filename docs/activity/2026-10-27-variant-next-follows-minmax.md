# Next's variant check names its port, and follows minmax

2026-10-27, roadmap's entry of that name (prioritization, e08c01f5), from
I47 as done in 03136805.

- **The floor half** reads a person's passing check of the default
  variants on `host` or `local`, as before (`floorsRaised`, out of
  `wider`); **the variant half** reads any passing check of the default
  variants, so a minmax pass names the variant next, and a `--variants`
  or `--variants each` check's pass names nothing.
- **One port**: `--variants` builds one port and `variantsTarget` refuses
  a branch that changes more than one without `--only`, so Next names
  the first directory with a variant whose sources changed, by its first
  changed target, with `--only <port>` where the check's plan changes more
  than one target, and says the rest in the row's words ("…, nor
  yq's +docs"). A one-port update's Next is as before, now saying the
  port: "the update changes the sources of jq's +docs, …".
- TestAPassingHostCheckNamesTheWiderCheck: minmax, then the variant once
  minmax passed, then nothing once the variant's check passed; `--only jq`
  where two targets change.
