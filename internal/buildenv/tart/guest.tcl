# The guest side of dockhand's Tart provider (Design v3 §7). launchd runs it
# as root in a fresh clone of a prepared image, under the image's
# port-tclsh. It builds a check's targets in order, as MacPorts CI does
# (decisions 11 and 22): for each, everything is deactivated and only the
# target's dependencies are installed and activated, then it is linted,
# fetched, checksummed, and installed, and its declared tests run after.
# Each target's result is written to results.json as it finishes, so a
# guest lost midway keeps what it recorded (decision 44).
package require json
package require json::write
json::write indented false

set root /var/tmp/dockhand-check
if {[info exists ::env(DOCKHAND_GUEST_ROOT)]} { set root $::env(DOCKHAND_GUEST_ROOT) }
set fd [open $root/input.json r]
set input [json::json2dict [read $fd]]
close $fd
set prefix [dict get $input prefix]
set port $prefix/bin/port
set results {}
set environment [dict create]

# save writes the results so far, replacing the file whole. A result's
# fields are strings, but for the ports active as it built, a list of
# them, and for where each step of its build began in its log, a list of
# each step's name and line.
proc save {state {detail ""}} {
    global root input results environment
    set targets {}
    foreach result $results {
        set fields {}
        dict for {key value} $result {
            if {$key eq "active"} {
                set ports {}
                foreach entry $value {
                    set object {}
                    dict for {name field} $entry { lappend object $name [json::write string $field] }
                    lappend ports [json::write object {*}$object]
                }
                lappend fields $key [json::write array {*}$ports]
            } elseif {$key eq "steps"} {
                set steps {}
                foreach entry $value {
                    lassign $entry step line
                    lappend steps [json::write object name [json::write string $step] line $line]
                }
                lappend fields $key [json::write array {*}$steps]
            } else {
                lappend fields $key [json::write string $value]
            }
        }
        lappend targets [json::write object {*}$fields]
    }
    set facts {}
    dict for {key value} $environment { lappend facts $key [json::write string $value] }
    set data [json::write object protocol [dict get $input protocol] state [json::write string $state] \
        detail [json::write string $detail] environment [json::write object {*}$facts] targets [json::write array {*}$targets]]
    set fd [open $root/results.json.tmp w]
    puts $fd $data
    close $fd
    file rename -force $root/results.json.tmp $root/results.json
}

