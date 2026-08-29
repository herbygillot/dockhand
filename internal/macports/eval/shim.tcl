# The evaluator's Session init script: MacPorts semantics for a shell Session.
# Values only — spans never cross this boundary.
#
# mportinit may print to stdout; the Session protocol tolerates that as
# noise, so nothing here needs to fight the ui layer yet.
package require macports
mportinit

# snapshot returns the evaluated metadata of the port at an absolute
# portdir, as a flat dict. With a subport argument, that subport is the
# evaluation context. Fields absent from PortInfo are omitted. The subports
# field, when present, lists the port's subports — snapshot structure for
# the caller's enumeration, not metadata of this context.
proc snapshot {portdir {subport ""} {variations {}}} {
    set opts {}
    if {$subport ne ""} {
        set opts [list subport $subport]
    }
    set handle [mportopen "file://$portdir" $opts $variations]
    array set info [mportinfo $handle]
    set out [dict create]
    foreach field {
        name version revision epoch categories license
        maintainers platforms
        depends_fetch depends_extract depends_patch
        depends_build depends_lib depends_run depends_test
        subports
    } {
        if {[info exists info($field)]} {
            dict set out $field $info($field)
        }
    }
    # checksums and distfiles are port options, not PortInfo: read them
    # from the port's own worker interpreter while the handle is open.
    set worker [ditem_key $handle workername]
    foreach opt {checksums distfiles} {
        if {![catch {$worker eval [list option $opt]} val] && $val ne ""} {
            dict set out $opt $val
        }
    }
    mportclose $handle
    return $out
}
::tclrpc::register snapshot snapshot
