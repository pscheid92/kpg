package kube

import "testing"

func TestCredentialHelpers(t *testing.T) {
	if got := secretString(nil, "username"); got != "" {
		t.Fatalf("nil secret string = %q", got)
	}
	if got := secretString(map[string][]byte{"username": []byte("app")}, "username"); got != "app" {
		t.Fatalf("secret string = %q", got)
	}
	if got := firstNonEmpty("", "", "app", "fallback"); got != "app" {
		t.Fatalf("first non-empty = %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("empty values = %q", got)
	}
}
