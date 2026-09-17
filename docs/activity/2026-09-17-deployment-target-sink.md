# The deployment-target setter is a benign sink

Asked for on 2026-09-17, the last of the three survey items chosen after the tree coverage survey. Reason 7 there: 562 assessed entries, "macosx_deployment_target is selected by the OS minor version or deployment target, which is not modeled".

## What the population is

Not 536 aqua ports. It is 27 Portfiles, seven of which are `aqua/qt5`, `qt6`, `qt64`, `qt67`, `qt68`, `qt69`, and `qt610` with 534 subports between them, plus `py-wxpython-4.0`, TeXShop, and a dozen others. All of them carry the same block, the qt5 one commented "qt5-qtbase is broken on macOS 15+":

    if {${os.platform} eq "darwin" && [vercmp ${macosx_deployment_target} >= 15.0]} {
        macosx_deployment_target 14.0
    }

## The rule

The modeled evaluation observes a Portfile in profiles that vary the architecture and cross the Darwin boundaries it compares against, so every branch of the version, distfile, and checksum declarations is seen. The deployment target and the OS minor version are not profile dimensions. A read of either is refused where its value could reach a source declaration, and classified harmless where it can only feed a benign sink: a build-only option family, a `system` or `reinplace` argument, a message. A branch guarded by such a read is harmless when every command in it is a sink.

`macosx_deployment_target` as a command, the setter, joins the sinks. Reassigning the deployment target changes how the port compiles and nothing dockhand edits or compares across profiles. The guard above therefore no longer makes the probe inconclusive. A branch that also names a source declaration, `macosx_deployment_target 14.0; distname legacy`, is still refused; the classifier judges every command in the branch.

## Exercise

`assess TeXShop py313-wxpython-4.0 qt6-qtbase`: the version-input and fetch checks pass on all three, where each was inconclusive before. `py313-wxpython-4.0` is ready. TeXShop and `qt6-qtbase` stop at their real next limits, a checksum group named by `${distname}` and checksum values held in variables, which the sink was never going to change. Re-assessing all 562 entries: the deployment-target finding is gone from every one of them. 23 are now input-found, 532 unsupported, 7 unknown. 517 of the unsupported are qt subports whose checksum values live in variables ("checksum value has no unique literal owner"), 11 are qt metaports with no archive of their own, and the rest are host-dependent evaluation, remote patchfiles, a post-fetch hook, and TeXShop's `${distname}`-named group. So the honest yield is 23 ports, about 0.06% of the index, and the qt family's real boundary is the checksum-in-variable convention, a different and larger piece of work than this survey item suggested.

## Tests

`profiles_test.go`: the qt block, and the same block inside `platform darwin`, leave the native profile alone; the block with a `distname` beside the setter is still a gap. The portedit and assess suites pass.
