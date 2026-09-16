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

proc ::dockhand::extract_versions {expression page} {
    set regex [join $expression]
    if {$regex eq ""} {error "empty livecheck expression"}
    # Match each line like Base's regex livecheck. Progress is explicit even for
    # zero-width matches, so malformed patterns cannot trap the interpreter.
    set versions [dict create]
    foreach line [split $page "\n"] {
        set start 0
        while {$start <= [string length $line] && [regexp -nocase -start $start -indices -- $regex $line whole capture]} {
            if {![info exists capture] || [lindex $capture 0] < 0} {error "livecheck requires a version capture"}
            lassign $capture first last
            set version [string range $line $first $last]
            if {$version eq ""} {error "empty livecheck version capture"}
            dict set versions $version 1
            set start [expr {max($start + 1, [lindex $whole 1] + 1)}]
        }
    }
    return [dict keys $versions]
}
::tclrpc::register extract-versions ::dockhand::extract_versions
