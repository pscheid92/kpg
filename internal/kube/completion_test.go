package kube

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextNamesLoadsDefaultKubeconfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config")
	config := []byte(`apiVersion: v1
kind: Config
current-context: prod
clusters: []
users: []
contexts:
  - name: prod
    context: {}
  - name: dev
    context: {}
  - name: ""
    context: {}
`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", configPath)

	names, err := ContextNames()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(names, ","), "dev,prod"; got != want {
		t.Fatalf("names = %q, want %q", got, want)
	}
}

func TestContextNamesFromNilConfig(t *testing.T) {
	if names := ContextNamesFromConfig(nil); names != nil {
		t.Fatalf("names = %#v", names)
	}
}
