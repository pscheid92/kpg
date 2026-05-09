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
