package kpg

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestConnectMissingAppSecretUsesBootstrapFallbackAndStoresNoSecrets(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	k := &fakeKube{
		targets: []Target{
			{Namespace: "app", Cluster: "app-db", Database: "bootstrapdb", User: "owner"},
		},
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := Connect(context.Background(), &out, &errOut, k, Options{}, "app-db", nil, true); err != nil {
		t.Fatalf("Connect error = %v, stderr = %s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "export PGUSER=owner\n") || !strings.Contains(out.String(), "export PGDATABASE=bootstrapdb\n") {
		t.Fatalf("unexpected env:\n%s", out.String())
	}
	data, err := os.ReadFile(filepath.Join(stateHome, "kpg", StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "owner") || strings.Contains(string(data), "bootstrapdb") {
		t.Fatalf("last target leaked connection data: %s", string(data))
	}
	if k.portForwardCalls != 1 {
		t.Fatalf("portForwardCalls = %d", k.portForwardCalls)
	}
}

func TestConnectNoTargetNonTTYShowsUsage(t *testing.T) {
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
	}
	err := Connect(context.Background(), io.Discard, io.Discard, k, Options{}, "", nil, false)
	if err == nil || !strings.Contains(err.Error(), "missing target") || !strings.Contains(err.Error(), "try: kpg list") {
		t.Fatalf("expected missing target usage, got %v", err)
	}
}

func TestConnectNoTargetPickerSelectsTarget(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	k := &fakeKube{
		targets: []Target{
			{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
			{Provider: ProviderCNPG, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
		},
	}
	var prompt bytes.Buffer
	var out bytes.Buffer
	var errOut bytes.Buffer
	opts := Options{
		OutputExplicit: true,
		Selection: Selection{
			Enabled: true,
			In:      strings.NewReader("2\n"),
			Out:     &prompt,
		},
	}
	err := Connect(context.Background(), &out, &errOut, k, opts, "", nil, true)
	if err != nil {
		t.Fatalf("Connect error = %v, stderr = %s", err, errOut.String())
	}
	if !strings.Contains(prompt.String(), "1  app/app-db") || !strings.Contains(prompt.String(), "2  billing/billing-db") {
		t.Fatalf("unexpected prompt:\n%s", prompt.String())
	}
	if !strings.Contains(out.String(), "export PGDATABASE=billing\n") {
		t.Fatalf("selected target env mismatch:\n%s", out.String())
	}
	last, err := ReadLastTarget()
	if err != nil {
		t.Fatal(err)
	}
	if last.Namespace != "billing" || last.Cluster != "billing-db" {
		t.Fatalf("last = %#v", last)
	}
}

func TestConnectNoTargetPickerStartsShell(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	shellPath := filepath.Join(t.TempDir(), "kpg-test-shell")
	if err := os.WriteFile(shellPath, []byte("#!/bin/sh\nprintf '%s|%s|%s|%s|%s|%s|%s' \"$KPG_TARGET\" \"$KPG_PROVIDER\" \"$PGHOST\" \"$PGPORT\" \"$PGUSER\" \"$PGDATABASE\" \"$PGSSLMODE\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shellPath)

	k := &fakeKube{
		targets: []Target{
			{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
			{Provider: ProviderCNPG, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
		},
	}
	var prompt bytes.Buffer
	var out bytes.Buffer
	var errOut bytes.Buffer
	opts := Options{
		Selection: Selection{
			Enabled: true,
			In:      strings.NewReader("2\n"),
			Out:     &prompt,
		},
	}
	err := Connect(context.Background(), &out, &errOut, k, opts, "", nil, true)
	if err != nil {
		t.Fatalf("Connect error = %v, stderr = %s", err, errOut.String())
	}
	parts := strings.Split(out.String(), "|")
	if len(parts) != 7 {
		t.Fatalf("unexpected shell output: %q", out.String())
	}
	if parts[0] != "billing/billing-db" || parts[1] != ProviderCNPG || parts[2] != "127.0.0.1" || parts[4] != "billing" || parts[5] != "billing" || parts[6] != "disable" {
		t.Fatalf("unexpected shell env: %q", out.String())
	}
	if parts[3] == "" {
		t.Fatalf("PGPORT was empty: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "starting subshell") || !strings.Contains(errOut.String(), "exit subshell to disconnect") {
		t.Fatalf("missing shell status:\n%s", errOut.String())
	}
	last, err := ReadLastTarget()
	if err != nil {
		t.Fatal(err)
	}
	if last.Namespace != "billing" || last.Cluster != "billing-db" {
		t.Fatalf("last = %#v", last)
	}
}

func TestResolveShellFallback(t *testing.T) {
	t.Setenv("SHELL", "")
	got, err := resolveShell()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/bin/sh" {
		t.Fatalf("shell = %q", got)
	}
}

func TestResolveShellRejectsMissingShell(t *testing.T) {
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	_, err := resolveShell()
	if err == nil || !strings.Contains(err.Error(), "could not start shell") {
		t.Fatalf("expected missing shell error, got %v", err)
	}
}

func TestResolveShellRejectsDirectory(t *testing.T) {
	t.Setenv("SHELL", t.TempDir())
	_, err := resolveShell()
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory shell error, got %v", err)
	}
}

func TestResolveShellRejectsNonExecutable(t *testing.T) {
	shellPath := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shellPath, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shellPath)
	_, err := resolveShell()
	if err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("expected non-executable shell error, got %v", err)
	}
}

func TestConnectLocalPortConflictDoesNotPortForward(t *testing.T) {
	port, err := FreeLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = ln.Close()
	}()

	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
	}
	err = Connect(context.Background(), io.Discard, io.Discard, k, Options{LocalPort: port}, "app-db", nil, false)
	if err == nil {
		t.Fatal("expected local port conflict")
	}
	if k.portForwardCalls != 0 {
		t.Fatalf("portForwardCalls = %d", k.portForwardCalls)
	}
}

func TestConnectExecInjectsPGEnvironmentAndStoresLastTarget(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	k := &fakeKube{
		targets: []Target{
			{Namespace: "app", Cluster: "app-db", Database: "bootstrapdb", User: "owner"},
		},
		secrets: map[string]AppSecret{
			"app/app-db": {Username: "appuser", Password: "secret", Database: "appdb"},
		},
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	clientArgs := []string{"sh", "-c", "printf '%s|%s|%s|%s|%s|%s' \"$PGHOST\" \"$PGPORT\" \"$PGUSER\" \"$PGPASSWORD\" \"$PGDATABASE\" \"$PGSSLMODE\""}
	err := Connect(context.Background(), &out, &errOut, k, Options{}, "app-db", clientArgs, true)
	if err != nil {
		t.Fatalf("Connect error = %v, stderr = %s", err, errOut.String())
	}
	parts := strings.Split(out.String(), "|")
	if len(parts) != 6 {
		t.Fatalf("unexpected client output: %q", out.String())
	}
	if parts[0] != "127.0.0.1" || parts[2] != "appuser" || parts[3] != "secret" || parts[4] != "appdb" || parts[5] != "disable" {
		t.Fatalf("unexpected env output: %q", out.String())
	}
	if parts[1] == "" {
		t.Fatalf("PGPORT was empty: %q", out.String())
	}
	data, err := os.ReadFile(filepath.Join(stateHome, "kpg", StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "appuser") {
		t.Fatalf("last target leaked secret data: %s", string(data))
	}
}

func TestConnectExecReturnsClientExitCode(t *testing.T) {
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
	}
	err := Connect(context.Background(), io.Discard, io.Discard, k, Options{}, "app-db", []string{"sh", "-c", "exit 7"}, false)
	var exitErr ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T %v", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d", exitErr.ExitCode())
	}
}

func TestConnectRejectsOutputWithCommand(t *testing.T) {
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
	}
	err := Connect(context.Background(), io.Discard, io.Discard, k, Options{OutputExplicit: true}, "app-db", []string{"psql"}, false)
	if err == nil || !strings.Contains(err.Error(), "--output cannot be combined") {
		t.Fatalf("expected output command conflict, got %v", err)
	}
	if k.portForwardCalls != 0 {
		t.Fatalf("portForwardCalls = %d", k.portForwardCalls)
	}
}
