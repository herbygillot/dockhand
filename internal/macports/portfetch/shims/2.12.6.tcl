# The fetch session's init script: MacPorts' own curl (pextlib), with
# macports.conf loaded by mportinit — proxies and defaults are port's
# own, not reimplemented.
package require macports
mportinit

# fetchdist downloads one URL to a destination file with the port's own
# fetch exceptions applied — the same flags portfetch.tcl passes to
# base's curl. Compression stays off: checksums are of the wire bytes.
proc fetchdist {url dest {disable_epsv 0} {ignore_sslcert 0} {ua ""}} {
    set args [list fetch]
    if {$disable_epsv} { lappend args --disable-epsv }
    if {$ignore_sslcert} { lappend args --ignore-ssl-cert }
    if {$ua ne ""} { lappend args --user-agent $ua }
    lappend args $url $dest
    curl {*}$args
    return ok
}
::tclrpc::register fetchdist fetchdist

# MacPorts' embedder ui API (macports.tcl documents ui_prefix and
# ui_channels): while a livecheck run is capturing, every ui message
# lands in the capture file, priority-tagged for parsing; otherwise ui
# is silenced — the protocol never depended on it.
proc ui_prefix {priority} { return "$priority\t" }
proc ui_channels {priority} {
    global dockhand_ui_fd
    if {[info exists dockhand_ui_fd]} { return [list $dockhand_ui_fd] }
    return {}
}

# livecheckrun executes the port's own livecheck phase — mportexec of
# the livecheck target, the exact machinery `port livecheck` drives —
# and returns the captured ui output. Every livecheck.type works,
# "default" resolution and the tree's type files included, because
# nothing is reimplemented.
proc livecheckrun {portdir subport dest} {
    global dockhand_ui_fd
    set opts {}
    if {$subport ne ""} {
        set opts [list subport $subport]
    }
    set handle [mportopen "file://$portdir" $opts {}]
    set dockhand_ui_fd [open $dest w]
    # ui channels are cached at init; re-init so ui_channels above is
    # consulted, capture, then re-init back to silence.
    macports::ui_init_all
    set code [catch {mportexec $handle livecheck} result]
    catch {close $dockhand_ui_fd}
    unset dockhand_ui_fd
    macports::ui_init_all
    mportclose $handle
    if {$code} {
        return -code error $result
    }
    set fd [open $dest r]
    set out [read $fd]
    close $fd
    return $out
}
::tclrpc::register livecheckrun livecheckrun

# vercmp exposes MacPorts' own version ordering.
::tclrpc::register vercmp vercmp
