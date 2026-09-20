package portedit

import (
	"fmt"
	"os"
	"time"
)

func zzT(label string) { fmt.Fprintf(os.Stderr, "TIMING %s %s\n", time.Now().Format("15:04:05.000"), label) }
