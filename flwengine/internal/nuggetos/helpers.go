package nuggetos

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)



var touchBuild = runtime.GOOS == "android"

func hostname() (string, error) {
	return os.Hostname()
}

func kernelVer() string {
	if runtime.GOOS == "linux" {
		if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}


func dbgPrintf(format string, args ...interface{}) {
	if currentFeatures().DevComponents {
		fmt.Fprintf(os.Stderr, "[dbg] "+format+"\n", args...)
	}
}
