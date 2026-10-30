namespace eval ::dockhand {
    # recording_paths is set when a survey records what each evaluation
    # read (tools/survey): the paths file, open, glob, and source read and
    # the command lines exec ran, each worker's hooks, what mportinit read,
    # and the aliases Base gives a worker. installation is what a port is
    # evaluated against (installation.tcl). Go sets both before this script
    # runs; these are their values for a script loaded without it.
    if {![info exists recording_paths]} { variable recording_paths 0 }
    if {![info exists installation]} { variable installation fresh }
    # worker_aliases are the registry_ and _ aliases Base gave the first
    # worker, each its name and target, recorded before the dispatcher
    # hides any of them.
    variable worker_aliases {}

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
        variable recording_paths
        variable worker_aliases
        if {$recording_paths && ![llength $worker_aliases]} {
            foreach name [lsort [interp aliases $worker]] {
                if {[string match registry_* $name] || [string match _* $name]} {
                    lappend worker_aliases [list $name [interp alias $worker $name]]
                }
            }
        }
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
                # record_paths is the parent's recording_paths. reads
                # counts each path read by command, file subcommand, what
                # the path is, and who answered; execs each command line
                # run by who answered; hooks are the Portfile's and its
                # PortGroups' phase bodies, each with where it was declared.
                variable record_paths 0
                variable reads [dict create]
                variable execs [dict create]
                variable hooks {}
                variable tcl_library ""
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
                # note keeps a call's read, as record_paths has it kept: a
                # command line by who answered it, or a path normalized,
                # by what it is (kind_of) and who answered it, the host
                # answering a host path in a modelled context being
                # host-in-model. Path arithmetic reads nothing and is left
                # to the ledger.
                proc note {name arguments answered} {
                    if {$name eq "exec" || ($name eq "open" && [string index [lindex $arguments 0] 0] eq "|")} {
                        variable execs
                        set line [expr {$name eq "exec" ? $arguments : [string range [lindex $arguments 0] 1 end]}]
                        dict incr execs [list [string range $line 0 199] $answered]
                        return
                    }
                    set subcommand ""
                    switch -- $name {
                        file {
                            variable pure
                            set subcommand [lindex $arguments 0]
                            if {$subcommand in $pure} { return }
                            set paths [list [lindex $arguments 1]]
                        }
                        open { set paths [list [lindex $arguments 0]] }
                        source { set paths [list [lindex $arguments end]] }
                        glob { set paths [patterns $arguments] }
                        default { return }
                    }
                    variable reads
                    variable toolchain
                    foreach path $paths {
                        set kind relative
                        if {![catch {interp invokehidden {} file normalize $path} normalized]} {
                            set path $normalized
                            set kind [kind_of $path]
                        }
                        set by $answered
                        if {$by eq "host" && $kind eq "host" && [dict size $toolchain]} { set by host-in-model }
                        dict incr reads [list $name $subcommand $kind $by $path]
                    }
                }
                # kind_of is what an absolute path is: the captured tree's,
                # Base's own library, Tcl's own library, or the host's.
                proc kind_of {path} {
                    variable source_root
                    variable base_root
                    variable base_library
                    variable tcl_library
                    foreach root [list $source_root $base_root] {
                        if {$root ne "" && ($path eq $root || [string first "$root/" $path] == 0)} { return tree }
                    }
                    if {$base_library ne "" && [string first "$base_library/" $path] == 0} { return base }
                    if {$tcl_library ne "" && ($path eq $tcl_library || [string first "$tcl_library/" $path] == 0)} { return library }
                    return host
                }
                # patterns are a glob's patterns, each joined with its
                # -directory or -path.
                proc patterns {arguments} {
                    set prefix ""
                    set i 0
                    while {$i < [llength $arguments]} {
                        switch -- [tcl::prefix match -error {} {-directory -join -nocomplain -path -tails -types --} [lindex $arguments $i]] {
                            -directory {
                                set prefix [lindex $arguments $i+1]/
                                incr i 2
                            }
                            -path {
                                set prefix [lindex $arguments $i+1]
                                incr i 2
                            }
                            -types { incr i 2 }
                            -join - -nocomplain - -tails { incr i }
                            -- {
                                incr i
                                break
                            }
                            default { break }
                        }
                    }
                    return [lmap pattern [lrange $arguments $i end] {string cat $prefix $pattern}]
                }
                # hooked keeps a phase body as makeuserproc makes it, every
                # hook a Portfile or PortGroup declares going through it
                # (portutil.tcl's target_provides).
                proc hooked {cmd op} {
                    variable recording
                    if {$recording} { return }
                    variable hooks
                    lappend hooks [list [lindex $cmd 1] [where] [lindex $cmd 2]]
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
                # parse reads a command line as exec does: the words of each
                # stage of its pipeline, by index, its redirections, each an
                # operator and its target, and whether it runs in the
                # background.
                proc parse {words} {
                    set i 0
                    while {[lindex $words $i] in {-ignorestderr -keepnewline --}} {
                        incr i
                        if {[lindex $words $i-1] eq "--"} { break }
                    }
                    set background [expr {[lindex $words end] eq "&"}]
                    set last [expr {[llength $words] - 1 - $background}]
                    set stages {}
                    set stage {}
                    set redirections {}
                    for {} {$i <= $last} {incr i} {
                        set word [lindex $words $i]
                        if {$word in {| |&}} {
                            lappend stages $stage
                            set stage {}
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
                            lappend stage $i
                            continue
                        }
                        set target [string range $word [string length $operator] end]
                        if {$target eq "" && $operator ne "2>@1"} { set target [lindex $words [incr i]] }
                        lappend redirections [list $operator $target]
                    }
                    lappend stages $stage
                    return [list $stages $redirections $background]
                }
                # pipeline judges a command line in exec's syntax: every
                # program in it admitted, output only to /dev/null or a
                # channel, and nothing left running in the background.
                proc pipeline {words} {
                    lassign [parse $words] stages redirections background
                    if {$background} { return "leaves a process running" }
                    foreach redirection $redirections {
                        lassign $redirection operator target
                        if {$operator in {> 2> >& >> 2>> >>&} && $target ne "/dev/null"} { return "writes $target" }
                    }
                    foreach stage $stages {
                        if {[llength $stage] == 0} { continue }
                        set reason [program [lmap i $stage {lindex $words $i}]]
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
                    # ruby not installed does, and falls back. There is
                    # what the fresh installation has (installation.tcl).
                    # The host's own installation has what the host has.
                    set path [lindex $command 0]
                    variable installation
                    if {[lindex [locate $path] [expr {$installation eq "host" ? 0 : 1}]] eq ""} { return "" }
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
                # dispatch is every hidden command's alias: a refusal, else
                # the modelled toolchain's answer, or the installed ports'
                # or the fresh installation's where they have one, else the
                # host's, counted in the ledger by command, file
                # subcommand, source, and subject, the port or program a
                # question of the installation is about.
                proc dispatch {name args} {
                    variable recording
                    if {$recording} { return [interp invokehidden {} $name {*}$args] }
                    set reason [refusal $name $args]
                    if {$reason ne ""} {
                        set command [string range [list $name {*}$args] 0 199]
                        interp invokehidden {} dockhand_refused $command $reason [where]
                        return -code error -errorcode {DOCKHAND REFUSED} "dockhand refused `$command`: it $reason"
                    }
                    variable ledger
                    variable record_paths
                    variable installation
                    set asked $args
                    set subcommand [expr {$name eq "file" ? [lindex $args 0] : ""}]
                    set answer [modelled $name $args]
                    set source table
                    if {$answer eq "" && $installation eq "installed"} {
                        set answer [installed $name $args]
                        set source installed
                    }
                    if {$answer eq ""} {
                        set answer [fresh $name $args]
                        set source fresh
                    }
                    if {$answer eq "" && $installation ne "host" && ($name eq "exec" || ($name eq "open" && [string index [lindex $args 0] 0] eq "|"))} {
                        set answer [run $name $args]
                        if {[lindex $answer 0] eq "run"} {
                            set args [lindex $answer 1]
                            set answer ""
                        }
                    }
                    if {$answer ne ""} {
                        lassign $answer subject code result options
                        dict incr ledger [list $name $subcommand $source $subject]
                        if {$record_paths} { note $name $asked $source }
                        dict set options -level 1
                        return -options $options $result
                    }
                    # In a modelled context, the host answering is a gap in
                    # the model, host-in-model (docs/oracle.md, decision 1).
                    set source [source_of $name $args]
                    variable toolchain
                    if {$source eq "host" && [dict size $toolchain]} { set source host-in-model }
                    # What the host's own installation is asked, where it
                    # answers (installation host), is about its subject too.
                    variable registry_reads
                    set subject [expr {$name in $registry_reads || $name in {findBinary binaryInPath} ? [lindex $args 0] : ""}]
                    dict incr ledger [list $name $subcommand $source $subject]
                    if {$record_paths} { note $name $asked host }
                    variable ready
                    if {$ready} { ::dockhand_observation::host 2 [list $name {*}$args] }
                    # file stat and lstat set an array in their caller's frame.
                    if {$subcommand in {stat lstat}} { return [uplevel 1 [list interp invokehidden {} $name {*}$args]] }
                    return [interp invokehidden {} $name {*}$args]
                }
                # run is how exec, or open of a pipe, runs from the fresh
                # installation: run and the words to run, or its answer
                # that a program isn't there.
                proc run {name arguments} {
                    set words [expr {$name eq "exec" ? $arguments : [string range [lindex $arguments 0] 1 end]}]
                    lassign [programs $words] words missing
                    if {$missing ne ""} {
                        return [list $missing 1 "couldn't execute \"$missing\": no such file or directory" {-code 1 -errorcode {POSIX ENOENT {no such file or directory}}}]
                    }
                    if {$name eq "open"} { return [list run [lreplace $arguments 0 0 |$words]] }
                    return [list run $words]
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
                    variable record_paths
                    if {$record_paths && [llength [info commands ::makeuserproc]] && ![llength [trace info execution ::makeuserproc]]} {
                        trace add execution ::makeuserproc enter ::dockhand_dispatcher::hooked
                    }
                }
            }
        }
        install_fresh $worker
        install_toolchain $worker
        $worker eval {
            ::dockhand_dispatcher::guard [list exec file open glob findBinary binaryInPath \
                {*}$::dockhand_dispatcher::registry_reads {*}[dict keys $::dockhand_dispatcher::outright]]
            trace add execution ::PortSystem leave ::dockhand_dispatcher::loaded
        }
        $worker eval [list set ::dockhand_dispatcher::programs $::dockhand::host_programs]
        $worker eval [list set ::dockhand_dispatcher::source_root $::dockhand::source_root]
        $worker eval [list set ::dockhand_dispatcher::base_root $::dockhand::base_root]
        $worker eval [list set ::dockhand_dispatcher::base_library [base_library]]
        $worker eval [list set ::dockhand_dispatcher::record_paths $recording_paths]
        $worker eval [list set ::dockhand_dispatcher::tcl_library [file normalize [info library]]]
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
