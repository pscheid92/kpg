package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/pscheid92/kpg/internal/kpg"
)

func cnpgClusterObject(namespace string, name string, spec map[string]any, status map[string]any) unstructured.Unstructured {
	object := map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Cluster",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
	}
	if spec != nil {
		object["spec"] = spec
	}
	if status != nil {
		object["status"] = status
	}
	return unstructured.Unstructured{Object: object}
}

func cnpgTargets(items ...unstructured.Unstructured) []kpg.Target {
	return cnpgProvider{}.targets(unstructured.UnstructuredList{Items: items})
}

func TestCNPGBootstrapReadsRecoveryAndCustomSecret(t *testing.T) {
	targets := cnpgTargets(cnpgClusterObject("app", "restored-db", map[string]any{
		"bootstrap": map[string]any{
			"recovery": map[string]any{
				"database": "restored",
				"owner":    "restorer",
				"secret":   map[string]any{"name": "restored-creds"},
			},
		},
	}, map[string]any{"writeService": "restored-db-primary"}))
	if len(targets) != 1 {
		t.Fatalf("targets = %#v", targets)
	}
	got := targets[0]
	if got.Database != "restored" || got.User != "restorer" {
		t.Fatalf("recovery bootstrap not applied: %#v", got)
	}
	if got.SecretName != "restored-creds" || got.UserSecrets["restorer"] != "restored-creds" {
		t.Fatalf("custom secret not applied: %#v", got)
	}
	if got.ServiceName != "restored-db-primary" {
		t.Fatalf("status.writeService not applied: %#v", got)
	}

	targets = cnpgTargets(cnpgClusterObject("app", "cloned-db", map[string]any{
		"bootstrap": map[string]any{
			"pg_basebackup": map[string]any{"database": "clone", "owner": "cloner"},
		},
	}, nil))
	if got := targets[0]; got.Database != "clone" || got.User != "cloner" || got.SecretName != "cloned-db-app" {
		t.Fatalf("pg_basebackup bootstrap not applied: %#v", got)
	}
}

func TestCNPGBootstrapDefaultsMatchOperator(t *testing.T) {
	cases := map[string]struct {
		spec         map[string]any
		wantDatabase string
		wantOwner    string
	}{
		"no bootstrap":    {spec: map[string]any{}, wantDatabase: "app", wantOwner: "app"},
		"empty initdb":    {spec: map[string]any{"bootstrap": map[string]any{"initdb": map[string]any{}}}, wantDatabase: "app", wantOwner: "app"},
		"owner defaults":  {spec: map[string]any{"bootstrap": map[string]any{"initdb": map[string]any{"database": "orders"}}}, wantDatabase: "orders", wantOwner: "orders"},
		"explicit values": {spec: map[string]any{"bootstrap": map[string]any{"initdb": map[string]any{"database": "orders", "owner": "shop"}}}, wantDatabase: "orders", wantOwner: "shop"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := cnpgTargets(cnpgClusterObject("app", "db", tc.spec, nil))[0]
			if got.Database != tc.wantDatabase || got.User != tc.wantOwner {
				t.Fatalf("database/user = %q/%q, want %q/%q", got.Database, got.User, tc.wantDatabase, tc.wantOwner)
			}
			if got.SecretName != "db-app" || got.UserSecrets[tc.wantOwner] != "db-app" {
				t.Fatalf("app secret mapping = %#v", got.UserSecrets)
			}
		})
	}
}

func TestCNPGExposesSuperuserAndManagedRoles(t *testing.T) {
	got := cnpgTargets(cnpgClusterObject("app", "app-db", map[string]any{
		"enableSuperuserAccess": true,
		"superuserSecret":       map[string]any{"name": "custom-super"},
		"managed": map[string]any{
			"roles": []any{
				map[string]any{"name": "reporter", "login": true, "passwordSecret": map[string]any{"name": "reporter-creds"}},
				map[string]any{"name": "batch", "passwordSecret": map[string]any{"name": "batch-creds"}},
				map[string]any{"name": "gone", "login": true, "ensure": "absent", "passwordSecret": map[string]any{"name": "gone-creds"}},
				map[string]any{"name": "nosecret", "login": true},
				"not-a-role",
			},
		},
	}, nil))[0]
	if got := strings.Join(got.UserOptions, ","); got != "app,reporter" {
		t.Fatalf("user options = %q", got)
	}
	want := map[string]string{"app": "app-db-app", "reporter": "reporter-creds", "postgres": "custom-super"}
	if len(got.UserSecrets) != len(want) {
		t.Fatalf("user secrets = %#v", got.UserSecrets)
	}
	for user, secret := range want {
		if got.UserSecrets[user] != secret {
			t.Fatalf("secret for %s = %q, want %q", user, got.UserSecrets[user], secret)
		}
	}

	defaults := cnpgTargets(cnpgClusterObject("app", "app-db", map[string]any{"enableSuperuserAccess": true}, nil))[0]
	if defaults.UserSecrets["postgres"] != "app-db-superuser" {
		t.Fatalf("default superuser secret = %#v", defaults.UserSecrets)
	}
	disabled := cnpgTargets(cnpgClusterObject("app", "app-db", map[string]any{"enableSuperuserAccess": false}, nil))[0]
	if _, ok := disabled.UserSecrets["postgres"]; ok {
		t.Fatalf("superuser secret should not be exposed when access is disabled: %#v", disabled.UserSecrets)
	}
}

func TestResolveConnectionCNPGUserOverrideUsesKnownSecrets(t *testing.T) {
	c := fakeClient(nil, []runtime.Object{
		&corev1.Secret{
			Name: "app-db-app", Namespace: "app",
			Data: map[string][]byte{"username": []byte("app"), "password": []byte("apw"), "dbname": []byte("app")},
		},
		&corev1.Secret{
			Name: "app-db-superuser", Namespace: "app",
			Data: map[string][]byte{"username": []byte("postgres"), "password": []byte("spw")},
		},
		&corev1.Secret{
			Name: "reporter-creds", Namespace: "app",
			Data: map[string][]byte{"username": []byte("reporter"), "password": []byte("rpw")},
		},
	})
	target := cnpgTargets(cnpgClusterObject("app", "app-db", map[string]any{
		"managed": map[string]any{"roles": []any{
			map[string]any{"name": "reporter", "login": true, "passwordSecret": map[string]any{"name": "reporter-creds"}},
		}},
	}, nil))[0]

	cases := map[string]struct {
		user         string
		wantPassword string
		wantSecret   string
	}{
		"owner":     {user: "app", wantPassword: "apw", wantSecret: "app-db-app"},
		"superuser": {user: "postgres", wantPassword: "spw", wantSecret: "app-db-superuser"},
		"managed":   {user: "reporter", wantPassword: "rpw", wantSecret: "reporter-creds"},
		"unknown":   {user: "stranger", wantPassword: "", wantSecret: "app-db-stranger"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resolved, secret, err := c.ResolveConnection(context.Background(), kpg.Options{User: tc.user}, target)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.User != tc.user || resolved.SecretName != tc.wantSecret || secret.Password != tc.wantPassword {
				t.Fatalf("resolved = %#v secret = %#v", resolved, secret)
			}
		})
	}
}
