namespace eval ::dockhand {
    # refusals are the effects the current port's workers attempted and
    # the dispatcher refused, each its command, why, and where. They are
    # kept here in the parent, where the Portfile can't reach them: a catch
    # in the worker hides the error, not the record, and a failed mportopen
    # deletes the worker, not this list. metadata starts each port afresh.
    variable refusals {}
    proc refused {command reason where} {
        variable refusals
        lappend refusals [list $command $reason $where]
        return
    }

    # guard_worker puts the dispatcher (docs/oracle.md, phases 1 and 2) in
    # every port's worker, observed or not. exec, file, open, and glob,
    # socket, and the Base commands that write, reach the network, or run
    # processes are hidden in the worker, those of Pextlib once PortSystem
    # has loaded it, and reach the host only through dispatch. It refuses
    # writes, the network, and programs other than the read-only ones Go
    # gives it (macports.HostPrograms), and passes everything else to the
    # host unchanged, counting each call in the ledger by where its answer
    # comes from: pure path arithmetic, the captured tree, Base's own
    # library, a process, a relative path, a directory enumeration, or the
    # host.
    proc guard_worker {cmd code result op} {
        if {$code != 0} { return }
        set worker [lindex $cmd 1]
        interp alias $worker dockhand_refused {} ::dockhand::refused
        interp hide $worker dockhand_refused
        $worker eval {
            namespace eval ::dockhand_dispatcher {
                # recording is set while dockhand's own code in the worker
                # reads the host, so its calls pass uncounted.
                variable recording 0
                # ready is set by observation once the Portfile's
                # PortSystem has run; see observation.tcl.
                variable ready 0
                variable ledger [dict create]
                variable source_root ""
                variable base_root ""
                variable base_library ""
                variable programs {}
                variable pure {join normalize dirname tail split extension rootname separator nativename pathtype}
                # outright are the commands refused whatever their
                # arguments, and why.
                variable outright [apply {{} {
                    set outright [dict create]
                    foreach {reason names} {
                        "reaches the network" {socket curl curlwrap curlwrap_async check_broken_dns}
                        "runs a process outside the dispatcher" {system mport_exec tracelib macports_create_thread}
                        "changes the filesystem" {mkdtemp mktemp symlink clonefile lchown xinstall set_pingtime}
                        "changes the MacPorts registry" {registry_activate registry_deactivate registry_deactivate_composite registry_install
                            registry_uninstall registry_write registry_new registry_prop_store registry_register_deps registry_bulk_register_files}
                    } {
                        foreach name $names { dict set outright $name $reason }
                    }
                    return $outright
                }}]
                # frames is the caller's stack, below the procedures of
                # this namespace that are running: skip of them.
                proc frames {skip} {
                    set frames {}
                    for {set i 1} {$i <= [info frame] - $skip} {incr i} {
                        set frame [info frame $i]
                        set file ""
                        if {[dict exists $frame file]} { set file [dict get $frame file] }
                        lappend frames [list $file [dict get $frame line] [dict get $frame cmd]]
                    }
                    return $frames
                }
                # where is the innermost line on the stack of a file in the
                # ports tree, a Portfile or PortGroup, or failing that of any
                # file: Base's catch, for one, is a procedure of its own.
                proc where {} {
                    variable source_root
                    variable base_root
                    set found ""
                    for {set i [info frame]} {$i > 0} {incr i -1} {
                        set frame [info frame $i]
                        if {![dict exists $frame file]} { continue }
                        set file [dict get $frame file]
                        set at "$file:[dict get $frame line]"
                        foreach root [list $source_root $base_root] {
                            if {$root ne "" && [string first "$root/" $file] == 0} { return $at }
                        }
                        if {$found eq ""} { set found $at }
                    }
                    return $found
                }
                proc source_of {name arguments} {
                    variable source_root
                    variable base_root
                    variable pure
                    switch -- $name {
                        exec { return process }
                        glob { return enumeration }
                        open {
                            set path [lindex $arguments 0]
                            if {[string index $path 0] eq "|"} { return process }
                        }
                        source { set path [lindex $arguments end] }
                        file {
                            if {[lindex $arguments 0] in $pure} { return pure }
                            set path [lindex $arguments 1]
                        }
                        default { return host }
                    }
                    if {[string index $path 0] ne "/"} { return relative }
                    foreach root [list $source_root $base_root] {
                        if {$root ne "" && ($path eq $root || [string first "$root/" $path] == 0)} { return tree }
                    }
                    variable base_library
                    if {$base_library ne "" && [string first "$base_library/" $path] == 0} { return base }
                    return host
                }
                # refusal is why a call is refused, or empty if it isn't.
                proc refusal {name arguments} {
                    variable outright
                    switch -- $name {
                        exec { return [pipeline $arguments] }
                        open {
                            set path [lindex $arguments 0]
                            if {[string index $path 0] eq "|"} { return [pipeline [string range $path 1 end]] }
                            if {[llength $arguments] > 1 && [writes [lindex $arguments 1]]} { return "opens $path for writing" }
                        }
                        file {
                            switch -- [lindex $arguments 0] {
                                mkdir - delete - copy - rename - tempfile - tempdir { return "changes the filesystem" }
                                link { if {[llength $arguments] > 2} { return "changes the filesystem" } }
                                attributes { if {[llength $arguments] > 3} { return "changes a file's attributes" } }
                                mtime - atime { if {[llength $arguments] > 2} { return "changes a file's times" } }
                            }
                        }
                        glob {}
                        default {
                            if {[dict exists $outright $name]} { return [dict get $outright $name] }
                        }
                    }
                    return ""
                }
                # writes reports whether an open access, a mode string or a
                # list of flags, can write.
                proc writes {access} {
                    if {[regexp {^[rwa][b+]*$} $access]} { return [expr {[string map {b ""} $access] ne "r"}] }
                    foreach flag $access {
                        if {$flag in {WRONLY RDWR APPEND CREAT TRUNC}} { return 1 }
                    }
                    return 0
                }
                # pipeline judges a command line in exec's syntax: every
                # program in it admitted, output only to /dev/null or a
                # channel, and nothing left running in the background.
                proc pipeline {words} {
                    while {[lindex $words 0] in {-ignorestderr -keepnewline --}} {
                        set words [lassign $words switch]
                        if {$switch eq "--"} { break }
                    }
                    if {[lindex $words end] eq "&"} { return "leaves a process running" }
                    set commands {}
                    set command {}
                    for {set i 0} {$i < [llength $words]} {incr i} {
                        set word [lindex $words $i]
                        if {$word in {| |&}} {
                            lappend commands $command
                            set command {}
                            continue
                        }
                        set operator ""
                        foreach candidate {2>@1 >&@ 2>@ >@ <@ << < >>& 2>> >> >& 2> >} {
                            if {[string first $candidate $word] == 0} {
                                set operator $candidate
                                break
                            }
                        }
                        if {$operator eq ""} {
                            lappend command $word
                            continue
                        }
                        if {$operator eq "2>@1"} { continue }
                        set target [string range $word [string length $operator] end]
                        if {$target eq ""} { set target [lindex $words [incr i]] }
                        if {$operator in {> 2> >& >> 2>> >>&} && $target ne "/dev/null"} { return "writes $target" }
                    }
                    lappend commands $command
                    foreach command $commands {
                        if {[llength $command] == 0} { continue }
                        set reason [program $command]
                        if {$reason ne ""} { return $reason }
                    }
                    return ""
                }
                # program judges one command of a pipeline by its program's
                # base name against the programs Go gave.
                proc program {command} {
                    variable programs
                    # A program that isn't there runs nothing: exec fails
                    # as it would have, as the ruby PortGroup's query of a
                    # ruby not installed does, and falls back.
                    set path [lindex $command 0]
                    if {[string index $path 0] eq "/" && ![interp invokehidden {} file executable $path]} { return "" }
                    set name [lindex [split $path /] end]
                    set arguments [lrange $command 1 end]
                    foreach entry $programs {
                        if {![regexp "^(?:[dict get $entry name])\$" $name]} { continue }
                        foreach pattern [dict get $entry refused] {
                            foreach argument $arguments {
                                if {[regexp "^(?:$pattern)\$" $argument]} { return "runs $name with $argument" }
                            }
                        }
                        set rest [lrange $arguments [admitted [dict get $entry options] $arguments] end]
                        switch -- [dict get $entry form] {
                            any { return "" }
                            wrapper {
                                if {[llength $rest]} { return [program $rest] }
                                return ""
                            }
                            subcommand {
                                if {[llength $rest] && [lindex $rest 0] in [dict get $entry subcommands]} { return "" }
                                return "runs $name [lindex $rest 0]"
                            }
                        }
                        if {[llength $rest]} { return "runs $name with [lindex $rest 0]" }
                        return ""
                    }
                    return "runs $name, which is not known to only report"
                }
                # admitted is how many of arguments, from the first, options
                # admit, with the values each takes.
                proc admitted {options arguments} {
                    set i 0
                    while {$i < [llength $arguments]} {
                        set next $i
                        foreach option $options {
                            lassign $option pattern values
                            if {[regexp "^(?:$pattern)\$" [lindex $arguments $i]]} {
                                set next [expr {$i + 1 + $values}]
                                break
                            }
                        }
                        if {$next == $i} { break }
                        set i $next
                    }
                    return $i
                }
                proc dispatch {name args} {
                    variable recording
                    if {!$recording} {
                        set reason [refusal $name $args]
                        if {$reason ne ""} {
                            set command [string range [list $name {*}$args] 0 199]
                            interp invokehidden {} dockhand_refused $command $reason [where]
                            return -code error -errorcode {DOCKHAND REFUSED} "dockhand refused `$command`: it $reason"
                        }
                        variable ledger
                        dict incr ledger [list $name [expr {$name eq "file" ? [lindex $args 0] : ""}] [source_of $name $args]]
                        variable ready
                        if {$ready} { ::dockhand_observation::host 2 [list $name {*}$args] }
                    }
                    return [interp invokehidden {} $name {*}$args]
                }
                # guard hides the commands of names the worker has and
                # hasn't hidden yet, and aliases them to dispatch.
                proc guard {names} {
                    set hidden [interp hidden {}]
                    foreach name $names {
                        if {$name ni $hidden && [llength [info commands ::$name]]} {
                            interp hide {} $name
                            interp alias {} $name {} ::dockhand_dispatcher::dispatch $name
                        }
                    }
                }
                # loaded guards what port1.0 brought with it: Pextlib's
                # commands, system and curl among them, exist only once
                # PortSystem has loaded it, after the worker was made.
                proc loaded {cmd code result op} {
                    variable outright
                    guard [dict keys $outright]
                }
                guard [list exec file open glob {*}[dict keys $outright]]
                trace add execution ::PortSystem leave ::dockhand_dispatcher::loaded
            }
        }
        $worker eval [list set ::dockhand_dispatcher::programs $::dockhand::host_programs]
        $worker eval [list set ::dockhand_dispatcher::source_root $::dockhand::source_root]
        $worker eval [list set ::dockhand_dispatcher::base_root $::dockhand::base_root]
        $worker eval [list set ::dockhand_dispatcher::base_library [base_library]]
    }

    # base_library is where MacPorts Base's own Tcl lives, the port1.0 and
    # macports1.0 its workers load: Base's code, not host state.
    variable library ""
    proc base_library {} {
        variable library
        if {$library eq "" && [regexp {(\S+)/macports1\.0/} [package ifneeded macports [package present macports]] -> found]} {
            set library $found
        }
        return $library
    }
}
