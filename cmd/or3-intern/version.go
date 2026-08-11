package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// These values are set for release and installer builds with -ldflags.
// Development builds fall back to Go's embedded VCS metadata.
var (
	buildVersion = "dev"
	buildCommit  = ""
	buildDirty   = ""
	buildTime    = ""
)

type buildVersionInfo struct {
	Version  string
	Commit   string
	Dirty    string
	BuiltAt  string
	Go       string
	Platform string
}

func currentBuildVersionInfo() buildVersionInfo {
	info := buildVersionInfo{
		Version:  strings.TrimSpace(buildVersion),
		Commit:   strings.TrimSpace(buildCommit),
		Dirty:    strings.TrimSpace(buildDirty),
		BuiltAt:  strings.TrimSpace(buildTime),
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "dev" && buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
			info.Version = buildInfo.Main.Version
		}
		if strings.TrimSpace(buildInfo.GoVersion) != "" {
			info.Go = buildInfo.GoVersion
		}
		for _, setting := range buildInfo.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = setting.Value
				}
			case "vcs.modified":
				if info.Dirty == "" {
					info.Dirty = setting.Value
				}
			case "vcs.time":
				if info.BuiltAt == "" {
					info.BuiltAt = setting.Value
				}
			}
		}
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.Dirty == "" {
		info.Dirty = "unknown"
	}
	if info.BuiltAt == "" {
		info.BuiltAt = "unknown"
	}
	return info
}

func currentBuildVersion() string {
	return currentBuildVersionInfo().Version
}

func buildVersionString() string {
	info := currentBuildVersionInfo()
	return fmt.Sprintf("or3-intern %s\ncommit: %s\ndirty: %s\nbuilt: %s\ngo: %s\nplatform: %s", info.Version, info.Commit, info.Dirty, info.BuiltAt, info.Go, info.Platform)
}
