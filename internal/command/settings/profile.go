package settings

import (
	"os"
	"runtime"
	"runtime/pprof"
)

// HeapProfileVariable names the file a command writes its in-use heap
// profile to as it exits, and serve also each time its queue empties after
// work, for a measurement such as what serve holds between daily looks
// (the roadmap's "serve gives back a look's memory once its work ends").
const HeapProfileVariable = "DOCKHAND_HEAPPROFILE"

// WriteHeapProfile writes the in-use heap, after a collection, to file, as
// runtime/pprof makes one, replacing what it held; go tool pprof
// -sample_index=inuse_space reads it.
func WriteHeapProfile(file string) error {
	runtime.GC()
	out, err := os.Create(file)
	if err != nil {
		return err
	}
	if err := pprof.Lookup("heap").WriteTo(out, 0); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
