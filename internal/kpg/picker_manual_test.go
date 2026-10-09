//go:build manual

package kpg

// These tests drive the interactive pickers on the real terminal and print the
// outcome, so the TUI can be exercised end to end. Build the test binary and
// run it on a terminal or under a PTY driver such as expect:
//
//	go test -c -tags manual -o picker.test ./internal/kpg/
//	./picker.test -test.run TestManualTargetPicker

import (
	"fmt"
	"os"
	"testing"
)

func TestManualTargetPicker(t *testing.T) {
	t.Log("driving the target picker on the terminal")
	targets := []Target{
		{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
		{Provider: ProviderZalando, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
		{Provider: ProviderCNPG, Namespace: "identity", Cluster: "identity-db", Database: "identity", User: "identity"},
	}
	target, err := PickTargetInteractive(os.Stdin, os.Stdout, targets)
	fmt.Fprintf(os.Stdout, "\nRESULT: %s %v\n", target.ID(), err)
}

func TestManualListPicker(t *testing.T) {
	t.Log("driving the list picker on the terminal")
	choice, err := PickFromListInteractive(os.Stdin, os.Stdout, "database", []string{"app", "reports", "analytics"})
	fmt.Fprintf(os.Stdout, "\nRESULT: %s %v\n", choice, err)
}
