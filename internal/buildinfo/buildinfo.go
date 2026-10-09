// Package buildinfo reports the version of the running binary.
package buildinfo

import "runtime/debug"

// These values are replaced by release builds via linker flags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// readBuildInfo is swapped out in tests.
var readBuildInfo = debug.ReadBuildInfo

// Current returns the values stamped by the release build. When those are
// absent it falls back to what the Go toolchain recorded, so that
// `go install ...@v1.2.3` reports the module version and a local build its
// commit.
func Current() Info {
	info := Info{Version: Version, Commit: Commit, Date: Date}
	bi, ok := readBuildInfo()
	if !ok {
		return info
	}
	if info.Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		info.Version = bi.Main.Version
	}
	modified := false
	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			if info.Commit == "none" && setting.Value != "" {
				info.Commit = setting.Value
			}
		case "vcs.time":
			if info.Date == "unknown" && setting.Value != "" {
				info.Date = setting.Value
			}
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if modified && Commit == "none" && info.Commit != "none" {
		info.Commit += "-dirty"
	}
	return info
}
