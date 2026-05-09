package kpg

import (
	"bytes"
	"fmt"
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

func TestTargetPickerModelNavigationCancelAndView(t *testing.T) {
	model := newTargetPickerModel([]Target{
		{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
		{Provider: ProviderZalando, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
		{Provider: ProviderCNPG, Namespace: "identity", Cluster: "identity-db", Database: "identity", User: "identity"},
	})
	if cmd := model.Init(); cmd != nil {
		t.Fatalf("Init cmd = %#v", cmd)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 9})
	model = updated.(targetPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(targetPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(targetPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(targetPickerModel)
	if model.cursor != 1 {
		t.Fatalf("cursor after navigation = %d", model.cursor)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	model = updated.(targetPickerModel)
	if model.cursor != len(model.matches)-1 {
		t.Fatalf("cursor after end = %d", model.cursor)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyHome})
	model = updated.(targetPickerModel)
	if model.cursor != 0 {
		t.Fatalf("cursor after home = %d", model.cursor)
	}

	view := model.View()
	for _, want := range []string{"Select target", "Filter:", "Target", "Provider", "app/app-db", "Enter connects"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(targetPickerModel)
	if !model.canceled {
		t.Fatal("expected picker to be canceled")
	}
}

func TestTargetPickerModelNoMatchesBackspaceAndVisibleRange(t *testing.T) {
	targets := make([]Target, 0, 20)
	for i := range 20 {
		targets = append(targets, Target{
			Provider:  ProviderCNPG,
			Namespace: "ns",
			Cluster:   fmt.Sprintf("db-%02d", i),
			Database:  "app",
			User:      "app",
		})
	}
	model := newTargetPickerModel(targets)
	model.height = 10
	model.cursor = 15
	start, end := model.visibleRange()
	if start <= 0 || end <= start || end-start != 5 {
		t.Fatalf("visibleRange = %d,%d", start, end)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("missing")})
	model = updated.(targetPickerModel)
	if len(model.matches) != 0 || !strings.Contains(model.View(), "No matching targets") {
		t.Fatalf("expected no matches, got %#v\n%s", model.matches, model.View())
	}

	for range len("missing") {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		model = updated.(targetPickerModel)
	}
	if model.query != "" || len(model.matches) != len(targets) {
		t.Fatalf("query = %q matches = %d", model.query, len(model.matches))
	}
}

func TestTargetPickerRowUsesDashesForMissingMetadata(t *testing.T) {
	row := targetPickerRow(Target{Namespace: "app", Cluster: "app-db"}, pickerWidths{
		Target:   12,
		Provider: 8,
		Database: 8,
		User:     4,
	})
	if !strings.Contains(row, "app/app-db") || !strings.Contains(row, "-         -         -") {
		t.Fatalf("unexpected row: %q", row)
	}
}

func TestPickTargetInvalidSelection(t *testing.T) {
	_, err := PickTarget(strings.NewReader("9\n"), io.Discard, []Target{{Namespace: "app", Cluster: "app-db"}})
	if err == nil || !strings.Contains(err.Error(), "invalid target selection") {
		t.Fatalf("expected invalid selection, got %v", err)
	}
}

func TestPickTargetInteractiveRejectsEmptyTargets(t *testing.T) {
	_, err := PickTargetInteractive(strings.NewReader(""), io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "no targets found") {
		t.Fatalf("expected no targets error, got %v", err)
	}
}
