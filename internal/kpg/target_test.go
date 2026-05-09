package kpg

import (
	"strings"
	"testing"
)

func TestResolveTarget(t *testing.T) {
	targets := []Target{
		{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db"},
		{Namespace: "billing", Cluster: "billing-db"},
		{Namespace: "identity", Cluster: "identity-db"},
	}

	got, err := ResolveTarget("app/app-db", targets)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "app/app-db" {
		t.Fatalf("got %s", got.ID())
	}

	got, err = ResolveTarget("identity", targets)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "identity/identity-db" {
		t.Fatalf("got %s", got.ID())
	}

	_, err = ResolveTarget("db", targets)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "\n  cnpg:app/app-db") {
		t.Fatalf("expected ambiguity with candidates, got %v", err)
	}
}

func TestResolveTargetWithProviderPrefix(t *testing.T) {
	targets := []Target{
		{Provider: ProviderCNPG, Namespace: "db", Cluster: "main"},
		{Provider: ProviderZalando, Namespace: "db", Cluster: "main"},
	}
	got, err := ResolveTarget("zalando:db/main", targets)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != ProviderZalando {
		t.Fatalf("provider = %q", got.Provider)
	}
	_, err = ResolveTarget("db/main", targets)
	if err == nil || !strings.Contains(err.Error(), "cnpg:db/main") || !strings.Contains(err.Error(), "zalando:db/main") {
		t.Fatalf("expected provider-qualified ambiguity, got %v", err)
	}
}
