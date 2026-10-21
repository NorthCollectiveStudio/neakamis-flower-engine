package buildinfo

import (
        "encoding/json"
        "runtime"
)

var (
        Version    = "0.1.0"
        EngineName = "flower engine"
        BuildType  = "dev"
        Platform   = ""
        FeatureSet = "{}"
        GitRev     = "none"
        BuildTime  = "unknown"
        PlainRoot  = ""
)

type Features struct {
        Dashboard     bool `json:"dashboard"`
        HotReload     bool `json:"hotreload"`
        Console       bool `json:"console"`
        DebugMenu     bool `json:"debugmenu"`
        Encryption    bool `json:"encryption"`
        Signing       bool `json:"signing"`
        Obfuscation   bool `json:"obfuscation"`
        Guard         bool `json:"guard"`
        Touch         bool `json:"touch"`
        BootLog       bool `json:"bootlog"`
        PlainRuntime  bool `json:"plainruntime"`
        DevComponents bool `json:"devcomponents"`
}

func CurrentPlatform() string {
        if Platform != "" {
                return Platform
        }
        return runtime.GOOS
}

func ParseFeatures() Features {
        if FeatureSet == "" || FeatureSet == "{}" {
                return DefaultFeatures(BuildType)
        }
        var f Features
        json.Unmarshal([]byte(FeatureSet), &f)
        return f
}

func DefaultFeatures(buildType string) Features {
        switch buildType {
        case "dev":
                return Features{
                        Dashboard: true, HotReload: true, Console: true, DebugMenu: true,
                        BootLog: true, DevComponents: true, PlainRuntime: true,
                }
        case "test":
                return Features{
                        HotReload: true, Console: true, DebugMenu: true,
                        BootLog: true, DevComponents: true, PlainRuntime: true,
                }
        case "prod":
                return Features{}
        default:
                return Features{
                        Dashboard: true, Console: true, DebugMenu: true,
                        BootLog: true, DevComponents: true, PlainRuntime: true,
                }
        }
}

func Type() string {
        return BuildType
}

func IsProd() bool {
        return BuildType == "prod"
}

func IsDev() bool {
        t := BuildType
        return t == "dev" || t == "test"
}
