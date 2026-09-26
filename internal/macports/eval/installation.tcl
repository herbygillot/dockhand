namespace eval ::dockhand {
    # The installation a port is evaluated against is a fresh one
    # (docs/oracle.md, phase 4): MacPorts Base alone in its prefix and an
    # empty registry, as MacPorts CI and a new Mac start from, whatever this
    # Mac has installed. The dispatcher answers the worker's questions about
    # it here, and they never reach the host's prefix or registry.

    # skeleton is the fresh prefix's directories, relative to it: those
    # Base's installer makes from its mtree files, which Base keeps in its
    # own share directory.
    variable skeleton ""
    proc skeleton {} {
        variable skeleton
        if {$skeleton ne ""} { return $skeleton }
        set skeleton [dict create "" 1]
        foreach name {prefix base} {
            set mtree [file join $::macports::prefix share macports install $name.mtree]
            if {[catch {open $mtree} channel]} { continue }
            set stack {}
            foreach line [split [read $channel] \n] {
                set word [lindex [string trim $line] 0]
                switch -glob -- $word {
                    "" - "#*" - "/*" - "." {}
                    ".." { set stack [lrange $stack 0 end-1] }
                    default {
                        lappend stack $word
                        dict set skeleton [join $stack /] 1
                    }
                }
            }
            close $channel
        }
        return $skeleton
    }

    # install_fresh gives a worker's dispatcher the fresh installation:
    # where it lives, and the answers.
    proc install_fresh {worker} {
        $worker eval {
            namespace eval ::dockhand_dispatcher {
                variable prefix ""
                variable applications ""
                variable skeleton {}
                # Base's own files: its library, configuration, and state,
                # and its programs.
                variable base_areas {libexec/macports share/macports etc/macports var/macports}
                variable base_files {bin/port bin/portindex bin/portmirror bin/port-tclsh bin/daemondo man}
                # nowhere is a directory that doesn't exist: an absent
                # path's question is asked of nowhere joined with it, and
                # Tcl answers as it would for the path, in its own words.
                variable nowhere /.dockhand-absent
                # absent reports whether a path is one the fresh
                # installation doesn't have: under the prefix but neither
                # Base's own nor a directory of its skeleton, or anything
                # inside the applications directory.
                proc absent {path} {
                    variable prefix
                    variable applications
                    if {$applications ne "" && [string first "$applications/" $path] == 0} { return 1 }
                    if {$prefix eq "" || [string first "$prefix/" $path] != 0} { return 0 }
                    set relative [string trimright [string range $path [string length $prefix]+1 end] /]
                    variable skeleton
                    variable base_files
                    if {[dict exists $skeleton $relative] || $relative in $base_files} { return 0 }
                    variable base_areas
                    foreach area $base_areas {
                        if {[string first "$area/" "$relative/"] == 0} { return 0 }
                    }
                    return 1
                }
                # locate is where exec would find a program, as the host
                # has it and as the fresh installation does: the first
                # executable along PATH for a bare name, the path itself
                # otherwise. Either is empty where the program isn't.
                proc locate {program} {
                    if {[string first / $program] >= 0} {
                        set host $program
                        if {[string index $program 0] eq "/" && ![interp invokehidden {} file executable $program]} { set host "" }
                        return [list $host [expr {[absent $program] ? "" : $host}]]
                    }
                    set host ""
                    foreach directory [split $::env(PATH) :] {
                        set candidate $directory/$program
                        if {![interp invokehidden {} file executable $candidate]} { continue }
                        if {$host eq ""} { set host $candidate }
                        if {![absent $candidate]} { return [list $host $candidate] }
                    }
                    return [list $host ""]
                }
                # programs answers a command line's programs from the fresh
                # installation: the words to run, a bare name the prefix
                # would have shadowed replaced by the path the fresh
                # installation runs instead, and a program the host has
                # and the fresh installation doesn't, if there is one.
                proc programs {words} {
                    foreach stage [lindex [parse $words] 0] {
                        if {[llength $stage] == 0} { continue }
                        set i [lindex $stage 0]
                        set program [lindex $words $i]
                        lassign [locate $program] host fresh
                        if {$fresh eq "" && $host ne ""} { return [list $words $program] }
                        if {$fresh ne $host && [string first / $program] < 0} { lset words $i $fresh }
                    }
                    return [list $words ""]
                }
                # registry answers the worker's registry aliases as an empty
                # registry does, in Base's words (registry2.0).
                proc registry {name arguments} {
                    switch -- $name {
                        registry_active {
                            if {[lindex $arguments 0] eq ""} { error "Registry error: No ports registered as active." }
                            error "Registry error: [lindex $arguments 0] not registered as installed & active."
                        }
                        registry_open {
                            lassign $arguments port version revision variants epoch
                            if {$revision eq ""} { set revision 0 }
                            return -code error -errorcode registry::not-found "no matching port found for: name=$port, version=$version, revision=$revision, variants=$variants, epoch=$epoch"
                        }
                        registry_list_depends { return {} }
                        registry_fileinfo_for_file {
                            if {[absent [lindex $arguments 0]]} { return {} }
                            return [interp invokehidden {} $name {*}$arguments]
                        }
                        registry_fileinfo_for_index {
                            variable prefix
                            set info {}
                            foreach file [lindex $arguments 0] {
                                if {[string index $file 0] ne "/"} { set file $prefix/$file }
                                lappend info [registry registry_fileinfo_for_file [list $file]]
                            }
                            return $info
                        }
                        default { return 0 }
                    }
                }
                variable registry_reads {registry_active registry_open registry_exists registry_exists_for_name registry_file_registered
                    registry_port_registered registry_list_depends registry_fileinfo_for_file registry_fileinfo_for_index _portnameactive}
                # binaryInPath and findBinary, Base's lookups of a program
                # along PATH, run in the parent and see the host's prefix;
                # these are the same, in the worker, over the fresh one.
                # Their reads stay as unobserved as they were in the parent.
                proc executable {path} {
                    expr {![absent $path] && [interp invokehidden {} file executable $path]}
                }
                proc binaryInPath {program} {
                    foreach directory [split $::env(PATH) :] {
                        if {[executable $directory/$program]} { return $directory/$program }
                    }
                    return -code error "Failed to locate '$program' in path: '$::env(PATH)'"
                }
                proc findBinary {program {hint {}}} {
                    if {$hint ne "" && [executable $hint]} { return $hint }
                    if {[catch {binaryInPath $program} found]} {
                        error "$found or at its MacPorts configuration time location, did you move it?"
                    }
                    return $found
                }
                # fresh is the fresh installation's answer to a call, as its
                # subject, then catch's code, result, and options; or
                # nothing, for a call it has no say in.
                proc fresh {name arguments} {
                    variable registry_reads
                    variable nowhere
                    switch -- $name {
                        findBinary - binaryInPath {
                            set code [catch {$name {*}$arguments} result options]
                            return [list [lindex $arguments 0] $code $result $options]
                        }
                        file {
                            variable pure
                            set path [lindex $arguments 1]
                            if {[lindex $arguments 0] in $pure || ![absent $path]} { return }
                            set index 1
                        }
                        open {
                            set path [lindex $arguments 0]
                            if {[string index $path 0] eq "|" || ![absent $path]} { return }
                            set index 0
                        }
                        glob { return [enumerate $arguments] }
                        default {
                            if {$name ni $registry_reads} { return }
                            set code [catch {registry $name $arguments} result options]
                            return [list [lindex $arguments 0] $code $result $options]
                        }
                    }
                    set code [catch {interp invokehidden {} $name {*}[lreplace $arguments $index $index $nowhere$path]} result options]
                    set result [string map [list $nowhere$path $path] $result]
                    if {[dict exists $options -errorinfo]} { dict set options -errorinfo $result }
                    return [list "" $code $result $options]
                }
                # enumerate answers a glob from the host with what the
                # fresh installation lacks taken out, and Tcl's own error
                # if that leaves nothing a glob without -nocomplain needs.
                proc enumerate {arguments} {
                    set directory ""
                    set tails 0
                    set complain 1
                    set i 0
                    # glob takes its switches abbreviated, as -dir.
                    while {$i < [llength $arguments]} {
                        switch -- [tcl::prefix match -error {} {-directory -join -nocomplain -path -tails -types --} [lindex $arguments $i]] {
                            -directory {
                                set directory [lindex $arguments $i+1]
                                incr i 2
                            }
                            -path - -types { incr i 2 }
                            -tails {
                                set tails 1
                                incr i
                            }
                            -nocomplain {
                                set complain 0
                                incr i
                            }
                            -join { incr i }
                            -- {
                                incr i
                                break
                            }
                            default { break }
                        }
                    }
                    set patterns [lrange $arguments $i end]
                    if {[catch {interp invokehidden {} glob -nocomplain {*}[lrange $arguments 0 $i-1] {*}$patterns} found]} { return }
                    set kept {}
                    foreach match $found {
                        set path $match
                        if {$tails && $directory ne ""} { set path $directory/$match }
                        if {![absent $path]} { lappend kept $match }
                    }
                    if {[llength $kept] == [llength $found]} { return }
                    if {[llength $kept] == 0 && $complain} {
                        set noun [expr {[llength $patterns] == 1 ? "pattern" : "patterns"}]
                        return [list "" 1 "no files matched glob $noun \"[join $patterns]\"" {-code 1 -errorcode {TCL OPERATION GLOB NOMATCH}}]
                    }
                    return [list "" 0 $kept {-code 0}]
                }
            }
        }
        $worker eval [list set ::dockhand_dispatcher::prefix $::macports::prefix]
        $worker eval [list set ::dockhand_dispatcher::applications $::macports::applications_dir]
        $worker eval [list set ::dockhand_dispatcher::skeleton [skeleton]]
    }
}
