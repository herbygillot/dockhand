# The guest side of dockhand's Tart provider (Design v3 §7). launchd runs it
# as root in a fresh clone of a prepared image, under the image's
# port-tclsh. It builds a check's targets in order, as MacPorts CI does
# (decisions 11 and 22): for each, everything is deactivated and only the
# target's dependencies are installed and activated, then it is linted,
# fetched, checksummed, and installed, and its declared tests run after.
# Each target's result is written to results.json as it finishes, so a
# guest lost midway keeps what it recorded (decision 44), and the target
# it is building is written as it begins, so the one a lost guest was
# building is recorded as interrupted.
#
# The local provider runs it on this Mac, in a MacPorts prefix of the
# person's (input's local): there MacPorts reads the staged tree through
# the macports.conf the host names in PORTSRC, and the prefix's
# etc/macports is never written. It builds among whatever is active,
# unless the prefix is one dockhand may clean, makes a port snapshot
# before it changes anything, and puts the prefix back after, unless it
# is asked to keep what it built active.
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
# otool is Xcode's, which reads a Mach-O's load commands; a test stands
# one in.
set otool /usr/bin/otool
if {[info exists ::env(DOCKHAND_GUEST_OTOOL)]} { set otool $::env(DOCKHAND_GUEST_OTOOL) }
set results {}
set building ""
# phase is the step under way of the target building, which mark says as
# it begins; empty between targets.
set phase ""
# fromsource are the dependencies of the target building that have no
# binary archive for the guest, so install builds them from source; said
# with the target, for live progress, and cleared between targets.
set fromsource {}
set environment [dict create]
# local is how a check in a prefix of the person's leaves it: whether it
# builds clean, as in a VM, whether it keeps what it built active, and the
# note its snapshot is made with; empty in a VM.
set local {}
if {[dict exists $input local]} { set local [dict get $input local] }

