# The root stop (the lifecycle design's rule 4): this program, run as root
# under sudo -n only to stop, as a put-back runs it only to put back, sends
# each target it is given its signal, and nothing else. It trusts nothing it
# is handed: each target is a signal, process or group, a PID and the start
# time the starter recorded for it, in seconds, and is signalled only where
# a process with that PID started in that very second; a target that fails
# it is named and not signalled (architecture's ruling of 2026-11-23).

# process_start is a process's start time to the second, as seconds since
# the epoch, from ps(1)'s lstart, read in the C locale; empty where no
# process has the PID. Nothing a root port-tclsh can run reads the
# kernel's microseconds, which procid reads (sysctl(8) has no
# kern.proc.pid), so the root stop's test is to the second, the starter's
# own before it exact.
proc process_start {pid} {
    if {[catch {exec /usr/bin/env LC_ALL=C /bin/ps -o lstart= -p $pid} said] || [string trim $said] eq ""} { return "" }
    if {[catch {clock scan [string trim $said] -format {%a %b %d %H:%M:%S %Y}} seconds]} { return "" }
    return $seconds
}

# root_stop signals each target, given as signal, how (process or group),
# PID and start, four words each; it says each it signalled and each it
# didn't, and returns 0 where every target was signalled.
proc root_stop {targets} {
    set status 0
    foreach {signal how pid start} $targets {
        if {![string is integer -strict $pid] || $pid <= 1 || $signal ni {TERM KILL}} {
            puts "not signalled: $signal $how $pid isn't a target this stop takes"
            set status 1
            continue
        }
        if {[process_start $pid] ne $start} {
            puts "not signalled: PID $pid isn't the process recorded, whose start was $start"
            set status 1
            continue
        }
        set to $pid
        if {$how eq "group"} { set to -$pid }
        if {[catch {exec /bin/kill -$signal -- $to} message]} {
            puts "not signalled: $signal to $to: $message"
            set status 1
            continue
        }
        puts "signalled: $signal to $to"
    }
    return $status
}
