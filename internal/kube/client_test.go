package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/pscheid92/kpg/internal/kpg"
)

func TestListTargetsAcrossNamespaces(t *testing.T) {
	c := fakeClient(
		[]runtime.Object{
			cnpgCluster("app", "app-db", "app", "app"),
			cnpgCluster("billing", "billing-db", "billing", "billing"),
			cnpgCluster("identity", "identity-db", "identity", "identity"),
			zalandoCluster("legacy", "acid-main", map[string]string{"app": "app_user"}, map[string][]string{"app_user": {}}),
		},
		nil,
	)

	targets, err := c.ListTargets(context.Background(), kpg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	kpg.SortTargets(targets)
	if got, want := targetIDs(targets), []string{"app/app-db", "billing/billing-db", "identity/identity-db", "legacy/acid-main"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("target ids = %#v, want %#v", got, want)
	}
	if targets[0].Database != "app" || targets[0].User != "app" {
		t.Fatalf("bootstrap fallback missing: %#v", targets[0])
	}
	if targets[3].Provider != kpg.ProviderZalando || targets[3].Database != "app" || targets[3].User != "app_user" {
		t.Fatalf("zalando target fields missing: %#v", targets[3])
	}
}

func TestListTargetsSkipsInvalidObjectsAndZalandoFallbacks(t *testing.T) {
	c := fakeClient(
		[]runtime.Object{
			cnpgCluster("", "missing-namespace", "", ""),
			cnpgCluster("app", "", "", ""),
			zalandoCluster("legacy", "acid-users", nil, map[string][]string{
				"reporting_user": {},
				"app_user":       {},
			}),
			zalandoCluster("legacy", "acid-cross", map[string]string{"app": "appspace.db_user"}, nil),
		},
		nil,
	)

	targets, err := c.ListTargets(context.Background(), kpg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	kpg.SortTargets(targets)
	if got, want := targetIDs(targets), []string{"legacy/acid-cross", "legacy/acid-users"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("target ids = %#v, want %#v", got, want)
	}
	cross := targets[0]
	if cross.User != "appspace.db_user" || cross.SecretNamespace != "appspace" || cross.SecretName != "db_user.acid-cross.credentials.postgresql.acid.zalan.do" {
		t.Fatalf("cross namespace secret fields missing: %#v", cross)
	}
	users := targets[1]
	if users.Database != "" || users.User != "app_user" || users.SecretNamespace != "legacy" {
		t.Fatalf("user fallback fields missing: %#v", users)
	}
}

func TestListTargetsNamespaceRestriction(t *testing.T) {
	c := fakeClient(
		[]runtime.Object{
			cnpgCluster("app", "app-db", "app", "app"),
			cnpgCluster("billing", "billing-db", "billing", "billing"),
		},
		nil,
	)

	targets, err := c.ListTargets(context.Background(), kpg.Options{Namespace: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID() != "app/app-db" {
		t.Fatalf("unexpected targets: %#v", targets)
	}
}

func TestReadCredentialsOverrideAndMissingSecret(t *testing.T) {
	c := fakeClient(nil, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "app-db-app", Namespace: "app"},
			Data: map[string][]byte{
				"username": []byte("appuser"),
				"password": []byte("secret"),
				"dbname":   []byte("appdb"),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "app_user.acid-main.credentials.postgresql.acid.zalan.do", Namespace: "legacy"},
			Data: map[string][]byte{
				"username": []byte("app_user"),
				"password": []byte("zalando-secret"),
			},
		},
	})

	secret, found, err := c.ReadCredentials(context.Background(), kpg.Options{}, kpg.Target{Namespace: "app", Cluster: "app-db"})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected secret to be found")
	}
	if secret.Username != "appuser" || secret.Password != "secret" || secret.Database != "appdb" {
		t.Fatalf("unexpected secret: %#v", secret)
	}

	_, found, err = c.ReadCredentials(context.Background(), kpg.Options{}, kpg.Target{Namespace: "app", Cluster: "missing-db"})
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("missing app secret should not be found")
	}

	secret, found, err = c.ReadCredentials(context.Background(), kpg.Options{}, kpg.Target{
		Provider:        kpg.ProviderZalando,
		Namespace:       "legacy",
		Cluster:         "acid-main",
		Database:        "app",
		User:            "app_user",
		SecretName:      "app_user.acid-main.credentials.postgresql.acid.zalan.do",
		SecretNamespace: "legacy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected zalando secret to be found")
	}
	if secret.Username != "app_user" || secret.Password != "zalando-secret" || secret.Database != "app" {
		t.Fatalf("unexpected zalando secret: %#v", secret)
	}
}

func TestListNamespaces(t *testing.T) {
	c := fakeClient(nil, []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "billing"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "identity"}},
	})

	names, err := c.ListNamespaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(names, ","), "app,billing,identity"; got != want {
		t.Fatalf("names = %q, want %q", got, want)
	}
}

func TestContextNamesFromConfig(t *testing.T) {
	names := ContextNamesFromConfig(&clientcmdapi.Config{Contexts: map[string]*clientcmdapi.Context{
		"prod":    {},
		"dev":     {},
		"staging": {},
	}})
	if got, want := strings.Join(names, ","), "dev,prod,staging"; got != want {
		t.Fatalf("names = %q, want %q", got, want)
	}
}

func TestDecodeLegacySecretValue(t *testing.T) {
	if got := DecodeLegacySecretValue(map[string]string{"dbname": "YXBwZGI="}, "dbname"); got != "appdb" {
		t.Fatalf("decoded %q", got)
	}
	if got := DecodeLegacySecretValue(nil, "dbname"); got != "" {
		t.Fatalf("nil map decoded %q", got)
	}
	if got := DecodeLegacySecretValue(map[string]string{"dbname": "not-base64"}, "dbname"); got != "" {
		t.Fatalf("invalid base64 decoded %q", got)
	}
}

func TestReadCredentialsFallsBackToTargetMetadata(t *testing.T) {
	c := fakeClient(nil, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "app-db-app", Namespace: "app"},
			Data: map[string][]byte{
				"password": []byte("secret"),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "legacy-db-app", Namespace: "app"},
			Data: map[string][]byte{
				"username": []byte("legacy"),
				"database": []byte("legacydb"),
			},
		},
	})

	secret, found, err := c.ReadCredentials(context.Background(), kpg.Options{}, kpg.Target{
		Namespace: "app",
		Cluster:   "app-db",
		User:      "target-user",
		Database:  "target-db",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected secret")
	}
	if secret.Username != "target-user" || secret.Database != "target-db" || secret.Password != "secret" {
		t.Fatalf("unexpected fallback secret: %#v", secret)
	}

	secret, found, err = c.ReadCredentials(context.Background(), kpg.Options{}, kpg.Target{
		Namespace:  "app",
		Cluster:    "ignored",
		SecretName: "legacy-db-app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected legacy database secret")
	}
	if secret.Username != "legacy" || secret.Database != "legacydb" {
		t.Fatalf("unexpected legacy database secret: %#v", secret)
	}
}
