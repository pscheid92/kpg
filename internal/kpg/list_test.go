package kpg

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestListSecretOverride(t *testing.T) {
	k := &fakeKube{
		targets: []Target{
			{Namespace: "app", Cluster: "app-db", Database: "bootstrapdb", User: "owner"},
		},
		secrets: map[string]AppSecret{
			"app/app-db": {Username: "appuser", Password: "pw", Database: "appdb"},
		},
	}
	var out bytes.Buffer
	if err := List(context.Background(), &out, io.Discard, k, Options{}); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	want := "TARGET      DATABASE  USER\napp/app-db  appdb     appuser"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestListShowsProviderWhenProvidersAreMixed(t *testing.T) {
	k := &fakeKube{
		targets: []Target{
			{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
			{Provider: ProviderZalando, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
		},
	}
	var out bytes.Buffer
	if err := List(context.Background(), &out, io.Discard, k, Options{}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "TARGET              PROVIDER  DATABASE  USER") {
		t.Fatalf("missing header:\n%s", got)
	}
	if !strings.Contains(got, "app/app-db          cnpg      app       app") {
		t.Fatalf("missing cnpg provider:\n%s", got)
	}
	if !strings.Contains(got, "billing/billing-db  zalando   billing   billing") {
		t.Fatalf("missing zalando provider:\n%s", got)
	}
}

func TestListJSON(t *testing.T) {
	k := &fakeKube{
		targets: []Target{
			{Provider: ProviderZalando, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing", ServiceName: "billing-db"},
		},
	}
	var out bytes.Buffer
	if err := List(context.Background(), &out, io.Discard, k, Options{Output: "json"}); err != nil {
		t.Fatal(err)
	}
	var got []ListTarget
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid json %v:\n%s", err, out.String())
	}
	if len(got) != 1 {
		t.Fatalf("targets = %#v", got)
	}
	want := ListTarget{
		Target:          "billing/billing-db",
		QualifiedTarget: "zalando:billing/billing-db",
		Provider:        ProviderZalando,
		Namespace:       "billing",
		Cluster:         "billing-db",
		Database:        "billing",
		User:            "billing",
		Service:         "billing-db",
	}
	if got[0] != want {
		t.Fatalf("got %#v want %#v", got[0], want)
	}
}