# The external commands go through these three, which a test replaces.
# run runs a command with its output appended to a log, returning "" or
# why it failed.
if {[llength [info procs run]] == 0} {
    proc run {log argv} {
        if {[catch {exec {*}$argv >>& $log} message]} { return $message }
        return ""
    }
}
# timed runs a command with a deadline, returning "", "timed out", or why
# it failed. A run past its deadline is asked to stop, with what it
# started, and killed if it hasn't within 10 seconds: a build's compilers
# are port's children, and outlive it.
if {[llength [info procs timed]] == 0} {
    proc timed {log argv seconds} {
        set out [open $log a]
        set chan [open |[concat $argv [list 2>@1]] r]
        fconfigure $chan -blocking 0
        set ::timedDone ""
        fileevent $chan readable [list apply {{chan out} {
            puts -nonewline $out [read $chan]
            if {[eof $chan]} { set ::timedDone eof }
        }} $chan $out]
        set timer [after [expr {$seconds * 1000}] {set ::timedDone timeout}]
        vwait ::timedDone
        after cancel $timer
        if {$::timedDone eq "timeout"} {
            foreach pid [pid $chan] { signal_tree $pid TERM }
            set timer [after 10000 {set ::timedDone kill}]
            vwait ::timedDone
            after cancel $timer
            if {$::timedDone eq "kill"} {
                foreach pid [pid $chan] { signal_tree $pid KILL }
            }
            fileevent $chan readable {}
            close $out
            catch {close $chan}
            return "timed out"
        }
        fileevent $chan readable {}
        close $out
        fconfigure $chan -blocking 1
        if {[catch {close $chan} message]} { return $message }
        return ""
    }
}
# signal_tree signals a process and each it started, theirs first.
proc signal_tree {pid signal} {
    if {![catch {exec /usr/bin/pgrep -P $pid} children]} {
        foreach child [split $children \n] { signal_tree $child $signal }
    }
    catch {exec /bin/kill -$signal $pid}
}
# bound_words says a bound in seconds as a person would: 6h, 10m, 90s.
proc bound_words {seconds} {
    if {$seconds % 3600 == 0} { return "[expr {$seconds / 3600}]h" }
    if {$seconds % 60 == 0} { return "[expr {$seconds / 60}]m" }
    return "${seconds}s"
}
# declares_tests asks MacPorts whether the port declares a test phase.
if {[llength [info procs declares_tests]] == 0} {
    proc declares_tests {portdir name variants} {
        set handle [mportopen "file://$portdir" [list subport $name] $variants]
        set worker [ditem_key $handle workername]
        set declared [$worker eval {tbool test.run}]
        mportclose $handle
        return $declared
    }
}
# checkout is where MacPorts' Git fetch left a port's source: its
# worksrcpath, the "full path to extracted source code" as the Portfile
# reference has it. Base's git fetch clones into it (portfetch.tcl's
# gitfetch, alike in 2.11 and 2.12), and Portfiles' own post-fetch steps
# find the clone there. It is read as declares_tests reads test.run.
if {[llength [info procs checkout]] == 0} {
    proc checkout {portdir name variants} {
        set handle [mportopen "file://$portdir" [list subport $name] $variants]
        set worker [ditem_key $handle workername]
        set path [$worker eval {option worksrcpath}]
        mportclose $handle
        return $path
    }
}
# fetched is the commit a checkout is at, as Git's own rev-parse reads it.
# The checkout is MacPorts' unprivileged user's, and this program runs as
# root, so Git is told it is safe for this one command, as Base tells it
# when it reads a port's own files with Git (portmain.tcl).
proc fetched {path} {
    return [string trim [exec /usr/bin/git -c safe.directory=* -C $path rev-parse --verify {HEAD^{commit}}]]
}

# fact is a command's output, trimmed, or nothing.
proc fact {args} {
    if {[catch {exec {*}$args 2>/dev/null} value]} { return "" }
    return [string trim $value]
}

