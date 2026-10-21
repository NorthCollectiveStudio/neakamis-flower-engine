package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"flwengine/internal/buildinfo"
	"flwengine/internal/cryptx"
	"flwengine/internal/gamehost"
	"flwengine/internal/supervisor"
)

func main() {
	mode := ""
	for i, a := range os.Args[1:] {
		if a == "-mode" || a == "--mode" {
			if i+2 < len(os.Args) {
				mode = os.Args[i+2]
			}
			break
		}
		if strings.HasPrefix(a, "-mode=") {
			mode = strings.TrimPrefix(a, "-mode=")
			break
		}
		if strings.HasPrefix(a, "--mode=") {
			mode = strings.TrimPrefix(a, "--mode=")
			break
		}
	}
	if mode == "gamehost" || mode == "serve" {
		os.Exit(gamehost.CliMain(os.Args[1:]))
	}

	dataDir := "flwdata"
	for i, a := range os.Args[1:] {
		if a == "-data" || a == "--data" {
			if i+2 < len(os.Args) {
				dataDir = os.Args[i+2]
			}
			break
		}
		if strings.HasPrefix(a, "-data=") {
			dataDir = strings.TrimPrefix(a, "-data=")
			break
		}
	}
	for _, a := range os.Args[1:] {
		if a == "-version" || a == "--version" || a == "version" {
			fmt.Printf("%s %s type=%s platform=%s go=%s\n", buildinfo.EngineName, buildinfo.Version, buildinfo.BuildType, buildinfo.CurrentPlatform(), runtime.Version())
			return
		}
	}

	if err := cryptx.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "cryptx init failed: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "datadir failed: %v\n", err)
		os.Exit(1)
	}

	supervisor.SetDataDir(dataDir)
	os.Setenv("FLW_DATA", dataDir)
	runSupervisor()
}
