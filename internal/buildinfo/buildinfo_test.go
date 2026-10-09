package buildinfo

import (
	"runtime/debug"
	"testing"
)

func stubBuildInfo(t *testing.T, info *debug.BuildInfo, ok bool) {
	t.Helper()
	old := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return info, ok }
	t.Cleanup(func() { readBuildInfo = old })
}

func stubLinkerValues(t *testing.T, version, commit, date string) {
	t.Helper()
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	Version, Commit, Date = version, commit, date
	t.Cleanup(func() { Version, Commit, Date = oldVersion, oldCommit, oldDate })
}

func TestCurrentPrefersLinkerValues(t *testing.T) {
	stubLinkerValues(t, "1.2.3", "abc123", "2026-05-09T12:00:00Z")
	stubBuildInfo(t, &debug.BuildInfo{
		Main:     debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "deadbeef"}, {Key: "vcs.modified", Value: "true"}},
	}, true)

	got := Current()
	if got.Version != "1.2.3" || got.Commit != "abc123" || got.Date != "2026-05-09T12:00:00Z" {
		t.Fatalf("Current() = %#v", got)
	}
}

func TestCurrentFallsBackToModuleAndVCSMetadata(t *testing.T) {
	stubLinkerValues(t, "dev", "none", "unknown")
	stubBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Version: "v0.3.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeef"},
			{Key: "vcs.time", Value: "2026-10-09T10:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}, true)

	got := Current()
	if got.Version != "v0.3.0" || got.Commit != "deadbeef-dirty" || got.Date != "2026-10-09T10:00:00Z" {
		t.Fatalf("Current() = %#v", got)
	}
}

func TestCurrentKeepsDefaultsWithoutUsableBuildInfo(t *testing.T) {
	stubLinkerValues(t, "dev", "none", "unknown")
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true)
	if got := Current(); got.Version != "dev" || got.Commit != "none" || got.Date != "unknown" {
		t.Fatalf("Current() with (devel) = %#v", got)
	}

	stubBuildInfo(t, nil, false)
	if got := Current(); got.Version != "dev" || got.Commit != "none" || got.Date != "unknown" {
		t.Fatalf("Current() without build info = %#v", got)
	}
}
