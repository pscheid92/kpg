package kpg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLastTargetSerialization(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	want := LastTarget{Namespace: "app", Cluster: "app-db"}
	if err := WriteLastTarget(want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLastTarget()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v want %#v", got, want)
	}
	info, err := os.Stat(filepath.Join(stateHome, "kpg", StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %o", info.Mode().Perm())
	}
}

func TestReadLastTargetRejectsInvalidState(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	path := filepath.Join(stateHome, "kpg", StateFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"namespace":"app"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLastTarget(); err == nil {
		t.Fatal("expected invalid state error")
	}
}

func TestStatePathUsesXDGStateHome(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	got, err := StatePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(stateHome, "kpg", StateFileName)
	if got != want {
		t.Fatalf("StatePath = %q, want %q", got, want)
	}
}

func TestStatePathUsesHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	got, err := StatePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "state", "kpg", StateFileName)
	if got != want {
		t.Fatalf("StatePath = %q, want %q", got, want)
	}
}
