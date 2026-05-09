package kpg

import (
	"bytes"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPickTargetFallbackTable(t *testing.T) {
	targets := []Target{
		{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
		{Provider: ProviderZalando, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
	}
	var out bytes.Buffer
	got, err := PickTarget(strings.NewReader("2\n"), &out, targets)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "billing/billing-db" {
		t.Fatalf("target = %s", got.ID())
	}
	prompt := out.String()
	for _, want := range []string{"#  Target", "Provider", "Database", "User", "1  app/app-db", "2  billing/billing-db", "Target [1-2]:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestTargetPickerModelFiltersAndSelects(t *testing.T) {
	model := newTargetPickerModel([]Target{
		{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
		{Provider: ProviderCNPG, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
		{Provider: ProviderCNPG, Namespace: "identity", Cluster: "identity-db", Database: "identity", User: "identity"},
	})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("iden")})
	model = updated.(targetPickerModel)
	if len(model.matches) != 1 {
		t.Fatalf("matches = %#v", model.matches)
	}
	if got := model.targets[model.matches[0]].ID(); got != "identity/identity-db" {
		t.Fatalf("match = %s", got)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(targetPickerModel)
	if model.selected < 0 || model.targets[model.selected].ID() != "identity/identity-db" {
		t.Fatalf("selected = %d %#v", model.selected, model.targets)
	}
}

func TestPickTargetInvalidSelection(t *testing.T) {
	_, err := PickTarget(strings.NewReader("9\n"), io.Discard, []Target{{Namespace: "app", Cluster: "app-db"}})
	if err == nil || !strings.Contains(err.Error(), "invalid target selection") {
		t.Fatalf("expected invalid selection, got %v", err)
	}
}
