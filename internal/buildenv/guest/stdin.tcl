# stdin_is says whether this process's standard input is the file at path,
# by device and file number, as a root build's guest checks before its
# first save that the lock its starter handed sudo reached it (the
# lifecycle design's "Local and Script", rule 4). The input is read by
# fstat of descriptor 0, through stat(1) with no file, which reads its own
# standard input, this one inherited; never by a stat of the path
# /dev/fd/0, which on macOS gives devfs's device beside the file's number
# and so never equals the lock's.
proc stdin_is {path} {
    if {[catch {exec /usr/bin/stat -f "%d %i"} in]} { return 0 }
    if {[catch {file stat $path held}]} { return 0 }
    return [expr {$in eq "$held(dev) $held(ino)"}]
}

