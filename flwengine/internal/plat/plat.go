package plat

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

type OSInfo struct {
	Platform    string `json:"platform"`
	PlatformVer string `json:"version"`
	Arch        string `json:"arch"`
	Kernel      string `json:"kernel"`
	Wine        bool   `json:"wine"`
	WineVersion string `json:"wineVersion,omitempty"`
	Host        string `json:"host"`
}

type ShellResult struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Err    string `json:"error,omitempty"`
}

var (
	infoOnce  sync.Once
	cachedOS  OSInfo
	cachedErr error
)

func Detect() (OSInfo, error) {
	infoOnce.Do(func() {
		cachedOS = OSInfo{
			Platform: runtime.GOOS,
			Arch:     runtime.GOARCH,
			Host:     hostName(),
		}
		switch runtime.GOOS {
		case "windows":
			cachedOS.PlatformVer = windowsVersion()
			cachedOS.Kernel = cachedOS.PlatformVer
			if v, ok := wineVersionWindows(); ok {
				cachedOS.Wine = true
				cachedOS.WineVersion = v
			}
		case "linux":
			cachedOS.PlatformVer = linuxVersion()
			cachedOS.Kernel = kernelVersion()
			cachedOS.Platform = linuxFlavor()
		case "android":
			cachedOS.PlatformVer = androidVersion()
			cachedOS.Kernel = kernelVersion()
		}
	})
	return cachedOS, cachedErr
}

func IsWindows() bool { return runtime.GOOS == "windows" }
func IsLinux() bool   { return runtime.GOOS == "linux" }
func IsAndroid() bool { return runtime.GOOS == "android" }

func ShellSupported() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "linux"
}

func ShellExec(dir string, command string, args []string, timeoutSec int) ShellResult {
	if !ShellSupported() {
		return ShellResult{Code: -1, Err: "shell not supported on this platform"}
	}
	if command == "" {
		return ShellResult{Code: -1, Err: "empty command"}
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return ShellResult{Code: -1, Err: err.Error()}
	}
	go func() { done <- cmd.Wait() }()
	var err error
	timedOut := false
	select {
	case err = <-done:
	case <-timeAfter(timeoutSec):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		timedOut = true
		err = <-done
	}
	res := ShellResult{
		Stdout: clipOutput(stdout.String()),
		Stderr: clipOutput(stderr.String()),
	}
	if timedOut {
		res.Code = -2
		res.Err = "timeout"
		return res
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.Code = ee.ExitCode()
		} else {
			res.Code = -1
			res.Err = err.Error()
		}
		return res
	}
	res.Code = 0
	return res
}

func clipOutput(s string) string {
	if len(s) > 128*1024 {
		return s[:128*1024] + "...(clipped)"
	}
	return s
}

func hostName() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func linuxVersion() string {
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "VERSION_ID=") {
				return strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
			}
		}
	}
	return ""
}

func linuxFlavor() string {
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "ID=") {
				return strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
			}
		}
	}
	return "linux"
}

func kernelVersion() string {
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

func androidVersion() string {
	if b, err := os.ReadFile("/system/build.prop"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "ro.build.version.release=") {
				return strings.TrimSpace(strings.TrimPrefix(line, "ro.build.version.release="))
			}
		}
	}
	if v, err := getprop("ro.build.version.release"); err == nil && v != "" {
		return v
	}
	return ""
}

func getprop(name string) (string, error) {
	out, err := exec.Command("/system/bin/getprop", name).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