# why is what MacPorts said about a failure, from the last Error lines of
# the failing step's own part of the log, or the command's own message.
# Each step is marked before it runs (mark), so the latest mark is where
# the failing step began. The whole log held earlier steps' errors too:
# the rust run, #35084, quoted lint's "Line 120 hardcodes /opt/local, use
# ${prefix} instead", which lint says and goes on, for the test step's
# failure. A step whose mark couldn't be read is read from the mark
# before it, as more of the log than its own; with none, the whole log.
proc why {log message} {
    global counted
    set from 0
    if {[dict exists $counted $log]} { set from [lindex [dict get $counted $log] 0] }
    set fd [open $log r]
    # seek counts bytes, as mark counts them, whatever the channel's
    # encoding.
    seek $fd $from
    set text [read $fd]
    close $fd
    set errors {}
    # What the failing command was, and what it said last before it
    # failed: MacPorts' "Command failed:" line, written by its system
    # call (portutil.tcl), and the tool's own lines above it, not
    # MacPorts' debug lines. "Failed to build mods: command execution
    # failed" said neither, where Go's "cannot find package" was in the
    # log (field testing, 2026-10-02). MacPorts doesn't document these
    # lines; the person accepted reading them, as an exception to
    # depending on its documented interfaces only.
    set command ""
    set said {}
    set recent {}
    foreach line [split $text \n] {
        if {[regexp {^Error: (.+)$} $line -> error] && ![regexp {^(See |Follow https://guide|Processing of port )} $error]} {
            lappend errors [string trim $error]
            continue
        }
        if {[regexp {^(?::[a-z]+:[a-z]+ )?Command failed:\s*(.+)$} $line -> failed]} {
            set command [string trim $failed]
            set said $recent
            continue
        }
        set line [string trim $line]
        # Each phase MacPorts runs starts what its failure said afresh.
        if {[regexp {Executing org\.macports\.} $line]} {
            set recent {}
            continue
        }
        if {$line eq "" || [regexp {^(DEBUG|--->|Exit code|Executing:|:debug:)} $line]} { continue }
        lappend recent [string range $line 0 299]
        if {[llength $recent] > 3} { set recent [lrange $recent end-2 end] }
    }
    if {![llength $errors]} { return $message }
    set detail [join [lrange $errors end-2 end] "; "]
    if {$command ne ""} {
        if {[llength $said]} {
            set detail [regsub {: command execution failed$} $detail ": [join $said {; }]"]
        }
        append detail "; the command was: [string range $command 0 499]"
    }
    return $detail
}

# counted is how far each target's log has been counted: the bytes read,
# and the lines they held.
set counted [dict create]

# mark records with a target's result where a step of its build begins in
# its log: the line the step's output starts on, counting from 1. A
# target's log holds its dependencies' installs as well as its own build,
# and hugo's own phases began near line 46,400 of 47,000 (the hugo
# exercise), so logs --port starts at the target's own. This program runs
# each step, so it says where each begins, and nothing is read from
# MacPorts' progress lines, which aren't an interface it documents. A log
# is counted from where its last count ended. A step whose place can't be
# read is left out, and the build goes on.
proc mark {resultVar log step} {
    upvar 1 $resultVar result
    global counted
    lassign {0 0} offset lines
    if {[dict exists $counted $log]} { lassign [dict get $counted $log] offset lines }
    if {[catch {
        set fd [open $log r]
        fconfigure $fd -translation binary
        seek $fd $offset
        set text [read $fd]
        close $fd
    }]} {
        return
    }
    incr offset [string length $text]
    incr lines [regexp -all {\n} $text]
    dict set counted $log [list $offset $lines]
    dict lappend result steps [list $step [expr {$lines + 1}]]
}

# digests remembers each archive's digest: the same archive is active for
# many targets.
set digests [dict create]

# digest is an archive's digest, sha256:<hex>, or nothing for a directory,
# which an image that keeps no archives has in its place.
proc digest {path} {
    global digests
    if {![dict exists $digests $path]} {
        set sum ""
        if {[file isfile $path] && [regexp {^([0-9a-f]{64})\s} [fact /usr/bin/shasum -a 256 $path] -> hex]} {
            set sum sha256:$hex
        }
        dict set digests $path $sum
    }
    return [dict get $digests $path]
}

# consumed is what a target's build read beyond its own directory
# (decision 28): the ports active as it built, other than itself, each
# with its version, revision, and variants, where its name resolves in the
# ports tree, and its archive's digest. The target's own archive's digest
# comes with them, where it is active, and where the archive is, for the
# host to keep it. Each is asked of port once for all the ports.
proc consumed {name} {
    global port root
    set ports {}
    set specs {}
    set own ""
    foreach line [split [fact $port -q installed active] \n] {
        if {![regexp {^\s*(\S+) (@\S+) \(active\)$} $line -> other spec]} { continue }
        if {[string equal -nocase $other $name]} {
            set own [list $other $spec]
        } else {
            lappend ports $other
            lappend specs $other $spec
        }
    }
    set inputs [dict create active {}]
    if {$own ne ""} {
        set location [fact $port -q location {*}$own]
        dict set inputs archive [digest $location]
        if {[dict get $inputs archive] ne ""} { dict set inputs archive_file $location }
    }
    if {![llength $ports]} { return $inputs }
    set locations [split [fact $port -q location {*}$specs] \n]
    if {[llength $locations] != [llength $ports]} {
        error "port location named [llength $locations] archives for [llength $ports] ports"
    }
    # A name port can't resolve fails the whole list; each is then asked
    # alone, and one that fails resolves nowhere.
    set directories [split [fact $port -q dir {*}$ports] \n]
    if {[llength $directories] != [llength $ports]} {
        set directories [lmap other $ports {fact $port -q dir $other}]
    }
    set trees [list $root/ports/ [file normalize $root/ports]/]
    set active {}
    foreach other $ports {_ spec} $specs location $locations directory $directories {
        set relative ""
        foreach tree $trees {
            if {[string first $tree $directory] == 0} { set relative [string range $directory [string length $tree] end] }
        }
        set location [string trim $location]
        set entry [dict create name $other spec $spec directory $relative archive [digest $location]]
        # Where its archive is, for the host to keep it for a later guest
        # (batch 90), as the target's own is kept.
        if {[dict get $entry archive] ne ""} { dict set entry archive_file $location }
        lappend active $entry
    }
    dict set inputs active $active
    return $inputs
}

# build builds one target, returning its result.
proc build {index target} {
    global root port input results
    set id [dict get $target id]
    set name [dict get $target name]
    set log $root/target-$index.log
    set result [dict create id $id outcome "" phase "" tests none log target-$index.log detail ""]
    close [open $log a]
    if {[dict exists $target blocked] && [dict get $target blocked]} {
        dict set result outcome blocked
        return $result
    }
    # A changed dependency that did not pass blocks its dependents: an old
    # build of it never stands in for the one the branch changes.
    if {[dict exists $target depends_on]} {
        foreach dependency [dict get $target depends_on] {
            foreach earlier $results {
                if {[dict get $earlier id] eq $dependency && [dict get $earlier outcome] ne "passed"} {
                    dict set result outcome blocked
                    dict set result detail "$dependency did not pass"
                    return $result
                }
            }
        }
    }
    set portdir [file dirname [file join $root ports [dict get $target portfile]]]
    set variants {}
    set selection [list subport=$name]
    if {[dict exists $target variants]} {
        dict for {variant enabled} [dict get $target variants] {
            set sign [expr {$enabled ? "+" : "-"}]
            lappend variants $variant $sign
            lappend selection $sign$variant
        }
    }
    set here [list $port -N -D $portdir]
    set fail {apply {{result phase detail} { dict set result outcome failed; dict set result phase $phase; dict set result detail $detail; return $result }}}

    # Only this target's dependencies are active when it builds.
    if {[fact $port -q installed active] ne ""} {
        mark result $log deactivate
        if {[set message [run $log [list $port -N -f deactivate active]]] ne ""} {
            return [{*}$fail $result install "deactivating the ports before it failed: [why $log $message]"]
        }
    }
    # An earlier target of the same port, another variant's build, left
    # its work directory, which MacPorts refuses to go on with under other
    # variants; CI cleans up between ports too (mpbb cleanup).
    mark result $log clean
    if {[set message [run $log [concat $here clean --work $selection]]] ne ""} {
        return [{*}$fail $result fetch "cleaning an earlier build's work failed: [why $log $message]"]
    }
    # Lint has its bound, and the build, its dependencies' installs, its
    # fetch and checksum, and its install, shares one, which a build that
    # runs past is ended at, as its tests are at theirs (D16).
    set lintBound [dict get $input lint_timeout]
    set buildBound [dict get $input build_timeout]
    set deadline [expr {[clock seconds] + $buildBound}]
    set left [list apply {{deadline} { expr {max(1, $deadline - [clock seconds])} }} $deadline]
    set over "the build ran past its [bound_words $buildBound] bound (providers.tart.build_timeout), so it was ended"
    mark result $log lint
    if {[set message [timed $log [concat $here lint $selection] $lintBound]] eq "timed out"} {
        return [{*}$fail $result lint "lint ran past its [bound_words $lintBound] bound, so it was ended"]
    } elseif {$message ne ""} {
        return [{*}$fail $result lint [why $log $message]]
    }
    set dependencies [fact $port -q echo depof:$name]
    if {$dependencies ne ""} {
        mark result $log dependencies
        if {[set message [timed $log [concat [list $port -N -d install --unrequested] $dependencies] [{*}$left]]] eq "timed out"} {
            return [{*}$fail $result install "installing its dependencies, $over"]
        } elseif {$message ne ""} {
            return [{*}$fail $result install "a dependency failed to install: [why $log $message]"]
        }
        # A dependency with no archive for the guest's release builds from
        # source, and what it built with stays active for the target: git
        # left gettext's msgfmt for taisei, which declares no gettext, and
        # its check passed where MacPorts CI's failed, installing git from
        # its archive (field testing, 2026-10-07). Only the target's
        # closure stays active: its dependencies, of every kind, and theirs
        # other than for building, as port(1)'s rdeps --no-build has them.
        set closure $dependencies
        foreach dependency $dependencies {
            foreach line [split [fact $port -q rdeps --no-build $dependency] \n] {
                if {[set other [string trim $line]] ne ""} { lappend closure $other }
            }
        }
        set strays {}
        foreach line [split [fact $port -q installed active] \n] {
            if {[regexp {^\s*(\S+) @\S+ \(active\)$} $line -> other] && [lsearch -exact -nocase $closure $other] < 0 && $other ni $strays} {
                lappend strays $other
            }
        }
        if {[llength $strays]} {
            set fd [open $log a]
            puts $fd "dockhand: deactivating what the dependencies built with, outside $name's dependencies: [join $strays {, }]"
            close $fd
            if {[set message [run $log [concat [list $port -N -f deactivate] $strays]]] ne ""} {
                return [{*}$fail $result install "deactivating what its dependencies built with failed: [why $log $message]"]
            }
        }
    }
    foreach phase {fetch checksum} {
        mark result $log $phase
        if {[set message [timed $log [concat $here -d $phase $selection] [{*}$left]]] eq "timed out"} {
            return [{*}$fail $result $phase $over]
        } elseif {$message ne ""} {
            return [{*}$fail $result $phase [why $log $message]]
        }
        # What a Git fetch checked out is the source the target builds
        # from, which its tag doesn't bind: it is reported, and where it
        # isn't the commit the check expected, the source moved since the
        # check was planned, and nothing is built from it. One that can't
        # be read is said in the log, and builds as it did before.
        if {$phase eq "fetch" && [dict exists $target git]} {
            set git [dict get $target git]
            if {[catch {fetched [checkout $portdir $name $variants]} commit]} {
                set fd [open $log a]
                puts $fd "dockhand: which commit the fetch checked out wasn't read: $commit"
                close $fd
                continue
            }
            dict set result fetched $commit
            # Read as a string: expr would take an abbreviated commit
            # that's all digits for a number, and one led by a zero for
            # octal, as 00230075 is 77885.
            set expect ""
            if {[dict exists $git expect]} {
                set expect [dict get $git expect]
            }
            if {$expect ne "" && [string first $expect $commit] != 0} {
                set ref "the default branch"
                if {[dict get $git ref] ne ""} { set ref "git.branch [dict get $git ref]" }
                return [{*}$fail $result fetch "the source moved: $ref named $expect when the check was planned, and the fetch checked out $commit"]
            }
        }
    }
    # Its dependencies are in place, as CI's install-port has them, unless
    # its variants ask for others, which install then brings. The target
    # itself is built from its source, never installed from a published
    # archive, as CI's install-port --source builds it: an archive of the
    # same version, revision, and variants is master's Portfile's build,
    # not the branch's.
    set install [concat $here -dks install --unrequested $selection]
    if {![llength $variants]} { set install [concat $here -dkns install --unrequested $selection] }
    mark result $log install
    if {[set message [timed $log $install [{*}$left]]] eq "timed out"} {
        return [{*}$fail $result install $over]
    } elseif {$message ne ""} {
        return [{*}$fail $result install [why $log $message]]
    }
    dict set result outcome passed
    set tests [dict get $input tests]
    if {$tests eq "skip"} {
        dict set result tests skipped
    } elseif {[declares_tests $portdir $name $variants]} {
        mark result $log test
        set message [timed $log [concat $here -d test $selection] [dict get $input test_timeout]]
        switch -- $message {
            "" { dict set result tests passed }
            "timed out" { dict set result tests timed-out }
            default { dict set result tests failed }
        }
        if {$message ne ""} {
            dict set result detail "tests: [why $log $message]"
            if {$tests eq "required"} { dict set result outcome failed; dict set result phase test }
        }
    }
    return $result
}

save running
try {
    dict set environment user [fact /usr/bin/id -un]
    dict set environment macos [fact /usr/bin/sw_vers -productVersion]
    dict set environment build [fact /usr/bin/sw_vers -buildVersion]
    dict set environment architecture [fact /usr/bin/uname -m]
    dict set environment developer_dir [fact /usr/bin/xcode-select -p]
    if {[regexp -line {^version: (.+)$} [fact /usr/sbin/pkgutil --pkg-info=com.apple.pkg.CLTools_Executables] -> tools]} {
        dict set environment tools $tools
    }
    # Xcode's version and build, in an image with Xcode; xcodebuild
    # refuses without it.
    set xcodebuild [fact /usr/bin/xcodebuild -version]
    if {[regexp -line {^Xcode (\S+)$} $xcodebuild -> xcode]} {
        dict set environment xcode $xcode
    }
    if {[regexp -line {^Build version (\S+)$} $xcodebuild -> xcodeBuild]} {
        dict set environment xcode_build $xcodeBuild
    }
    dict set environment macports [fact $port version]
    if {![info exists foreignManagers]} { set foreignManagers {/opt/homebrew /usr/local/Homebrew /usr/local/Cellar /sw /opt/pkg} }
    foreach foreign $foreignManagers {
        if {[file exists $foreign]} { error "the image has another package manager, at $foreign" }
    }
    if {[set installed [fact $port -q installed]] ne ""} { error "the image already has ports installed: $installed" }
    if {![file exists $root/ports/PortIndex]} { error "the staged ports tree has no PortIndex" }
    set sources [open $prefix/etc/macports/sources.conf w]
    puts $sources "file://$root/ports \[default\]"
    close $sources
    # The archives dockhand kept of targets this guest installs rather than
    # builds (decision 28) are an archive site of MacPorts' own kind: local,
    # so tried first, and verified by the keys it is told to trust.
    if {[dict exists $input archives] && [llength [dict get $input archives]]} {
        set keys [open $prefix/etc/macports/pubkeys.conf a]
        foreach key [dict get $input archive_keys] { puts $keys $key }
        close $keys
        set types {}
        foreach archive [dict get $input archives] {
            set type [string range [file extension [dict get $archive name]] 1 end]
            if {$type ni $types} { lappend types $type }
        }
        set sites [open $prefix/etc/macports/archive_sites.conf a]
        foreach type $types {
            puts $sites "\nname dockhand_$type\nurls file://[dict get $input archive_site]/\ntype $type\nprefix $prefix"
        }
        close $sites
    }
    package require macports
    mportinit
    set platform [dict get $input platform]
    foreach {key actual} [list OS $::macports::os_platform Version $::macports::os_major Architecture $::macports::build_arch] {
        if {[dict get $platform $key] ne $actual} { error "the guest's $key is $actual, not [dict get $platform $key]" }
    }
} on error {message} {
    save errored $message
    exit 1
}
# What the guest found about itself is written now, not only with the
# first result, so it is read however soon the program ends.
save running
set index 0
foreach target [dict get $input targets] {
    incr index
    if {[catch {build $index $target} result]} {
        save errored "building [dict get $target id]: $result"
        exit 1
    }
    # What a verdict's build read is recorded with it; a verdict stands
    # without it, its inputs unknown.
    if {[dict get $result outcome] in {passed failed}} {
        if {[catch {consumed [dict get $target name]} inputs]} {
            set log [open $root/[dict get $result log] a]
            puts $log "dockhand: what this build read wasn't recorded: $inputs"
            close $log
        } else {
            set result [dict merge $result $inputs]
        }
    }
    lappend results $result
    save running
}
save finished
