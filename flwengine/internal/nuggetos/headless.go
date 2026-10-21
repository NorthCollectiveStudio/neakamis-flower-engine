package nuggetos

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flwengine/internal/buildinfo"
	"flwengine/internal/nklog"
	"flwengine/internal/supervisor"
)





func isHeadlessBuild() bool {
	f := currentFeatures()
	return !f.Dashboard && !f.BootLog && !f.Console && !f.DebugMenu
}

func runHeadless(logger *nklog.Writer) int {
	mgr := supervisor.M()
	logger.Info("supervisor", "headless prod mode: launching game directly")
	if err := mgr.LaunchGame(); err != nil {
		logger.Crit("supervisor", "game launch failed: %v", err)
		fmt.Fprintf(os.Stderr, "flower engine: game launch failed: %v\n", err)
		return 1
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case s := <-sig:
			logger.Info("supervisor", "signal %v, stopping", s)
			mgr.KillGame()
			time.Sleep(400 * time.Millisecond)
			return 0
		case <-tick.C:
			if mgr.State() == supervisor.StateIdle {
				logger.Info("supervisor", "game exited, engine closing")
				return 0
			}
		}
	}
}

var _ = buildinfo.Version