# save writes the results so far, replacing the file whole. A result's
# fields are strings, but for the ports active as it built, what its
# binaries link, and its own libraries, lists of them, the files it
# installed, a list of paths, and for where each step of its build began
# in its log, a list of each step's name and line.
proc save {state {detail ""}} {
    global root input results building phase fromsource environment
    set targets {}
    foreach result $results {
        set fields {}
        dict for {key value} $result {
            if {$key in {installed configure lint}} {
                lappend fields $key [json::write array {*}[lmap file $value { json::write string $file }]]
            } elseif {$key in {active links libraries}} {
                set ports {}
                foreach entry $value {
                    set object {}
                    dict for {name field} $entry { lappend object $name [json::write string $field] }
                    lappend ports [json::write object {*}$object]
                }
                lappend fields $key [json::write array {*}$ports]
            } elseif {$key eq "nitpick"} {
                lappend fields $key [expr {$value ? "true" : "false"}]
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
        detail [json::write string $detail] environment [json::write object {*}$facts] building [json::write string $building] \
        phase [json::write string $phase] \
        from_source [json::write array {*}[lmap name $fromsource { json::write string $name }]] \
        targets [json::write array {*}$targets]]
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
# work_dir is a port's work directory, its workpath, where its build
# systems leave their test logs. It is read as declares_tests reads
# test.run.
if {[llength [info procs work_dir]] == 0} {
    proc work_dir {portdir name variants} {
        set handle [mportopen "file://$portdir" [list subport $name] $variants]
        set worker [ditem_key $handle workername]
        set path [$worker eval {option workpath}]
        mportclose $handle
        return $path
    }
}
# test_logs appends to a target's log the last lines of the test logs its
# build systems left in its work directory, where a failed test phase
# said why: CTest's Testing/Temporary/LastTest.log, automake's
# test-suite.log, and meson's meson-logs/testlog.txt, each under a
# dockhand: line naming it. The work directory goes with the VM, and
# exiv2's log said only where LastTest.log was (the exiv2 field notes,
# 2026-10-19). At most three are taken, each its last 200 lines.
proc test_logs {log work} {
    if {![file isdirectory $work]} { return }
    catch {exec /usr/bin/find $work -maxdepth 6 -type f ( -path */Testing/Temporary/LastTest.log -o -name test-suite.log -o -path */meson-logs/testlog.txt )} found
    set paths {}
    foreach path [split $found \n] {
        if {[string match $work/* $path]} { lappend paths $path }
    }
    if {![llength $paths]} { return }
    set paths [lsort $paths]
    set fd [open $log a]
    foreach path [lrange $paths 0 2] {
        if {[catch {exec /usr/bin/tail -n 200 $path} tail]} { continue }
        puts $fd "dockhand: the last lines of [string range $path [string length $work/] end], the tests' own log:"
        puts $fd $tail
    }
    close $fd
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

# known is a command's output, trimmed, where the answer must be known: a
# command that fails is an error, never an empty answer.
proc known {args} {
    if {[catch {exec {*}$args 2>/dev/null} value]} { error "[lrange $args 1 end] failed: $value" }
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
        # make's and ninja's own failure lines, and a compiler's count,
        # follow the tool's error: kept, they'd be all a compile under make
        # says (field testing's lwan, teleport, and wails).
        if {[regexp {^(g?make(\[[0-9]+\])?: (\*\*\*|Leaving directory|Entering directory)|ninja: build stopped|FAILED: |[0-9]+ (warnings? and [0-9]+ )?errors? generated\.|-- Configuring incomplete, errors occurred)} $line]} { continue }
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

# transient is the line of a failed step's own part of its log that says
# the network failed it upstream, as a module proxy resetting its stream
# or answering 5xx, or a TLS handshake timing out; empty where none does.
# dolt's go build failed on proxy.golang.org's "stream error: stream ID
# 575; INTERNAL_ERROR; received from peer" and counted as the port's
# failure, with no second attempt (field testing at 23c5e2aa).
proc transient {log} {
    global counted
    set from 0
    if {[dict exists $counted $log]} { set from [lindex [dict get $counted $log] 0] }
    if {[catch {open $log r} fd]} { return "" }
    seek $fd $from
    set text [read $fd]
    close $fd
    foreach line [split $text \n] {
        if {![regexp {https?://|tcp } $line]} { continue }
        if {[regexp {stream error: stream ID [0-9]+; [A-Z_]+; received from peer|connection reset by peer|502 Bad Gateway|503 Service Unavailable|504 Gateway Time-?out|TLS handshake timeout} $line]} {
            return [string range [string trim $line] 0 299]
        }
    }
    return ""
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
    global counted phase
    # The step under way is said at once, for live progress: what the
    # guest is building, and how far it is (the roadmap's part 3).
    set phase $step
    save running
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

# archiveless are the ports among dependencies, and what they need, not
# yet installed, that have no binary archive for the guest: port -b
# archivefetch, binary-only, fails for one, and fetches the archive install
# would fetch for the rest. What can't be read is left out.
proc archiveless {log dependencies} {
    global port
    set closure {}
    foreach dependency $dependencies {
        if {$dependency ni $closure} { lappend closure $dependency }
        if {[catch {fact $port -q rdeps $dependency} needed]} { continue }
        foreach line [split $needed \n] {
            if {[set other [string trim $line]] ne "" && $other ni $closure} { lappend closure $other }
        }
    }
    set missing {}
    foreach name $closure {
        if {[catch {fact $port -q installed $name} installed] || [string trim $installed] ne ""} { continue }
        if {[run $log [list $port -N -b archivefetch $name]] ne ""} { lappend missing $name }
    }
    return $missing
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
    # A list that couldn't be read leaves what the build read unknown, and
    # its result isn't reused; read as none, it said the build read no
    # other port (Codex's review, checked against a268b6e5).
    foreach line [split [known $port -q installed active] \n] {
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

# links are what a passed target's installed Mach-O files link under the
# prefix (v0.5.0 part 6): each library, as the link resolves, with the
# binary that links it and the port that provides it, "" where none does.
# port contents lists the target's files, otool -L each Mach-O's
# libraries, its LC_RPATHs resolving an @rpath one, and port provides each
# library's port, asked once for all of them. A list that can't be read is
# an error, which leaves the links unknown, never none.
proc links {contents} {
    global port prefix
    set found {}
    foreach file $contents {
        if {$file eq "" || [catch {file type $file} type] || $type ne "file" || ![macho $file]} { continue }
        foreach library [linked $file] {
            if {[string first $prefix/ $library] == 0} { lappend found [list $file $library] }
        }
    }
    set libraries [lsort -unique [lmap entry $found { lindex $entry 1 }]]
    set owners [dict create]
    if {[llength $libraries]} {
        foreach line [split [known $port provides {*}$libraries] \n] {
            if {[regexp {^(.+) is provided by: (\S+)$} [string trim $line] -> library owner]} { dict set owners $library $owner }
        }
    }
    return [lmap entry $found {
        lassign $entry file library
        set owner ""
        if {[dict exists $owners $library]} { set owner [dict get $owners $library] }
        dict create binary $file library $library port $owner
    }]
}

# summary are the configure phase's summary lines in a build's log, each
# once, in order (Then's item 10): autoconf's "checking for …", CMake's
# "-- Found …" and "-- Could NOT find …", and meson's "Dependency … found",
# each said as it says what it found and not where or at what version, as
# a CMake path or a meson version moves with every build: what an update
# found, or stopped finding, against the port's previous build.
proc summary {log} {
    set found {}
    if {[catch {open $log r} fd]} { error "can't read $log: $fd" }
    while {[gets $fd line] >= 0} {
        set line [string trim $line]
        set said ""
        if {[regexp {^checking (for |whether |if ).*\.\.\. } $line]} {
            set said $line
        } elseif {[regexp {^-- (Found [^:]+?)(:| \(|$)} $line -> what]} {
            set said "-- $what"
        } elseif {[regexp {^-- (Could NOT find \S+)} $line -> what]} {
            set said "-- $what"
        } elseif {[regexp {^(Run-time dependency|Dependency|Found pkg-config|Library) (\S+) found: (YES|NO)} $line -> kind what answer]} {
            set said "$kind $what found: $answer"
        }
        if {$said ne "" && $said ni $found} { lappend found $said }
    }
    close $fd
    return $found
}

# warnings are what lint warned of, each "Warning: " line since the
# step began, without its prefix, each once (the docs dig's I13).
proc warnings {log} {
    global counted
    set from 0
    if {[dict exists $counted $log]} { set from [lindex [dict get $counted $log] 0] }
    set fd [open $log r]
    seek $fd $from
    set text [read $fd]
    close $fd
    set found {}
    foreach line [split $text \n] {
        if {[regexp {^Warning: (.+)$} [string trim $line] -> said] && $said ni $found} { lappend found $said }
    }
    return $found
}

# contents are the files a target installed, as port contents lists them,
# which the link scan and the install names read, and which its result
# keeps (Then's item 9, the docs dig's I14).
proc contents {name} {
    global port
    set files {}
    foreach file [split [known $port -q contents $name] \n] {
        set file [string trim $file]
        if {$file ne ""} { lappend files $file }
    }
    return $files
}

# libraries are the target's own dynamic libraries, each with its
# LC_ID_DYLIB install name and compatibility version as otool -l lists
# them (the reflections' E): what its dependents link, which a change of
# breaks them until they're rebuilt.
proc libraries {contents} {
    global otool
    set found {}
    foreach file $contents {
        if {[catch {file type $file} type] || $type ne "file" || ![macho $file]} { continue }
        set name ""
        set compatibility ""
        set inid 0
        foreach line [split [known $otool -l $file] \n] {
            set line [string trim $line]
            if {$line eq "cmd LC_ID_DYLIB"} { set inid 1; continue }
            if {!$inid} { continue }
            if {[regexp {^name (.+) \(offset \d+\)$} $line -> name]} { continue }
            if {[regexp {^compatibility version (\S+)$} $line -> compatibility]} { break }
            if {[string match "cmd *" $line]} { break }
        }
        if {$name ne ""} { lappend found [dict create file $file name $name compatibility $compatibility] }
    }
    return $found
}

# macho reports whether a file is a Mach-O, thin or universal, by its
# magic: a universal one's magic is Java's class files' too, which a small
# count of architectures after it tells apart.
proc macho {file} {
    if {[catch {open $file rb} fd]} { return 0 }
    set head [read $fd 8]
    close $fd
    if {[binary scan $head H8 magic] != 1} { return 0 }
    if {$magic in {feedface feedfacf cefaedfe cffaedfe}} { return 1 }
    return [expr {$magic eq "cafebabe" && [binary scan $head x4I count] == 1 && $count > 0 && $count < 32}]
}

# linked are the libraries a Mach-O links, as otool -L lists them, but for
# its own install name, each @rpath, @loader_path, or @executable_path
# resolved where it can be, through its LC_RPATHs as otool -l lists them;
# one that can't is left out.
proc linked {file} {
    global otool
    set own [lindex [split [fact $otool -D $file] \n] end]
    set rpaths {}
    set loads [split [known $otool -l $file] \n]
    set inrpath 0
    foreach line $loads {
        set line [string trim $line]
        if {$line eq "cmd LC_RPATH"} { set inrpath 1; continue }
        if {$inrpath && [regexp {^path (.+) \(offset \d+\)$} $line -> path]} {
            lappend rpaths $path
            set inrpath 0
        }
    }
    set dir [file dirname $file]
    set libraries {}
    foreach line [lrange [split [known $otool -L $file] \n] 1 end] {
        if {![regexp {^\s*(\S.*?) \(compatibility version} $line -> library] || $library eq $own} { continue }
        set resolved ""
        switch -glob -- $library {
            @rpath/* {
                foreach rpath $rpaths {
                    set candidate [string map [list @loader_path $dir @executable_path $dir] $rpath]/[string range $library 7 end]
                    if {[file exists $candidate]} { set resolved [lexical $candidate]; break }
                }
            }
            @loader_path/* - @executable_path/* {
                set resolved [lexical $dir/[string range $library [expr {[string first / $library] + 1}] end]]
            }
            default { set resolved $library }
        }
        if {$resolved ne "" && $resolved ni $libraries} { lappend libraries $resolved }
    }
    return $libraries
}

# lexical is a path with its . and .. parts taken away, as the dynamic
# loader reads one, never through a symbolic link as file normalize does.
proc lexical {path} {
    set parts {}
    foreach part [file split $path] {
        switch -- $part {
            . {}
            .. { if {[llength $parts] > 1} { set parts [lrange $parts 0 end-1] } }
            default { lappend parts $part }
        }
    }
    return [file join {*}$parts]
}

# build builds one target, returning its result.
# clean says whether each target builds with all but its dependencies
# deactivated: in a VM always, and in a prefix of the person's only where
# dockhand may treat it as its own (providers.local.clean).
proc clean {} {
    global local
    return [expr {![llength $local] || [dict get $local clean]}]
}

# actives are the ports a listing says are active, by name, each with its
# version, revision, and variants: "@1.8.1_0+universal".
proc actives {listing} {
    set found [dict create]
    foreach line [split $listing \n] {
        if {[regexp {^\s*(\S+) (@\S+) \(active\)$} $line -> name spec]} { dict set found $name $spec }
    }
    return $found
}

# snapshot makes MacPorts' port snapshot of what is active in the prefix
# before the check changes it, and returns its ID, which port restore
# --snapshot-id takes. port snapshot --create says no ID, so it is read
# from --list, the newest made with the check's note.
proc snapshot {note} {
    global port root
    if {[set message [run $root/snapshot.log [list $port -N snapshot --create --note $note]]] ne ""} {
        error "port snapshot --create failed: $message"
    }
    set id ""
    foreach line [split [known $port -q snapshot --list] \n] {
        if {[regexp {^\s*(\d+)\s+\d{4}-\d\d-\d\d \d\d:\d\d:\d\d\S*\s+(.*?)\s*$} $line -> found said] && $said eq $note && ($id eq "" || $found > $id)} {
            set id $found
        }
    }
    if {$id eq ""} { error "port snapshot --list named no snapshot noted \"$note\"" }
    return $id
}

# putback puts the prefix back as it was before the check, from the
# listing of what was active then, before.txt: each port whose active
# version changed is activated again at its old one, each the check
# activated that wasn't is deactivated, and each it deactivated is
# activated again. What the check installed stays, inactive and
# unrequested, for port reclaim. It's faster than port restore, which
# installs each port of the snapshot again. Returns "" or why it failed.
proc putback {} {
    global port root
    set fd [open $root/before.txt r]
    set before [actives [read $fd]]
    close $fd
    if {[catch {actives [known $port -q installed active]} now]} { return $now }
    set off {}
    set on {}
    dict for {name spec} $now {
        if {![dict exists $before $name]} {
            lappend off $name
        } elseif {[dict get $before $name] ne $spec} {
            lappend on $name [dict get $before $name]
        }
    }
    dict for {name spec} $before {
        if {![dict exists $now $name]} { lappend on $name $spec }
    }
    set log $root/putback.log
    if {[llength $off]} {
        if {[set message [run $log [concat [list $port -N -f deactivate] $off]]] ne ""} { return "deactivating [join $off {, }]: $message" }
    }
    foreach {name spec} $on {
        if {[set message [run $log [list $port -N activate $name $spec]]] ne ""} { return "activating $name $spec: $message" }
    }
    return ""
}

proc build {index target} {
    global root port input results local
    set id [dict get $target id]
    set name [dict get $target name]
    # Each target's log is named by its target, as the host names it.
    set logname target-$index.log
    if {[dict exists $target log] && [dict get $target log] ne ""} { set logname [dict get $target log] }
    set log $root/$logname
    set result [dict create id $id outcome "" phase "" tests none log $logname detail ""]
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

    # Only this target's dependencies are active when it builds, in a VM
    # or a prefix dockhand may clean; elsewhere it builds as-is.
    if {[clean] && [fact $port -q installed active] ne ""} {
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
    # A port the branch adds is linted with --nitpick, as MacPorts asks
    # of a new port, and every target's warnings are kept: lint fails on
    # errors alone, and warns and goes on (the docs dig's I13).
    set nitpick {}
    if {[dict exists $target added] && [dict get $target added]} {
        set nitpick --nitpick
        dict set result nitpick 1
    }
    mark result $log lint
    if {[set message [timed $log [concat $here lint $nitpick $selection] $lintBound]] eq "timed out"} {
        return [{*}$fail $result lint "lint ran past its [bound_words $lintBound] bound, so it was ended"]
    } elseif {$message ne ""} {
        return [{*}$fail $result lint [why $log $message]]
    }
    if {![catch {warnings $log} said]} {
        dict set result lint $said
    }
    set dependencies [fact $port -q echo depof:$name]
    if {$dependencies ne ""} {
        mark result $log dependencies
        # Which of them will build from source is said before they do: each
        # not yet installed, with what it needs, is asked for its binary
        # archive alone, as port(1)'s -b documents, where none is no error
        # but what install will build (the roadmap's part 3).
        set fromsource [archiveless $log $dependencies]
        if {[llength $fromsource]} {
            set fd [open $log a]
            puts $fd "dockhand: no binary archive for [join $fromsource {, }] on this release; install builds [expr {[llength $fromsource] == 1 ? {it} : {them}}] from source"
            close $fd
            save running
        }
        # Debug output only where something builds from source, which D10's
        # reading of a compiler's error and MacPorts' "Command failed:"
        # line need: installing archives with -d wrote tar's listing and
        # each "activating file:", most of exiv2's 74,000 lines (the exiv2
        # field notes, 2026-10-19).
        set debug [expr {[llength $fromsource] ? {-d} : {}}]
        if {[set message [timed $log [concat [list $port -N] $debug [list install --unrequested] $dependencies] [{*}$left]]] eq "timed out"} {
            return [{*}$fail $result dependencies "installing its dependencies, $over"]
        } elseif {$message ne ""} {
            # A phase of its own, with MacPorts' line naming the dependency,
            # "Failed to build qt5-qtwebengine: command execution failed".
            set result [{*}$fail $result dependencies [why $log $message]]
            # Which dependency failed, and its version and revision as the
            # ports tree has them, for an acceptance of the failure that
            # stands until either changes (the person's choice, 2026-10-18:
            # Monterey's Xcode refusing lang/gcc15 at pre-fetch).
            if {[regexp {Failed to [a-z]+ (\S+?):} [dict get $result detail] -> failed]} {
                dict set result dependency $failed
                lassign [split [fact $port -q info --line --version --revision $failed] \t] version revision
                if {$version ne ""} { dict set result dependency_version $version[expr {$revision ne "" ? "_$revision" : ""}] }
            }
            return $result
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
        foreach line [split [expr {[clean] ? [fact $port -q installed active] : ""}] \n] {
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
    # In a prefix of the person's, an image of the version the branch
    # builds would be activated, not built. One inactive and unrequested,
    # as a check's put-back leaves what it built for port reclaim, is
    # uninstalled first, and said; an active or requested one is the
    # person's, and the check stops, saying so (rc7's acceptance, where a
    # second check of jlv's revision was refused).
    if {[llength $local]} {
        lassign [split [fact {*}$here -q info --line --version --revision $selection] \t] version revision
        set requested [fact $port -q installed requested and $name]
        foreach line [split [fact $port -q installed $name] \n] {
            if {$version eq "" || ![regexp {^\s*\S+ (@([^+\s]+)\S*)} $line -> image installed] || $installed ne "${version}_$revision"} {
                continue
            }
            set mine [expr {[string match {*(active)*} $line] || [lsearch -exact [regexp -all -inline {\S+} $requested] $image] >= 0}]
            if {!$mine} {
                set ::phase "uninstalling $name $image, inactive and unrequested, so the branch's builds"
                save running
                if {[set message [run $log [list $port -N uninstall $name $image]]] eq ""} {
                    set fd [open $log a]
                    puts $fd "dockhand: uninstalled $name $image, inactive and unrequested, so the branch's builds"
                    close $fd
                    continue
                }
            }
            return -code error -errorcode {DOCKHAND REFUSED} "$name @${version}_$revision is installed in [dict get $input prefix] already, so MacPorts would activate it rather than build the branch's; port uninstall $name @${version}_$revision, or a revision bump, lets it build"
        }
    }
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
            # What the tests said comes back, from the logs they left.
            catch { test_logs $log [work_dir $portdir $name $variants] }
        }
    }
    return $result
}

# A check in a prefix that ended before it put the prefix back is put
# back by the next, which runs this program to do only that.
if {[llength $local] && [dict exists $local restore] && [dict get $local restore]} {
    if {[set message [putback]] ne ""} {
        save errored "putting the prefix back: $message"
        exit 1
    }
    dict set environment restored yes
    save finished
    exit 0
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
    # In a prefix of the person's, what's there is a fact of the build,
    # said with it: how many ports are active, and any other package
    # manager on the Mac. In a VM, either is an image that isn't clean.
    set foreigners {}
    foreach foreign $foreignManagers {
        if {[file exists $foreign]} {
            if {![llength $local]} { error "the image has another package manager, at $foreign" }
            lappend foreigners $foreign
        }
    }
    if {[llength $local]} {
        # The program's own process, which goes on where dockhand's ends
        # first, and says the prefix is still being built in.
        dict set environment pid [pid]
        dict set environment foreign [join $foreigners ,]
        set listing [known $port -q installed active]
        dict set environment active_ports [dict size [actives $listing]]
    } elseif {[set installed [fact $port -q installed]] ne ""} {
        error "the image already has ports installed: $installed"
    }
    if {![file exists $root/ports/PortIndex]} { error "the staged ports tree has no PortIndex" }
    # In a prefix of the person's, MacPorts reads the staged tree through
    # the host's macports.conf (PORTSRC), and the prefix's own files are
    # never written.
    if {![llength $local]} {
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
    }
    package require macports
    mportinit
    set platform [dict get $input platform]
    foreach {key actual} [list OS $::macports::os_platform Version $::macports::os_major Architecture $::macports::build_arch] {
        if {[dict get $platform $key] ne $actual} { error "the guest's $key is $actual, not [dict get $platform $key]" }
    }
    # What was active is recorded, and a snapshot made, before anything
    # changes.
    if {[llength $local]} {
        set fd [open $root/before.txt w]
        puts $fd $listing
        close $fd
        dict set environment snapshot [snapshot [dict get $local note]]
    }
} on error {message} {
    save errored $message
    exit 1
}

# finish ends the program's results with their state. A check in a prefix
# of the person's puts it back first, unless it keeps what it built
# active, and says how it left the prefix: restored, kept, or why it
# couldn't be put back, its snapshot then standing for it. The snapshot
# of a prefix put back is deleted, its work done.
proc finish {state {detail ""}} {
    global local environment port root
    if {[llength $local] && [dict exists $environment snapshot]} {
        if {[dict get $local keep]} {
            dict set environment restored kept
        } elseif {[set message [putback]] ne ""} {
            dict set environment restored "no: $message"
        } else {
            dict set environment restored yes
            run $root/putback.log [list $port -N snapshot --delete [dict get $environment snapshot]]
        }
    }
    save $state $detail
    exit [expr {$state eq "finished" ? 0 : 1}]
}
# What the guest found about itself is written now, not only with the
# first result, so it is read however soon the program ends.
save running
set index 0
foreach target [dict get $input targets] {
    incr index
    set building [dict get $target id]
    save running
    if {[catch {build $index $target} result options]} {
        # A build the prefix keeps from being a check of the branch's
        # stops it, as a refusal another attempt won't change.
        if {[dict get $options -errorcode] eq {DOCKHAND REFUSED}} { finish refused $result }
        finish errored "building [dict get $target id]: $result"
    }
    # A step the network failed upstream is said, for the host to try the
    # build again; a test's failure is the port's own.
    if {[dict get $result outcome] eq "failed" && [dict get $result phase] ni {lint test checksum}} {
        set fault [transient $root/[dict get $result log]]
        if {$fault ne ""} { dict set result transient $fault }
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
    # What a passed build's binaries link is an observation beside its
    # result, unknown where it can't be read.
    # So are the files it installed, and its own libraries' install
    # names, which a later check of its next version compares (Then's
    # item 9).
    if {[dict get $result outcome] eq "passed"} {
        if {[catch {contents [dict get $target name]} installed]} {
            set log [open $root/[dict get $result log] a]
            puts $log "dockhand: what this build installed wasn't read: $installed"
            close $log
        } else {
            dict set result installed $installed
            if {[catch {links $installed} linked]} {
                set log [open $root/[dict get $result log] a]
                puts $log "dockhand: what this build's binaries link wasn't read: $linked"
                close $log
            } else {
                dict set result links $linked
            }
            if {[catch {libraries $installed} own]} {
                set log [open $root/[dict get $result log] a]
                puts $log "dockhand: this build's libraries' install names weren't read: $own"
                close $log
            } else {
                dict set result libraries $own
            }
        }
        # And what its configure found, from its log (Then's item 10).
        if {![catch {summary $root/[dict get $result log]} said]} {
            dict set result configure $said
        }
    }
    lappend results $result
    set building ""
    set phase ""
    set fromsource {}
    save running
}
finish finished
