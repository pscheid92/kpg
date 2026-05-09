package buildinfo

import "testing"

func TestCurrentReturnsBuildVariables(t *testing.T) {
	oldVersion := Version
	oldCommit := Commit
	oldDate := Date
	defer func() {
		Version = oldVersion
		Commit = oldCommit
		Date = oldDate
	}()

	Version = "1.2.3"
	Commit = "abc123"
	Date = "2026-05-09T12:00:00Z"

	got := Current()
	if got.Version != Version || got.Commit != Commit || got.Date != Date {
		t.Fatalf("Current() = %#v", got)
	}
}
