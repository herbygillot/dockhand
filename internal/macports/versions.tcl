proc ::dockhand::select_version {current expression args} {
    if {[llength $args] % 3 != 0} {error "invalid version candidates"}
    set regex [join $expression]
    if {$regex eq ""} {error "empty version expression"}
    regexp -- $regex ""
    set indices {}
    set latest ""
    set index 0
    foreach {version capture subject} $args {
        set captured ""
        if {[regexp -nocase -- $regex $subject matched captured]} {
            if {$captured ne $capture} {error "livecheck capture does not match the tag version"}
            if {$latest eq "" || [vercmp $version $latest] > 0} {
                set latest $version
                set indices [list $index]
            } elseif {[vercmp $version $latest] == 0} {
                lappend indices $index
            }
        }
        incr index
    }
    if {$latest eq ""} {return [list 0]}
    return [list [vercmp $latest $current] {*}$indices]
}
::tclrpc::register select-version ::dockhand::select_version
