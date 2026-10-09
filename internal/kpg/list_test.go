package kpg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestRenderTargetListEmpty(t *testing.T) {
	var out bytes.Buffer
	if err := RenderTargetList(&out, nil); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected empty output, got %q", out.String())
	}
}

func TestShouldShowProviderForSameTargetAcrossProviders(t *testing.T) {
	targets := []ListTarget{
		{Target: "app/app-db", Provider: ProviderCNPG},
		{Target: "app/app-db", Provider: ProviderZalando},
	}
	if !ShouldShowProvider(targets) {
		t.Fatal("expected provider column for duplicate targets across providers")
	}
}

func TestRenderTargetListWithoutProviderColumn(t *testing.T) {
	var out bytes.Buffer
	err := RenderTargetList(&out, []ListTarget{{Target: "app/app-db"}})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "PROVIDER") || !strings.Contains(got, "app/app-db") || !strings.Contains(got, "-") {
		t.Fatalf("unexpected list output:\n%s", got)
	}
}

func TestListReportsNoTargets(t *testing.T) {
	k := &fakeKube{}
	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := List(context.Background(), &out, &errOut, k, Options{Namespace: "empty"}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no table, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), `no Postgres targets found in namespace "empty"`) {
		t.Fatalf("missing empty message:\n%s", errOut.String())
	}

	errOut.Reset()
	if err := List(context.Background(), &out, &errOut, k, Options{Output: "json"}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("json output = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "no Postgres targets found; check the kube context") {
		t.Fatalf("missing empty message:\n%s", errOut.String())
	}
}

func TestListResolvesTargetsConcurrentlyInOrder(t *testing.T) {
	const count = 3 * listResolveWorkers
	targets := make([]Target, 0, count)
	secrets := map[string]AppSecret{}
	for i := range count {
		name := fmt.Sprintf("db-%02d", i)
		targets = append(targets, Target{Namespace: "ns", Cluster: name})
		secrets["ns/"+name] = AppSecret{Username: "user-" + name, Database: "database-" + name}
	}
	k := &fakeKube{targets: targets, secrets: secrets}
	var out bytes.Buffer
	if err := List(context.Background(), &out, io.Discard, k, Options{Output: "json"}); err != nil {
		t.Fatal(err)
	}
	var got []ListTarget
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid json %v:\n%s", err, out.String())
	}
	if len(got) != count {
		t.Fatalf("len = %d", len(got))
	}
	for i, item := range got {
		name := fmt.Sprintf("db-%02d", i)
		if item.Cluster != name || item.User != "user-"+name || item.Database != "database-"+name {
			t.Fatalf("item %d = %#v", i, item)
		}
	}
}
