package kube

import "testing"

func TestNestedStringSliceMap(t *testing.T) {
	got, found := nestedStringSliceMap(map[string]any{
		"spec": map[string]any{
			"users": map[string]any{
				"app":      []any{"login", float64(42), "createdb"},
				"readonly": "not-a-slice",
			},
		},
	}, "spec", "users")
	if !found {
		t.Fatal("expected users map")
	}
	if values := got["app"]; len(values) != 2 || values[0] != "login" || values[1] != "createdb" {
		t.Fatalf("app values = %#v", values)
	}
	if got["readonly"] != nil {
		t.Fatalf("readonly values = %#v", got["readonly"])
	}

	if _, found := nestedStringSliceMap(map[string]any{}, "spec", "users"); found {
		t.Fatal("expected missing users map")
	}
}

func TestZalandoSecretHelpers(t *testing.T) {
	namespace, user := zalandoSecretUser("appspace.db_user")
	if namespace != "appspace" || user != "db_user" {
		t.Fatalf("cross namespace user = %q %q", namespace, user)
	}
	namespace, user = zalandoSecretUser("db_user")
	if namespace != "" || user != "db_user" {
		t.Fatalf("local user = %q %q", namespace, user)
	}
	if got := zalandoSecretName("", "acid-main"); got != "" {
		t.Fatalf("empty user secret = %q", got)
	}
	if got := zalandoSecretName("db_user", "acid-main"); got != "db_user.acid-main.credentials.postgresql.acid.zalan.do" {
		t.Fatalf("secret name = %q", got)
	}
}
